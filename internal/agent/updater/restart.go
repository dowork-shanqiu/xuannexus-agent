package updater

import "go.uber.org/zap"

// RestartSelf 触发 agent 不停机重启。
// Unix 平台通过 syscall.Exec 原地替换进程，Windows 平台通过启动新进程后退出。
func RestartSelf(logger *zap.Logger) {
	restartSelf(logger)
}
