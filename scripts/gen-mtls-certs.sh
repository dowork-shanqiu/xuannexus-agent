#!/usr/bin/env bash
# =============================================================================
# XuanNexus gRPC mTLS 证书生成脚本
#
# 使用基于 CA 的 PKI 体系：
#   - 一个私有 CA 签发所有证书
#   - Server 持有：CA 证书、服务端证书、服务端私钥
#   - 每个 Agent 持有：CA 证书、Agent 证书（由同一 CA 签发）、Agent 私钥
#   - Server 只需配置一个 CA 证书，即可验证所有由该 CA 签发的 Agent 证书
#   - 新增 Agent 时只需用 CA 签发一张新证书，无需修改 server 配置
#
# 用法：
#   # 1. 初始化 CA（只需执行一次）
#   ./gen-mtls-certs.sh init-ca  [--ca-dir /path/to/ca]
#
#   # 2. 生成服务端证书
#   ./gen-mtls-certs.sh gen-server --host your-server.example.com  [--ca-dir /path/to/ca] [--out /path/to/server/certs]
#
#   # 3. 为新 Agent 生成证书（每次注册新主机时执行）
#   ./gen-mtls-certs.sh gen-agent --name agent-hostname  [--ca-dir /path/to/ca] [--out /path/to/agent/certs]
#
# 依赖：openssl（通常已内置于系统）
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEFAULT_CA_DIR="${SCRIPT_DIR}/../certs/ca"
DEFAULT_OUT_DIR="${SCRIPT_DIR}/../certs"
DAYS_CA=3650     # CA 有效期 10 年
DAYS_CERT=825    # 服务端/Agent 证书有效期约 2.25 年（符合主流浏览器/客户端要求）

# 颜色输出
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*" >&2; exit 1; }

# --------------------------------------------------------------------------
# 子命令：init-ca — 创建私有根 CA
# --------------------------------------------------------------------------
cmd_init_ca() {
    local ca_dir="$DEFAULT_CA_DIR"
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --ca-dir) ca_dir="$2"; shift 2 ;;
            *) error "未知参数：$1" ;;
        esac
    done

    if [[ -f "${ca_dir}/ca.key" ]]; then
        warn "CA 私钥已存在：${ca_dir}/ca.key"
        warn "若要重新生成，请先删除该目录后再执行本命令。"
        warn "注意：重新生成 CA 后，所有旧证书将失效，需重新签发。"
        exit 1
    fi

    mkdir -p "$ca_dir"
    chmod 700 "$ca_dir"

    info "生成 CA 私钥 (4096-bit RSA)..."
    openssl genrsa -out "${ca_dir}/ca.key" 4096
    chmod 600 "${ca_dir}/ca.key"

    info "生成 CA 自签名证书（有效期 ${DAYS_CA} 天）..."
    openssl req -new -x509 -key "${ca_dir}/ca.key" \
        -out "${ca_dir}/ca.crt" \
        -days "${DAYS_CA}" \
        -subj "/CN=XuanNexus-gRPC-CA/O=XuanNexus/OU=Infrastructure"

    info "CA 证书已生成："
    info "  私钥：${ca_dir}/ca.key  （请妥善保管，勿外泄）"
    info "  证书：${ca_dir}/ca.crt  （分发给所有 Server 和 Agent）"
    openssl x509 -in "${ca_dir}/ca.crt" -noout -subject -dates
}

# --------------------------------------------------------------------------
# 子命令：gen-server — 生成服务端证书
# --------------------------------------------------------------------------
cmd_gen_server() {
    local host=""; local ca_dir="$DEFAULT_CA_DIR"; local out_dir="${DEFAULT_OUT_DIR}/server"
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --host)   host="$2";    shift 2 ;;
            --ca-dir) ca_dir="$2";  shift 2 ;;
            --out)    out_dir="$2"; shift 2 ;;
            *) error "未知参数：$1" ;;
        esac
    done
    [[ -z "$host" ]] && error "请通过 --host 指定服务端域名或 IP（例：your-server.example.com 或 192.168.1.1）"
    [[ ! -f "${ca_dir}/ca.key" ]] && error "CA 私钥不存在，请先执行 init-ca"

    mkdir -p "$out_dir"

    info "生成服务端私钥..."
    openssl genrsa -out "${out_dir}/server.key" 2048
    chmod 600 "${out_dir}/server.key"

    # 构建 SAN（Subject Alternative Names）配置
    local san_cfg; san_cfg=$(mktemp)
    trap "rm -f ${san_cfg}" EXIT
    local san_value
    if [[ "$host" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        san_value="IP:${host}"
    else
        san_value="DNS:${host}"
    fi
    cat > "$san_cfg" <<EOF
[req]
distinguished_name = req_distinguished_name
req_extensions = v3_req
prompt = no

[req_distinguished_name]
CN = ${host}
O = XuanNexus
OU = Server

[v3_req]
basicConstraints = CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = ${san_value}
EOF

    info "生成服务端 CSR..."
    openssl req -new -key "${out_dir}/server.key" \
        -out "${out_dir}/server.csr" \
        -config "$san_cfg"

    info "用 CA 签发服务端证书（有效期 ${DAYS_CERT} 天）..."
    openssl x509 -req \
        -in "${out_dir}/server.csr" \
        -CA "${ca_dir}/ca.crt" \
        -CAkey "${ca_dir}/ca.key" \
        -CAcreateserial \
        -out "${out_dir}/server.crt" \
        -days "${DAYS_CERT}" \
        -extfile "$san_cfg" \
        -extensions v3_req

    cp "${ca_dir}/ca.crt" "${out_dir}/ca.crt"
    rm -f "${out_dir}/server.csr"

    info "服务端证书已生成："
    info "  ${out_dir}/server.crt  （服务端证书）"
    info "  ${out_dir}/server.key  （服务端私钥）"
    info "  ${out_dir}/ca.crt      （CA 证书，配置到 ca_cert）"
    echo ""
    info "请在 config.yaml 中配置如下（host.grpc_tls 节）："
    echo "  grpc_tls:"
    echo "    enable: true"
    echo "    cert_file: ${out_dir}/server.crt"
    echo "    key_file:  ${out_dir}/server.key"
    echo "    ca_cert:   ${out_dir}/ca.crt"
    echo "    client_auth: true   # 开启 mTLS，强制要求 Agent 提供证书"
}

# --------------------------------------------------------------------------
# 子命令：gen-agent — 为新 Agent 生成证书
# --------------------------------------------------------------------------
cmd_gen_agent() {
    local name=""; local ca_dir="$DEFAULT_CA_DIR"; local out_dir=""
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --name)   name="$2";    shift 2 ;;
            --ca-dir) ca_dir="$2";  shift 2 ;;
            --out)    out_dir="$2"; shift 2 ;;
            *) error "未知参数：$1" ;;
        esac
    done
    [[ -z "$name" ]] && error "请通过 --name 指定 Agent 名称（例：agent-web01 或主机名）"
    [[ ! -f "${ca_dir}/ca.key" ]] && error "CA 私钥不存在，请先执行 init-ca"

    [[ -z "$out_dir" ]] && out_dir="${DEFAULT_OUT_DIR}/agents/${name}"
    mkdir -p "$out_dir"

    info "为 Agent [${name}] 生成私钥..."
    openssl genrsa -out "${out_dir}/agent.key" 2048
    chmod 600 "${out_dir}/agent.key"

    local san_cfg; san_cfg=$(mktemp)
    trap "rm -f ${san_cfg}" EXIT
    cat > "$san_cfg" <<EOF
[req]
distinguished_name = req_distinguished_name
req_extensions = v3_req
prompt = no

[req_distinguished_name]
CN = ${name}
O = XuanNexus
OU = Agent

[v3_req]
basicConstraints = CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = clientAuth
subjectAltName = DNS:${name}
EOF

    info "生成 Agent CSR..."
    openssl req -new -key "${out_dir}/agent.key" \
        -out "${out_dir}/agent.csr" \
        -config "$san_cfg"

    info "用 CA 签发 Agent 证书（有效期 ${DAYS_CERT} 天）..."
    openssl x509 -req \
        -in "${out_dir}/agent.csr" \
        -CA "${ca_dir}/ca.crt" \
        -CAkey "${ca_dir}/ca.key" \
        -CAcreateserial \
        -out "${out_dir}/agent.crt" \
        -days "${DAYS_CERT}" \
        -extfile "$san_cfg" \
        -extensions v3_req

    cp "${ca_dir}/ca.crt" "${out_dir}/ca.crt"
    rm -f "${out_dir}/agent.csr"

    info "Agent [${name}] 证书已生成："
    info "  ${out_dir}/agent.crt  （Agent 证书）"
    info "  ${out_dir}/agent.key  （Agent 私钥）"
    info "  ${out_dir}/ca.crt     （CA 证书，用于验证服务端）"
    echo ""
    info "请将以上三个文件复制到 Agent 主机，并在 agent.yaml 中配置如下（server.tls 节）："
    echo "  tls:"
    echo "    enable: true"
    echo "    ca_cert:     /path/to/ca.crt"
    echo "    client_cert: /path/to/agent.crt"
    echo "    client_key:  /path/to/agent.key"
    echo "    insecure_skip_verify: false"
}

# --------------------------------------------------------------------------
# 入口
# --------------------------------------------------------------------------
SUBCMD="${1:-help}"
shift || true

case "$SUBCMD" in
    init-ca)    cmd_init_ca    "$@" ;;
    gen-server) cmd_gen_server "$@" ;;
    gen-agent)  cmd_gen_agent  "$@" ;;
    help|--help|-h)
        echo "用法："
        echo "  $0 init-ca   [--ca-dir DIR]                      # 初始化私有 CA（只需执行一次）"
        echo "  $0 gen-server --host HOST [--ca-dir DIR] [--out DIR]  # 生成服务端证书"
        echo "  $0 gen-agent  --name NAME [--ca-dir DIR] [--out DIR]  # 为新 Agent 生成证书"
        echo ""
        echo "mTLS 配置原则："
        echo "  - Server 只需配置一个 CA 证书路径，可验证所有由同一 CA 签发的 Agent 证书"
        echo "  - 新增 Agent 时只需执行 gen-agent，无需修改 Server 配置"
        echo "  - CA 私钥 (ca.key) 请妥善保管，只在签发新证书时使用"
        ;;
    *) error "未知子命令：${SUBCMD}，使用 help 查看用法" ;;
esac
