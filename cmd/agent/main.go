package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/kardianos/service"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"github.com/dowork-shanqiu/xuannexus-agent/internal/agent/client"
	"github.com/dowork-shanqiu/xuannexus-agent/internal/agent/collector"
	"github.com/dowork-shanqiu/xuannexus-agent/internal/agent/config"
	"github.com/dowork-shanqiu/xuannexus-agent/internal/agent/updater"
	"github.com/dowork-shanqiu/xuannexus-agent/pkg/installer"
)

var (
	Version   = "1.0.0"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

// agentProgram implements service.Interface, holding all agent runtime state.
type agentProgram struct {
	// config
	configFilePath string

	// runtime state
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// Start is called by kardianos/service when the service starts; must be non-blocking.
func (p *agentProgram) Start(_ service.Service) error {
	p.done = make(chan struct{})
	go p.run()
	return nil
}

// Stop is called by kardianos/service to initiate graceful shutdown.
func (p *agentProgram) Stop(_ service.Service) error {
	p.mu.Lock()
	cancel := p.cancel
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if p.done != nil {
		<-p.done
	}
	return nil
}

// run contains the agent main loop, executed in a dedicated goroutine.
func (p *agentProgram) run() {
	defer close(p.done)

	// ── 确保配置文件存在 ──────────────────────────────────────────────────────
	created, cfgErr := config.EnsureConfigFile(p.configFilePath)
	if cfgErr != nil {
		fmt.Fprintf(os.Stderr, "初始化配置文件失败: %v\n", cfgErr)
		return
	}
	if created {
		fmt.Printf("已生成默认配置文件: %s\n", p.configFilePath)
		fmt.Println("提示：请编辑配置文件，填写 registration.key（从后台获取），然后重新启动 agent。")
		// registration.key 是必填项，无默认值，首次无法继续启动
		return
	}

	// ── 加载配置 ──────────────────────────────────────────────────────────────
	cfg, err := config.Load(p.configFilePath)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		return
	}

	// ── 初始化日志 ────────────────────────────────────────────────────────────
	logger := initLogger(cfg)
	defer func() { _ = logger.Sync() }()

	logger.Info("Starting XuanNexus Agent",
		zap.String("version", Version),
		zap.String("build_time", BuildTime),
		zap.String("git_commit", GitCommit))

	// ── 创建上下文（用于优雅关闭） ────────────────────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	p.mu.Lock()
	p.cancel = cancel
	p.mu.Unlock()
	defer cancel()

	// ── 创建 gRPC 客户端并连接 ────────────────────────────────────────────────
	grpcClient, err := client.NewGRPCClient(cfg, logger)
	if err != nil {
		logger.Fatal("Failed to create gRPC client", zap.Error(err))
	}
	grpcClient.SetVersion(Version)
	if err := grpcClient.Connect(); err != nil {
		logger.Fatal("Failed to connect to server", zap.Error(err))
	}
	defer func() { _ = grpcClient.Close() }()
	logger.Info("Connected to server", zap.String("address", cfg.Server.Address))

	// ── 注册 Agent ────────────────────────────────────────────────────────────
	if cfg.Registration.AgentID == "" || cfg.Registration.Token == "" {
		logger.Info("Registering agent with server...")
		agentID, token, err := grpcClient.Register(Version)
		if err != nil {
			logger.Fatal("Failed to register agent", zap.Error(err))
		}
		cfg.Registration.AgentID = agentID
		cfg.Registration.Token = token
		if err := config.Save(cfg, p.configFilePath); err != nil {
			logger.Warn("Failed to save configuration", zap.Error(err))
		} else {
			logger.Info("Agent registered successfully",
				zap.String("agent_id", agentID),
				zap.String("version", Version))
		}
	} else {
		logger.Info("Using existing agent credentials",
			zap.String("agent_id", cfg.Registration.AgentID),
			zap.String("version", Version))
	}

	// ── 采集静态信息 ──────────────────────────────────────────────────────────
	systemCollector := collector.NewSystemCollector(logger)
	logger.Info("Reporting static system information...")
	staticInfo := systemCollector.CollectStaticInfo()
	if err := grpcClient.ReportStaticInfo(staticInfo); err != nil {
		logger.Error("Failed to report static info", zap.Error(err))
	} else {
		logger.Info("Static information reported successfully")
	}

	// 连接成功后的上报逻辑（重连时复用）
	reportAfterConnect := func() {
		logger.Info("Reporting static info after reconnection...")
		si := systemCollector.CollectStaticInfo()
		if err := grpcClient.ReportStaticInfo(si); err != nil {
			logger.Error("Failed to report static info after reconnection", zap.Error(err))
		} else {
			logger.Info("Static info re-reported after reconnection")
		}

		logger.Info("Reporting initial dynamic metrics after reconnection...")
		dm := systemCollector.CollectDynamicMetrics()
		if err := grpcClient.ReportDynamicMetrics(dm); err != nil {
			logger.Error("Failed to report dynamic metrics after reconnection", zap.Error(err))
		} else {
			logger.Info("Dynamic metrics reported after reconnection")
		}
	}

	// ── 启动重连管理器 ────────────────────────────────────────────────────────
	reconnectMgr := client.NewReconnectManager(grpcClient, cfg, logger, 0, reportAfterConnect)
	go reconnectMgr.Start(ctx)
	logger.Info("Auto-reconnect manager started")

	// ── 启动命令监听器 ────────────────────────────────────────────────────────
	commandListener := client.NewCommandListener(grpcClient, cfg.Registration.AgentID, cfg.Registration.Token, logger, systemCollector)

	// ── 初始化自升级模块 ──────────────────────────────────────────────────────
	onRestart := func() {
		logger.Info("升级完成，触发不停机重启...")
		updater.RestartSelf(logger)
	}
	agentUpdater := updater.NewUpdater(cfg, logger, Version, onRestart)
	upgradeScheduler := updater.NewScheduler(agentUpdater, cfg, logger)

	// 将升级调度器注入命令监听器，使 server 可以通过 __check_update__ 指令触发升级
	commandListener.SetUpdateChecker(upgradeScheduler)

	go commandListener.Start(ctx)
	logger.Info("Command listener started")

	// ── 启动自动升级调度器 ────────────────────────────────────────────────────
	if err := upgradeScheduler.Start(ctx); err != nil {
		logger.Error("Failed to start upgrade scheduler", zap.Error(err))
	} else {
		logger.Info("Upgrade scheduler started",
			zap.Bool("enabled", cfg.Upgrade.Enabled),
			zap.String("schedule", cfg.Upgrade.Schedule))
	}

	// ── 心跳与指标上报循环 ────────────────────────────────────────────────────
	heartbeatTicker := time.NewTicker(cfg.Heartbeat.Interval)
	defer heartbeatTicker.Stop()
	metricsTicker := time.NewTicker(cfg.Metrics.ReportInterval)
	defer metricsTicker.Stop()

	logger.Info("Agent started, entering main loop",
		zap.Duration("heartbeat_interval", cfg.Heartbeat.Interval),
		zap.Duration("metrics_interval", cfg.Metrics.ReportInterval))

	for {
		select {
		case <-heartbeatTicker.C:
			if reconnectMgr.IsConnected() {
				if err := grpcClient.SendHeartbeat(); err != nil {
					logger.Error("Failed to send heartbeat", zap.Error(err))
				} else {
					logger.Debug("Heartbeat sent successfully")
				}
			} else {
				logger.Debug("Skipping heartbeat, not connected")
			}

		case <-metricsTicker.C:
			if reconnectMgr.IsConnected() {
				logger.Debug("Collecting and reporting metrics...")
				metrics := systemCollector.CollectDynamicMetrics()
				if err := grpcClient.ReportDynamicMetrics(metrics); err != nil {
					logger.Error("Failed to report metrics", zap.Error(err))
				} else {
					logger.Debug("Metrics reported successfully")
				}
			} else {
				logger.Debug("Skipping metrics report, not connected")
			}

		case <-ctx.Done():
			logger.Info("Shutting down agent...")
			upgradeScheduler.Stop()
			commandListener.Stop()
			reconnectMgr.Stop()
			logger.Info("Agent stopped")
			return
		}
	}
}

func main() {
	configPath := flag.String("config", "configs/agent.yaml", "Path to configuration file")
	version := flag.Bool("version", false, "Show version information")
	doInstall := flag.Bool("install", false, "将 agent 注册为系统服务（需要 root/管理员权限）")
	doUninstall := flag.Bool("uninstall", false, "从系统中卸载 agent 服务（需要 root/管理员权限）")
	serviceUser := flag.String("service-user", "", "以指定用户身份运行服务，留空则使用默认用户")
	serviceGroup := flag.String("service-group", "", "以指定用户组身份运行服务（仅 Linux/macOS），留空则使用默认用户组")
	flag.Parse()

	if *version {
		fmt.Printf("XuanNexus Agent\n")
		fmt.Printf("Version: %s\n", Version)
		fmt.Printf("Build Time: %s\n", BuildTime)
		fmt.Printf("Git Commit: %s\n", GitCommit)
		os.Exit(0)
	}

	cfgPath := *configPath

	// ── install / uninstall ──────────────────────────────────────────────────
	if *doInstall || *doUninstall {
		if *doInstall {
			created, cfgErr := config.EnsureConfigFile(cfgPath)
			if cfgErr != nil {
				fmt.Fprintf(os.Stderr, "初始化配置文件失败: %v\n", cfgErr)
				os.Exit(1)
			}
			if created {
				fmt.Printf("已生成默认配置文件: %s\n", cfgPath)
			}
			fmt.Printf("\n[重要] 请在继续之前编辑配置文件 %s\n", cfgPath)
			fmt.Println("必填项：registration.key（从 XuanNexus 后台预创建 Agent 后获取）")
			fmt.Println("可选项：server.address（gRPC 服务端地址，默认 localhost:9090）")
			fmt.Print("\n是否已完成配置，继续安装? [y/N]: ")
			var confirm string
			_, scanErr := fmt.Scanln(&confirm)
			if scanErr != nil || (confirm != "y" && confirm != "Y") {
				fmt.Println("已取消安装。请编辑配置文件后重新运行 --install。")
				os.Exit(0)
			}

			opts := installer.ServiceOptions{
				Name:        "xuannexus-agent",
				DisplayName: "XuanNexus Agent",
				Description: "XuanNexus 主机管理 Agent 服务",
				Args:        []string{"-config", cfgPath},
				UserName:    *serviceUser,
				GroupName:   *serviceGroup,
			}
			if err := installer.Install(opts); err != nil {
				fmt.Fprintf(os.Stderr, "安装失败: %v\n", err)
				os.Exit(1)
			}
		} else {
			if err := installer.Uninstall("xuannexus-agent"); err != nil {
				fmt.Fprintf(os.Stderr, "卸载失败: %v\n", err)
				os.Exit(1)
			}
		}
		os.Exit(0)
	}

	// ── 正常运行（交互或作为服务） ────────────────────────────────────────────
	prg := &agentProgram{
		configFilePath: cfgPath,
	}

	svcConfig := &service.Config{
		Name:        "xuannexus-agent",
		DisplayName: "XuanNexus Agent",
		Description: "XuanNexus 主机管理 Agent 服务",
	}

	svc, err := service.New(prg, svcConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "创建服务失败: %v\n", err)
		os.Exit(1)
	}

	if err := svc.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "服务运行失败: %v\n", err)
		os.Exit(1)
	}
}

func initLogger(cfg *config.Config) *zap.Logger {
	var level zapcore.Level
	if err := level.UnmarshalText([]byte(cfg.Log.Level)); err != nil {
		level = zapcore.InfoLevel
	}

	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	var encoder zapcore.Encoder
	if cfg.Log.Format == "json" {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	} else {
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	}

	core := zapcore.NewCore(encoder, zapcore.AddSync(os.Stdout), level)
	return zap.New(core, zap.AddCaller())
}
