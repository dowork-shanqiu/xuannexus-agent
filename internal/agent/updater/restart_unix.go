//go:build !windows

package updater

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"go.uber.org/zap"
)

// restartSelf 通过 syscall.Exec 实现不停机重启（Unix 平台）。
// 该调用会用新的二进制文件替换当前进程，PID 保持不变。
// 如果 agent 是通过 systemd 管理的，退出后 systemd 会自动重启新版本。
func restartSelf(logger *zap.Logger) {
	exe, err := resolveExePath()
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

// resolveExePath 解析当前可执行文件的文件系统路径。
//
// 不直接使用 os.Executable()，因为在 Linux 上它通过 /proc/self/exe 跟踪进程的
// 可执行文件 inode。自更新时，replaceBinary 会将原始二进制 rename 为 .old 再删除，
// 导致 /proc/self/exe 指向已删除的 inode，os.Executable() 返回不存在的路径，
// 进而使 syscall.Exec 报 "no such file or directory"。
//
// 改用 os.Args[0] 解析路径：它记录的是进程启动时传入的文件系统路径字符串，
// 不随 inode 变化，自更新完成后新二进制恰好位于该路径。
func resolveExePath() (string, error) {
	arg0 := os.Args[0]

	// 已是绝对路径，直接使用（systemd 服务最常见的场景）
	if filepath.IsAbs(arg0) {
		return arg0, nil
	}

	// 相对路径或裸名称，通过 exec.LookPath 在 CWD / PATH 中查找
	p, err := exec.LookPath(arg0)
	if err == nil {
		return p, nil
	}

	// 最后回退：使用 os.Executable()（可能返回过期路径，但至少尝试）
	return os.Executable()
}
