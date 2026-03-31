# XuanNexus Agent

XuanNexus Agent 是 [XuanNexus](https://github.com/dowork-shanqiu/xuannexus) 主机管理平台的客户端组件，负责部署在被管理的主机上，与 XuanNexus 服务端通过 gRPC 进行通信，实现系统信息采集、指标上报、远程命令执行等功能。

## 功能特性

- **系统信息采集**：自动采集主机的 CPU、内存、磁盘、网络等静态和动态指标
- **gRPC 通信**：通过 gRPC 双向流与服务端进行安全通信
- **mTLS 安全通信**：支持双向 TLS 认证，保障通信安全
- **自动注册**：Agent 启动后自动向服务端注册并获取凭证
- **心跳保活**：定期发送心跳包，维持连接状态
- **断线重连**：内置自动重连机制，保障连接可靠性
- **远程命令执行**：支持从服务端下发命令并在沙箱环境中安全执行
- **文件传输**：支持与服务端之间的文件上传和下载
- **系统服务**：支持注册为系统服务（systemd / launchd / Windows Service）
- **跨平台支持**：支持 Linux、macOS、Windows

## 项目结构

```
.
├── api/                        # API 定义
│   └── proto/agent/            # gRPC Protobuf 定义及生成代码
├── cmd/                        # 可执行程序入口
│   └── agent/                  # Agent 主程序
│       └── main.go
├── configs/                    # 配置文件模板
│   └── agent.yaml              # Agent 默认配置
├── internal/                   # 内部包（不对外导出）
│   └── agent/
│       ├── client/             # gRPC 客户端、命令监听、重连管理等
│       ├── collector/          # 系统指标采集器（按平台区分）
│       └── config/             # 配置加载与管理
├── pkg/                        # 可复用的公共包
│   └── installer/              # 系统服务安装/卸载
├── scripts/                    # 辅助脚本
│   └── gen-mtls-certs.sh       # mTLS 证书生成脚本
├── .github/workflows/          # CI/CD 工作流
│   ├── ci.yml                  # 持续集成（lint、test、build）
│   └── release.yml             # 发布构建（多平台二进制）
├── go.mod                      # Go 模块定义
└── go.sum                      # 依赖校验
```

## 快速开始

### 环境要求

- Go 1.25 或更高版本

### 构建

```bash
# 克隆仓库
git clone https://github.com/dowork-shanqiu/xuannexus-agent.git
cd xuannexus-agent

# 构建
go build -o xuannexus-agent ./cmd/agent

# 带版本信息构建
go build -ldflags "-X 'main.Version=v1.0.0' -X 'main.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)' -X 'main.GitCommit=$(git rev-parse HEAD)'" -o xuannexus-agent ./cmd/agent
```

### 运行

```bash
# 首次运行会自动生成默认配置文件
./xuannexus-agent

# 指定配置文件路径
./xuannexus-agent -config /path/to/agent.yaml

# 查看版本信息
./xuannexus-agent -version
```

### 安装为系统服务

```bash
# 安装为系统服务（需要 root/管理员权限）
sudo ./xuannexus-agent -install -config /etc/xuannexus-agent/agent.yaml

# 指定运行用户和用户组
sudo ./xuannexus-agent -install -config /etc/xuannexus-agent/agent.yaml -service-user xuannexus -service-group xuannexus

# 卸载系统服务
sudo ./xuannexus-agent -uninstall
```

## 配置说明

Agent 配置文件默认路径为 `configs/agent.yaml`，首次运行时会自动生成。

### 主要配置项

| 配置项 | 说明 | 默认值 |
|--------|------|--------|
| `server.address` | gRPC 服务端地址 | `localhost:9090` |
| `server.timeout` | 连接超时时间 | `30s` |
| `server.tls.enable` | 是否启用 TLS | `false` |
| `registration.key` | 注册密钥（必填，从服务端获取） | 空 |
| `heartbeat.interval` | 心跳间隔 | `30s` |
| `metrics.report_interval` | 指标上报间隔 | `60s` |
| `log.level` | 日志级别 | `info` |
| `log.format` | 日志格式（console/json） | `console` |

### TLS 配置（mTLS）

如果服务端启用了 gRPC TLS，需要配置以下选项：

```yaml
server:
  tls:
    enable: true
    ca_cert: "/path/to/ca.crt"
    client_cert: "/path/to/client.crt"
    client_key: "/path/to/client.key"
```

可使用 `scripts/gen-mtls-certs.sh` 脚本生成所需的 mTLS 证书。

## 开发

### 运行测试

```bash
go test ./...
```

### 代码检查

```bash
go vet ./...
```

### 重新生成 Protobuf 代码

如需修改 gRPC 接口定义，编辑 `api/proto/agent/agent.proto` 后执行：

```bash
protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
       api/proto/agent/agent.proto
```

## CI/CD

本项目包含两个 GitHub Actions 工作流：

- **CI**（`ci.yml`）：在 push 到 main 分支和 Pull Request 时触发，执行代码检查、测试和多平台构建验证
- **Release**（`release.yml`）：在推送以 `v` 开头的 tag 时触发，自动构建多平台二进制并创建 GitHub Release

### 支持的构建目标

| 操作系统 | 架构 |
|----------|------|
| Linux | amd64 |
| Windows | amd64 |
| macOS | amd64 |
| macOS | arm64 |

## 许可证

本项目基于 [MIT 许可证](LICENSE) 开源。
