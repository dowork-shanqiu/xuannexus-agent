//go:build linux

package installer

import "github.com/kardianos/service"

// applyPlatformOptions 在 Linux 上注入用户组支持：
//   - 当 GroupName 非空时，通过自定义 systemd 单元模板将 Group= 写入 [Service] 节；
//   - 模板基于 kardianos/service 内置 systemd 模板，仅增加 Group= 行。
//
// 模板数据结构中 .Option 即 service.KeyValue（map[string]interface{}），
// 因此可在模板内通过 index .Option "GroupName" 读取用户组名称。
func applyPlatformOptions(cfg *service.Config, opts ServiceOptions) {
	if opts.GroupName == "" {
		return
	}
	cfg.Option["GroupName"] = opts.GroupName
	cfg.Option["SystemdScript"] = linuxSystemdTemplate
}

// linuxSystemdTemplate 是增加了 Group= 支持的 systemd 单元模板。
// 该模板与 kardianos/service 内置模板保持一致，仅在 User= 行下方插入可选的 Group= 行。
const linuxSystemdTemplate = `[Unit]
Description={{.Description}}
Documentation=https://xn.codeyet.com
After=network-online.target remote-fs.target nss-lookup.target
Wants=network-online.target

[Service]
StartLimitInterval=5
StartLimitBurst=10
ExecStart={{.Path|cmdEscape}}{{range .Arguments}} {{.|cmd}}{{end}}
{{if .ChRoot}}RootDirectory={{.ChRoot|cmd}}{{end}}
{{if .WorkingDirectory}}WorkingDirectory={{.WorkingDirectory|cmdEscape}}{{end}}
{{if .UserName}}User={{.UserName}}{{end}}
{{if index .Option "GroupName"}}Group={{index .Option "GroupName"}}{{end}}
{{if .ReloadSignal}}ExecReload=/bin/kill -{{.ReloadSignal}} "$MAINPID"{{end}}
{{if .PIDFile}}PIDFile={{.PIDFile|cmd}}{{end}}
{{if and .LogOutput .HasOutputFileSupport -}}
StandardOutput=file:{{.LogDirectory}}/{{.Name}}.out
StandardError=file:{{.LogDirectory}}/{{.Name}}.err
{{else}}
StandardOutput=journal
StandardError=journal
{{- end}}
{{if gt .LimitNOFILE -1 }}LimitNOFILE={{.LimitNOFILE}}{{end}}
{{if .Restart}}Restart={{.Restart}}{{end}}
{{if .SuccessExitStatus}}SuccessExitStatus={{.SuccessExitStatus}}{{end}}
RestartSec=120
# EnvironmentFile=-/etc/sysconfig/{{.Name}}

{{range $k, $v := .EnvVars -}}
Environment={{$k}}={{$v}}
{{end -}}

[Install]
WantedBy=multi-user.target
`
