//go:build windows

package updater

import (
	"os"
	"os/exec"

	"go.uber.org/zap"
)

// restartSelf 在 Windows 平台上重启 agent。
// Windows 不支持 syscall.Exec，因此启动一个新进程后退出当前进程。
// 如果 agent 是作为 Windows Service 运行的，退出后 SCM 会自动重启新版本。
func restartSelf(logger *zap.Logger) {
	exePath, err := os.Executable()
	if err != nil {
		logger.Error("获取可执行文件路径失败，无法重启", zap.Error(err))
		return
	}

	logger.Info("Windows 平台重启：启动新进程后退出当前进程",
		zap.String("executable", exePath),
		zap.Strings("args", os.Args[1:]))

	cmd := exec.Command(exePath, os.Args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		logger.Error("启动新进程失败", zap.Error(err))
		return
	}

	logger.Info("新进程已启动，退出当前进程", zap.Int("new_pid", cmd.Process.Pid))
	os.Exit(0)
}
