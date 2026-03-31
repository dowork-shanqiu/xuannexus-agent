//go:build !linux && !darwin

package installer

import "github.com/kardianos/service"

// applyPlatformOptions 在 Windows 等平台上不执行任何操作。
// GroupName 在这些平台上不适用，忽略该字段。
func applyPlatformOptions(_ *service.Config, _ ServiceOptions) {}
