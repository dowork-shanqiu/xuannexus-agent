//go:build darwin

package installer

import "github.com/kardianos/service"

// applyPlatformOptions 在 macOS 上注入用户组支持：
//   - 当 GroupName 非空时，通过自定义 launchd plist 模板将 GroupName 键写入 plist；
//   - 模板基于 kardianos/service 内置 launchd 模板，仅增加 GroupName 节点。
//
// 模板数据结构中 .Option 即 service.KeyValue（map[string]interface{}），
// 因此可在模板内通过 index .Option "GroupName" 读取用户组名称。
func applyPlatformOptions(cfg *service.Config, opts ServiceOptions) {
	if opts.GroupName == "" {
		return
	}
	cfg.Option["GroupName"] = opts.GroupName
	cfg.Option["LaunchdConfig"] = darwinLaunchdTemplate
}

// darwinLaunchdTemplate 是增加了 GroupName 支持的 launchd plist 模板。
// 该模板与 kardianos/service 内置 launchd 模板保持一致，
// 仅在 UserName 节点下方插入可选的 GroupName 节点。
const darwinLaunchdTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Disabled</key>
	<false/>
	{{- if .EnvVars}}
	<key>EnvironmentVariables</key>
	<dict>
		{{- range $k, $v := .EnvVars}}
		<key>{{html $k}}</key>
		<string>{{html $v}}</string>
		{{- end}}
	</dict>
	{{- end}}
	<key>KeepAlive</key>
	<{{bool .KeepAlive}}/>
	<key>Label</key>
	<string>{{html .Name}}</string>
	<key>ProgramArguments</key>
	<array>
		<string>{{html .Path}}</string>
		{{- if .Config.Arguments}}
		{{- range .Config.Arguments}}
		<string>{{html .}}</string>
		{{- end}}
	{{- end}}
	</array>
	{{- if .ChRoot}}
	<key>RootDirectory</key>
	<string>{{html .ChRoot}}</string>
	{{- end}}
	<key>RunAtLoad</key>
	<{{bool .RunAtLoad}}/>
	<key>SessionCreate</key>
	<{{bool .SessionCreate}}/>
	{{- if .StandardErrorPath}}
	<key>StandardErrorPath</key>
	<string>{{html .StandardErrorPath}}</string>
	{{- end}}
	{{- if .StandardOutPath}}
	<key>StandardOutPath</key>
	<string>{{html .StandardOutPath}}</string>
	{{- end}}
	{{- if .UserName}}
	<key>UserName</key>
	<string>{{html .UserName}}</string>
	{{- end}}
	{{- if index .Option "GroupName"}}
	<key>GroupName</key>
	<string>{{html (index .Option "GroupName")}}</string>
	{{- end}}
	{{- if .WorkingDirectory}}
	<key>WorkingDirectory</key>
	<string>{{html .WorkingDirectory}}</string>
	{{- end}}
</dict>
</plist>
`
