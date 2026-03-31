package updater

import (
	"context"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
	"github.com/dowork-shanqiu/xuannexus-agent/internal/agent/config"
)

// Scheduler 基于 cron 表达式定时执行版本检查与升级。
type Scheduler struct {
	updater *Updater
	cfg     *config.Config
	logger  *zap.Logger
	cron    *cron.Cron
	cancel  context.CancelFunc
}

// NewScheduler 创建一个新的定时升级调度器。
func NewScheduler(updater *Updater, cfg *config.Config, logger *zap.Logger) *Scheduler {
	return &Scheduler{
		updater: updater,
		cfg:     cfg,
		logger:  logger,
	}
}

// Start 启动定时调度器。若配置中升级未启用则直接返回。
func (s *Scheduler) Start(ctx context.Context) error {
	if !s.cfg.Upgrade.Enabled {
		s.logger.Info("自动升级检测已禁用")
		return nil
	}

	schedule := s.cfg.Upgrade.Schedule
	if schedule == "" {
		schedule = "0 3 * * *"
	}

	s.logger.Info("启动自动升级调度器",
		zap.String("schedule", schedule))

	childCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	s.cron = cron.New()

	_, err := s.cron.AddFunc(schedule, func() {
		s.logger.Info("定时升级检查触发")
		upgraded, err := s.updater.CheckAndUpgrade(childCtx)
		if err != nil {
			s.logger.Error("定时升级检查失败", zap.Error(err))
			return
		}
		if upgraded {
			s.logger.Info("定时升级检查完成：已升级到新版本")
		} else {
			s.logger.Info("定时升级检查完成：当前已是最新版本")
		}
	})
	if err != nil {
		cancel()
		return err
	}

	s.cron.Start()

	// 在后台等待上下文取消
	go func() {
		<-childCtx.Done()
		s.cron.Stop()
		s.logger.Info("自动升级调度器已停止")
	}()

	return nil
}

// Stop 停止定时调度器。
func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
}

// TriggerCheck 手动触发一次版本检查与升级（可由 server 端指令调用）。
func (s *Scheduler) TriggerCheck(ctx context.Context) (bool, error) {
	s.logger.Info("手动触发升级检查")
	return s.updater.CheckAndUpgrade(ctx)
}
