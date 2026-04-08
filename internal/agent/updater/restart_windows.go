//go:build windows

package updater

import (
	"os"
	"os/exec"
	"path/filepath"

	"go.uber.org/zap"
)

// restartSelf 在 Windows 平台上重启 agent。
// Windows 不支持 syscall.Exec，因此启动一个新进程后退出当前进程。
// 如果 agent 是作为 Windows Service 运行的，退出后 SCM 会自动重启新版本。
func restartSelf(logger *zap.Logger) {
	exePath, err := resolveExePath()
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

// resolveExePath 解析当前可执行文件的文件系统路径。
// 参见 restart_unix.go 中同名函数的注释，原理相同。
func resolveExePath() (string, error) {
	arg0 := os.Args[0]

	// 已是绝对路径，直接使用
	if filepath.IsAbs(arg0) {
		return arg0, nil
	}

	// 相对路径或裸名称，通过 exec.LookPath 在 CWD / PATH 中查找
	p, err := exec.LookPath(arg0)
	if err == nil {
		return p, nil
	}

	// 最后回退：使用 os.Executable()
	return os.Executable()
}
