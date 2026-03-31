package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds the agent configuration
type Config struct {
	Server       ServerConfig       `yaml:"server"`
	Registration RegistrationConfig `yaml:"registration"`
	Heartbeat    HeartbeatConfig    `yaml:"heartbeat"`
	Metrics      MetricsConfig      `yaml:"metrics"`
	Log          LogConfig          `yaml:"log"`
	FileTransfer FileTransferConfig `yaml:"file_transfer"`
	Certificate  CertificateConfig  `yaml:"certificate"`
	Upgrade      UpgradeConfig      `yaml:"upgrade"`
}

// UpgradeConfig holds auto-upgrade configuration
type UpgradeConfig struct {
	// Enabled 是否启用自动升级检测，默认 true
	Enabled bool `yaml:"enabled"`
	// Schedule cron 表达式，用于定时检测新版本，默认 "0 3 * * *"（每日凌晨 3 点）
	Schedule string `yaml:"schedule"`
	// MirrorURL 升级镜像地址，为空时使用 GitHub Release API；
	// 适用于中国大陆等无法直接访问 GitHub 的网络环境。
	// 镜像应提供与 GitHub Release 相同的目录结构：{mirror_url}/{tag}/xuannexus-agent-{os}-{arch}
	MirrorURL string `yaml:"mirror_url"`
	// GithubRepo GitHub 仓库地址，用于从 GitHub Release 检测与下载，默认 "dowork-shanqiu/xuannexus-agent"
	GithubRepo string `yaml:"github_repo"`
}

// ServerConfig holds server connection configuration
type ServerConfig struct {
	Address string        `yaml:"address"` // gRPC address
	Timeout time.Duration `yaml:"timeout"`
	TLS     TLSConfig     `yaml:"tls"`
}

// TLSConfig holds TLS configuration
type TLSConfig struct {
	Enable             bool   `yaml:"enable"`
	// CACert CA 证书路径，用于验证服务端证书（单向 TLS 和 mTLS 均需要）
	CACert             string `yaml:"ca_cert"`
	// ClientCert 客户端证书路径（mTLS 双向认证时需要）
	ClientCert         string `yaml:"client_cert"`
	// ClientKey 客户端私钥路径（mTLS 双向认证时需要）
	ClientKey          string `yaml:"client_key"`
	// InsecureSkipVerify 跳过服务端证书验证（仅用于开发/测试环境，生产环境请勿启用）
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify"`
}

// RegistrationConfig holds agent registration configuration (NEW MECHANISM)
type RegistrationConfig struct {
	Key     string `yaml:"key"`      // Registration key from pre-created agent
	AgentID string `yaml:"agent_id"` // Auto-filled after registration
	Token   string `yaml:"token"`    // Auto-filled after registration
}

// HeartbeatConfig holds heartbeat configuration
type HeartbeatConfig struct {
	Interval time.Duration `yaml:"interval"`
}

// MetricsConfig holds metrics configuration
type MetricsConfig struct {
	ReportInterval time.Duration `yaml:"report_interval"`
}

// LogConfig holds log configuration
type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// FileTransferConfig holds file transfer configuration
type FileTransferConfig struct {
	ChunkSize int    `yaml:"chunk_size"`
	TempDir   string `yaml:"temp_dir"`
}

// CertificateConfig holds certificate storage configuration
type CertificateConfig struct {
	StorageDir string `yaml:"storage_dir"` // Directory to store certificates, default: ./certs
}

// Load loads configuration from file
func Load(configPath string) (*Config, error) {
	// Default configuration
	cfg := &Config{
		Server: ServerConfig{
			Address: "localhost:9090",
			Timeout: 30 * time.Second,
			TLS: TLSConfig{
				Enable: false,
			},
		},
		Registration: RegistrationConfig{},
		Heartbeat: HeartbeatConfig{
			Interval: 30 * time.Second,
		},
		Metrics: MetricsConfig{
			ReportInterval: 60 * time.Second,
		},
		Log: LogConfig{
			Level:  "info",
			Format: "console",
		},
		FileTransfer: FileTransferConfig{
			ChunkSize: 1024 * 1024, // 1MB
			TempDir:   "/tmp/xuannexus-agent",
		},
		Certificate: CertificateConfig{
			StorageDir: "./certs", // Default certificate storage directory
		},
		Upgrade: UpgradeConfig{
			Enabled:    true,
			Schedule:   "0 3 * * *", // 默认每日凌晨 3 点检测
			GithubRepo: "dowork-shanqiu/xuannexus-agent",
		},
	}

	// Find configuration file
	if configPath == "" {
		configPath = findConfigFile()
	}

	// Load from file if exists
	if configPath != "" && fileExists(configPath) {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}

		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", err)
		}
	}

	// Decrypt token if encrypted
	if cfg.Registration.Token != "" {
		decrypted, err := DecryptToken(cfg.Registration.Token)
		if err == nil {
			cfg.Registration.Token = decrypted
		}
		// If decryption fails, keep the original token (backward compatibility)
	}

	// Validate configuration
	if err := validate(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Save saves configuration to file
func Save(cfg *Config, configPath string) error {
	if configPath == "" {
		configPath = findConfigFile()
		if configPath == "" {
			configPath = "./agent.yaml"
		}
	}

	// Create a copy of config for saving
	saveCfg := *cfg

	// Encrypt token before saving
	if saveCfg.Registration.Token != "" {
		encrypted, err := EncryptToken(saveCfg.Registration.Token)
		if err == nil {
			saveCfg.Registration.Token = encrypted
		}
		// If encryption fails, save as-is (backward compatibility)
	}

	data, err := yaml.Marshal(&saveCfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// validate validates the configuration
func validate(cfg *Config) error {
	if cfg.Server.Address == "" {
		return fmt.Errorf("server address is required")
	}

	if cfg.Registration.Key == "" && (cfg.Registration.AgentID == "" || cfg.Registration.Token == "") {
		return fmt.Errorf("registration key is required for initial registration")
	}

	return nil
}

// EnsureConfigFile 检查 Agent 配置文件是否存在，若不存在则使用默认内容创建。
// 返回 (true, nil) 表示新建了配置文件，(false, nil) 表示配置文件已存在。
// 注意：新生成的配置文件中 registration.key 为空，需要用户手动填写后方可启动 agent。
func EnsureConfigFile(configPath string) (created bool, err error) {
	if configPath == "" {
		configPath = "./agent.yaml"
	}
	if _, statErr := os.Stat(configPath); statErr == nil {
		return false, nil // 已存在
	}

	dir := filepath.Dir(configPath)
	if dir != "" && dir != "." {
		if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
			return false, fmt.Errorf("创建配置目录失败: %w", mkErr)
		}
	}

	content := defaultAgentConfigContent()
	if writeErr := os.WriteFile(configPath, []byte(content), 0o600); writeErr != nil {
		return false, fmt.Errorf("写入默认配置文件失败: %w", writeErr)
	}
	return true, nil
}

// defaultAgentConfigContent 返回 Agent 默认配置文件内容（YAML 格式）。
func defaultAgentConfigContent() string {
	return `# XuanNexus Agent 配置文件
# 首次启动时自动生成，请根据实际情况修改

# Server gRPC 连接配置
server:
  address: "localhost:9090"   # gRPC 服务端地址（host:port）
  timeout: 30s
  tls:
    enable: false                 # 是否启用 TLS
    ca_cert: ""                   # CA 证书路径，用于验证服务端证书（单向 TLS 和 mTLS 均需要）
    client_cert: ""               # 客户端证书路径（mTLS 双向认证时需要）
    client_key: ""                # 客户端私钥路径（mTLS 双向认证时需要）
    insecure_skip_verify: false   # 跳过服务端证书验证（仅测试环境，生产环境请勿启用）

# 注册信息
# 首次注册：在 XuanNexus 后台预创建 Agent 后，将获得的注册密钥填入 key 字段
# 注册成功后 agent_id 和 token 会自动写入，无需手动修改
registration:
  key: ""         # 注册密钥（必填，从后台获取）

# 心跳配置
heartbeat:
  interval: 30s

# 指标上报配置
metrics:
  report_interval: 60s

# 日志配置
log:
  level: info     # debug / info / warn / error
  format: console # console / json

# 文件传输配置
file_transfer:
  chunk_size: 1048576                   # 1MB
  temp_dir: "/tmp/xuannexus-agent"

# 证书存储目录
certificate:
  storage_dir: "./certs"

# 自动升级配置
upgrade:
  enabled: true                                    # 是否启用自动升级检测
  schedule: "0 3 * * *"                            # cron 表达式，默认每日凌晨 3 点检测
  mirror_url: ""                                   # 升级镜像地址（留空则使用 GitHub Release）
                                                   # 中国大陆用户可配置镜像以加速下载
                                                   # 镜像目录结构：{mirror_url}/{tag}/xuannexus-agent-{os}-{arch}
  github_repo: "dowork-shanqiu/xuannexus-agent"    # GitHub 仓库（owner/repo）
`
}

// findConfigFile searches for configuration file in multiple locations
func findConfigFile() string {
	searchPaths := []string{
		"./configs/agent.yaml",
		"./agent.yaml",
		"/etc/xuannexus/agent.yaml",
		filepath.Join(os.Getenv("HOME"), ".xuannexus", "agent.yaml"),
	}

	for _, path := range searchPaths {
		if fileExists(path) {
			return path
		}
	}

	return ""
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
