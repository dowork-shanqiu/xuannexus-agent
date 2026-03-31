// Package updater 实现 Agent 自升级功能，包括版本检测、下载、替换二进制和不停机重启。
package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"github.com/dowork-shanqiu/xuannexus-agent/internal/agent/config"
)

// githubRelease 表示 GitHub Release API 中的一个 release
type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

// githubAsset 表示 GitHub Release 中的一个 asset
type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Updater 负责检查更新、下载新版本并执行不停机升级。
type Updater struct {
	cfg            *config.Config
	logger         *zap.Logger
	currentVersion string
	httpClient     *http.Client

	mu         sync.Mutex
	updating   bool
	onRestart  func() // 重启回调（由外部提供）
}

// NewUpdater 创建一个新的 Updater 实例。
// currentVersion 是当前正在运行的版本号（如 "v1.0.0" 或 "1.0.0"）。
// onRestart 是升级完成后触发重启的回调函数。
func NewUpdater(cfg *config.Config, logger *zap.Logger, currentVersion string, onRestart func()) *Updater {
	return &Updater{
		cfg:            cfg,
		logger:         logger,
		currentVersion: normalizeVersion(currentVersion),
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
		onRestart: onRestart,
	}
}

// CheckAndUpgrade 检查是否有新版本可用，如果有则下载并执行升级。
// 返回 (是否已升级, error)。
func (u *Updater) CheckAndUpgrade(ctx context.Context) (bool, error) {
	u.mu.Lock()
	if u.updating {
		u.mu.Unlock()
		u.logger.Info("升级正在进行中，跳过本次检查")
		return false, nil
	}
	u.updating = true
	u.mu.Unlock()

	defer func() {
		u.mu.Lock()
		u.updating = false
		u.mu.Unlock()
	}()

	u.logger.Info("开始检查更新...", zap.String("current_version", u.currentVersion))

	// 1. 获取最新版本信息
	latestVersion, downloadURL, err := u.checkLatestVersion(ctx)
	if err != nil {
		return false, fmt.Errorf("检查最新版本失败: %w", err)
	}

	u.logger.Info("获取到最新版本信息",
		zap.String("latest_version", latestVersion),
		zap.String("current_version", u.currentVersion))

	// 2. 比较版本
	if !isNewerVersion(u.currentVersion, latestVersion) {
		u.logger.Info("当前已是最新版本，无需升级",
			zap.String("current", u.currentVersion),
			zap.String("latest", latestVersion))
		return false, nil
	}

	u.logger.Info("发现新版本，开始升级",
		zap.String("current", u.currentVersion),
		zap.String("latest", latestVersion),
		zap.String("download_url", downloadURL))

	// 3. 下载新版本
	newBinaryPath, err := u.downloadBinary(ctx, downloadURL)
	if err != nil {
		return false, fmt.Errorf("下载新版本失败: %w", err)
	}

	// 4. 替换当前二进制文件
	if err := u.replaceBinary(newBinaryPath); err != nil {
		// 清理下载的文件
		os.Remove(newBinaryPath)
		return false, fmt.Errorf("替换二进制文件失败: %w", err)
	}

	u.logger.Info("升级完成，准备重启",
		zap.String("new_version", latestVersion))

	// 5. 触发重启
	if u.onRestart != nil {
		go u.onRestart()
	}

	return true, nil
}

// checkLatestVersion 检查最新版本，返回 (版本号, 下载URL, error)
func (u *Updater) checkLatestVersion(ctx context.Context) (string, string, error) {
	binaryName := buildBinaryName()

	// 如果配置了镜像地址，从镜像获取版本信息
	if u.cfg.Upgrade.MirrorURL != "" {
		return u.checkLatestVersionFromMirror(ctx, binaryName)
	}

	// 否则从 GitHub Release API 获取
	return u.checkLatestVersionFromGitHub(ctx, binaryName)
}

// checkLatestVersionFromGitHub 从 GitHub Release API 获取最新版本
func (u *Updater) checkLatestVersionFromGitHub(ctx context.Context, binaryName string) (string, string, error) {
	repo := u.cfg.Upgrade.GithubRepo
	if repo == "" {
		repo = "dowork-shanqiu/xuannexus-agent"
	}

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "xuannexus-agent-updater")

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("请求 GitHub API 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", "", fmt.Errorf("GitHub API 返回非 200 状态码: %d, body: %s", resp.StatusCode, string(body))
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", "", fmt.Errorf("解析 GitHub Release 响应失败: %w", err)
	}

	latestVersion := normalizeVersion(release.TagName)

	// 查找匹配当前平台的 asset
	for _, asset := range release.Assets {
		if asset.Name == binaryName {
			return latestVersion, asset.BrowserDownloadURL, nil
		}
	}

	return "", "", fmt.Errorf("未找到适配当前平台的二进制文件: %s", binaryName)
}

// checkLatestVersionFromMirror 从镜像获取最新版本
// 镜像需要提供一个 latest.json 文件，格式与 GitHub Release API 的 /latest 端点兼容
func (u *Updater) checkLatestVersionFromMirror(ctx context.Context, binaryName string) (string, string, error) {
	mirrorURL := strings.TrimRight(u.cfg.Upgrade.MirrorURL, "/")

	// 从镜像获取最新版本信息
	latestURL := mirrorURL + "/latest.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("创建镜像请求失败: %w", err)
	}
	req.Header.Set("User-Agent", "xuannexus-agent-updater")

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("请求镜像版本信息失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", "", fmt.Errorf("镜像返回非 200 状态码: %d, body: %s", resp.StatusCode, string(body))
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", "", fmt.Errorf("解析镜像版本信息失败: %w", err)
	}

	latestVersion := normalizeVersion(release.TagName)

	// 构建下载 URL：{mirror_url}/{tag}/{binary_name}
	downloadURL := fmt.Sprintf("%s/%s/%s", mirrorURL, release.TagName, binaryName)

	return latestVersion, downloadURL, nil
}

// downloadBinary 下载新版本二进制文件到临时路径
func (u *Updater) downloadBinary(ctx context.Context, downloadURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("创建下载请求失败: %w", err)
	}
	req.Header.Set("User-Agent", "xuannexus-agent-updater")

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载返回非 200 状态码: %d", resp.StatusCode)
	}

	// 写入临时文件
	tmpDir := u.cfg.FileTransfer.TempDir
	if tmpDir == "" {
		tmpDir = os.TempDir()
	}
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", fmt.Errorf("创建临时目录失败: %w", err)
	}

	tmpFile, err := os.CreateTemp(tmpDir, "xuannexus-agent-update-*")
	if err != nil {
		return "", fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpPath := tmpFile.Name()

	written, err := io.Copy(tmpFile, resp.Body)
	tmpFile.Close()
	if err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("写入临时文件失败: %w", err)
	}

	u.logger.Info("新版本下载完成",
		zap.String("path", tmpPath),
		zap.Int64("size", written))

	// 设置可执行权限
	if err := os.Chmod(tmpPath, 0755); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("设置可执行权限失败: %w", err)
	}

	return tmpPath, nil
}

// replaceBinary 替换当前正在运行的二进制文件
// 使用 rename 策略实现原子替换：
// 1. 将当前文件重命名为 .old
// 2. 将新文件 rename 到当前文件路径
// 3. 删除 .old 文件
func (u *Updater) replaceBinary(newBinaryPath string) error {
	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取当前可执行文件路径失败: %w", err)
	}

	// 解析符号链接，获取真实路径
	currentExe, err = filepath.EvalSymlinks(currentExe)
	if err != nil {
		return fmt.Errorf("解析可执行文件符号链接失败: %w", err)
	}

	oldPath := currentExe + ".old"

	// 删除可能存在的旧备份
	os.Remove(oldPath)

	// 将当前二进制重命名为 .old
	if err := os.Rename(currentExe, oldPath); err != nil {
		return fmt.Errorf("备份当前二进制文件失败: %w", err)
	}

	// 将新二进制重命名到目标路径
	if err := os.Rename(newBinaryPath, currentExe); err != nil {
		// 回滚：恢复旧文件
		if rbErr := os.Rename(oldPath, currentExe); rbErr != nil {
			u.logger.Error("回滚失败", zap.Error(rbErr))
		}
		return fmt.Errorf("替换二进制文件失败: %w", err)
	}

	// 清理旧文件（允许失败，不影响升级）
	if err := os.Remove(oldPath); err != nil {
		u.logger.Warn("清理旧二进制文件失败（不影响升级）", zap.Error(err))
	}

	u.logger.Info("二进制文件替换完成", zap.String("path", currentExe))
	return nil
}

// buildBinaryName 构建当前平台对应的二进制文件名
func buildBinaryName() string {
	osName := runtime.GOOS
	arch := runtime.GOARCH
	ext := ""
	if osName == "windows" {
		ext = ".exe"
	}
	return fmt.Sprintf("xuannexus-agent-%s-%s%s", osName, arch, ext)
}

// normalizeVersion 规范化版本号，去除前缀 "v"
func normalizeVersion(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

// isNewerVersion 比较两个语义版本号，判断 latest 是否比 current 更新。
// 支持 major.minor.patch 格式。
func isNewerVersion(current, latest string) bool {
	currentParts := parseVersionParts(current)
	latestParts := parseVersionParts(latest)

	for i := 0; i < 3; i++ {
		if latestParts[i] > currentParts[i] {
			return true
		}
		if latestParts[i] < currentParts[i] {
			return false
		}
	}
	return false
}

// parseVersionParts 将版本字符串解析为 [major, minor, patch] 数组
func parseVersionParts(v string) [3]int {
	v = normalizeVersion(v)
	var parts [3]int
	n, _ := fmt.Sscanf(v, "%d.%d.%d", &parts[0], &parts[1], &parts[2])
	// 如果解析失败，至少解析了部分
	_ = n
	return parts
}
