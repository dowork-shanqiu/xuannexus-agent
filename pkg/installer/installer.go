// Package installer 提供基于 kardianos/service 的跨平台系统服务安装/卸载功能。
// 支持 Linux（systemd / SysV init / upstart）、Windows（服务管理器）和 macOS（launchd）。
package installer

import (
	"fmt"

	"github.com/kardianos/service"
)

// ServiceOptions 描述要注册的系统服务选项。
type ServiceOptions struct {
	// Name 服务名称（英文、无空格），如 "xuannexus-server"
	Name string
	// DisplayName 人性化显示名称
	DisplayName string
	// Description 服务描述
	Description string
	// ExecPath 可执行文件的绝对路径；留空则使用当前进程的可执行文件
	ExecPath string
	// Args 服务启动时传递给可执行文件的参数，例如 []string{"-config", "/etc/xuannexus/config.yaml"}
	Args []string
	// WorkingDir 工作目录；留空时使用 ExecPath 所在目录
	WorkingDir string
	// UserName 以指定用户身份运行（Linux/macOS）；留空时使用默认用户
	UserName string
	// GroupName 以指定用户组身份运行（仅 Linux/macOS）；留空时使用默认用户组
	GroupName string
	// Restart 服务重启策略（如 "on-failure"、"always" 等，具体值依赖于平台和服务管理器）；留空使用默认重启策略
	Restart string
	// EnvVars 需要注入到服务环境中的环境变量（仅 Linux/macOS）；键值对形式，如 map[string]string{"ENV_VAR1": "value1", "ENV_VAR2": "value2"}
	EnvVars map[string]string
}

// Install 将可执行程序注册为系统服务，并尝试立即启动。
func Install(opts ServiceOptions) error {
	if opts.Name == "" {
		return fmt.Errorf("服务名称不能为空")
	}

	svc, err := buildService(opts)
	if err != nil {
		return fmt.Errorf("创建服务对象失败: %w", err)
	}

	if err := svc.Install(); err != nil {
		return fmt.Errorf("安装服务失败（请以 root/管理员身份运行）: %w", err)
	}
	fmt.Printf("服务 %s 已安装\n", opts.Name)

	if err := svc.Start(); err != nil {
		fmt.Printf("警告：启动服务 %s 失败: %v（可手动启动）\n", opts.Name, err)
	} else {
		fmt.Printf("服务 %s 已启动\n", opts.Name)
	}
	return nil
}

// Uninstall 停止并删除指定系统服务。
func Uninstall(name string) error {
	if name == "" {
		return fmt.Errorf("服务名称不能为空")
	}

	svc, err := buildService(ServiceOptions{Name: name})
	if err != nil {
		return fmt.Errorf("创建服务对象失败: %w", err)
	}

	// 尝试停止（允许失败——服务可能未运行）
	_ = svc.Stop()

	if err := svc.Uninstall(); err != nil {
		return fmt.Errorf("卸载服务失败（请以 root/管理员身份运行）: %w", err)
	}
	fmt.Printf("服务 %s 已卸载\n", name)
	return nil
}

// buildService 根据选项构建 service.Service 对象。
// 使用 noopProgram 作为占位程序接口（仅用于安装/卸载，不需要实际运行）。
func buildService(opts ServiceOptions) (service.Service, error) {
	cfg := &service.Config{
		Name:             opts.Name,
		DisplayName:      opts.DisplayName,
		Description:      opts.Description,
		Executable:       opts.ExecPath,
		Arguments:        opts.Args,
		WorkingDirectory: opts.WorkingDir,
		UserName:         opts.UserName,
		Option:           service.KeyValue{},
	}

	// 应用平台特定选项（如用户组注入等）
	applyPlatformOptions(cfg, opts)

	prg := &noopProgram{}
	return service.New(prg, cfg)
}

// noopProgram 是一个占位实现，满足 service.Interface 接口要求。
// Install/Uninstall 操作仅注册/注销系统服务元数据，不需要运行实际的程序逻辑，
// 因此 Start/Stop 方法均为空操作。实际的服务逻辑在 cmd/server/main.go 和
// cmd/agent/main.go 的各自 serverProgram/agentProgram 中实现。
type noopProgram struct{}

func (p *noopProgram) Start(_ service.Service) error { return nil }
func (p *noopProgram) Stop(_ service.Service) error  { return nil }
