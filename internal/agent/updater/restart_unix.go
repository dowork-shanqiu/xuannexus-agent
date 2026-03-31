//go:build !windows

package updater

import (
	"os"
	"syscall"

	"go.uber.org/zap"
)

// restartSelf 通过 syscall.Exec 实现不停机重启（Unix 平台）。
// 该调用会用新的二进制文件替换当前进程，PID 保持不变。
// 如果 agent 是通过 systemd 管理的，退出后 systemd 会自动重启新版本。
func restartSelf(logger *zap.Logger) {
	exe, err := os.Executable()
	if err != nil {
		logger.Error("获取可执行文件路径失败，无法重启", zap.Error(err))
		return
	}

	args := os.Args
	logger.Info("执行不停机重启",
		zap.String("executable", exe),
		zap.Strings("args", args))

	// 使用 syscall.Exec 替换当前进程
	// 这是 exec 系统调用的 Go 封装，不会创建新进程，而是直接替换当前进程映像
	err = syscall.Exec(exe, args, os.Environ())
	if err != nil {
		// 如果 Exec 失败，回退为正常退出（由 systemd 或其他 supervisor 重启）
		logger.Error("syscall.Exec 失败，回退为正常退出", zap.Error(err))
		os.Exit(0)
	}
}
