package updater

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
	"github.com/dowork-shanqiu/xuannexus-agent/internal/agent/config"
)

func TestNormalizeVersion(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"v1.0.0", "1.0.0"},
		{"1.0.0", "1.0.0"},
		{"v2.3.4", "2.3.4"},
		{" v1.0.0 ", "1.0.0"},
		{"", ""},
	}

	for _, tt := range tests {
		result := normalizeVersion(tt.input)
		if result != tt.expected {
			t.Errorf("normalizeVersion(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		current  string
		latest   string
		expected bool
	}{
		{"1.0.0", "1.0.1", true},
		{"1.0.0", "1.1.0", true},
		{"1.0.0", "2.0.0", true},
		{"1.0.0", "1.0.0", false},
		{"1.1.0", "1.0.0", false},
		{"2.0.0", "1.9.9", false},
		{"v1.0.0", "v1.0.1", true},
		{"v1.0.0", "v1.0.0", false},
		{"1.2.3", "1.2.4", true},
		{"1.2.3", "1.3.0", true},
		{"1.2.3", "1.2.3", false},
		{"1.2.3", "1.2.2", false},
		{"0.9.0", "1.0.0", true},
	}

	for _, tt := range tests {
		result := isNewerVersion(tt.current, tt.latest)
		if result != tt.expected {
			t.Errorf("isNewerVersion(%q, %q) = %v, want %v", tt.current, tt.latest, result, tt.expected)
		}
	}
}

func TestParseVersionParts(t *testing.T) {
	tests := []struct {
		input    string
		expected [3]int
	}{
		{"1.0.0", [3]int{1, 0, 0}},
		{"v2.3.4", [3]int{2, 3, 4}},
		{"10.20.30", [3]int{10, 20, 30}},
		{"", [3]int{0, 0, 0}},
		{"invalid", [3]int{0, 0, 0}},
	}

	for _, tt := range tests {
		result := parseVersionParts(tt.input)
		if result != tt.expected {
			t.Errorf("parseVersionParts(%q) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestBuildBinaryName(t *testing.T) {
	name := buildBinaryName()
	if name == "" {
		t.Error("buildBinaryName() returned empty string")
	}
	// Should contain OS and architecture
	if len(name) < 10 {
		t.Errorf("buildBinaryName() = %q, seems too short", name)
	}
}

func TestCheckLatestVersionFromGitHub(t *testing.T) {
	// Create a mock GitHub API server
	release := githubRelease{
		TagName: "v2.0.0",
		Assets: []githubAsset{
			{
				Name:               buildBinaryName(),
				BrowserDownloadURL: "https://github.com/test/repo/releases/download/v2.0.0/" + buildBinaryName(),
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(release)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	cfg := &config.Config{
		Upgrade: config.UpgradeConfig{
			Enabled:    true,
			GithubRepo: "test/repo",
		},
	}

	u := NewUpdater(cfg, logger, "1.0.0", nil)
	// Override HTTP client to use test server (we can't override the URL directly,
	// so test the mirror path which allows custom URLs)
	cfg.Upgrade.MirrorURL = server.URL

	ctx := context.Background()
	version, downloadURL, err := u.checkLatestVersion(ctx)
	if err != nil {
		t.Fatalf("checkLatestVersion() error: %v", err)
	}

	if version != "2.0.0" {
		t.Errorf("version = %q, want %q", version, "2.0.0")
	}

	if downloadURL == "" {
		t.Error("downloadURL is empty")
	}
}

func TestCheckLatestVersionFromMirror(t *testing.T) {
	binaryName := buildBinaryName()
	browserDownloadURL := "https://github.com/test/repo/releases/download/v3.1.0/" + binaryName
	release := githubRelease{
		TagName: "v3.1.0",
		Assets: []githubAsset{
			{
				Name:               binaryName,
				BrowserDownloadURL: browserDownloadURL,
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 镜像代理将真实 GitHub URL 拼接在镜像地址后，接受任意路径
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(release)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	cfg := &config.Config{
		Upgrade: config.UpgradeConfig{
			Enabled:    true,
			MirrorURL:  server.URL,
			GithubRepo: "test/repo",
		},
	}

	u := NewUpdater(cfg, logger, "1.0.0", nil)

	ctx := context.Background()
	version, downloadURL, err := u.checkLatestVersionFromMirror(ctx, binaryName)
	if err != nil {
		t.Fatalf("checkLatestVersionFromMirror() error: %v", err)
	}

	if version != "3.1.0" {
		t.Errorf("version = %q, want %q", version, "3.1.0")
	}

	expectedURL := server.URL + "/" + browserDownloadURL
	if downloadURL != expectedURL {
		t.Errorf("downloadURL = %q, want %q", downloadURL, expectedURL)
	}
}

func TestCheckLatestVersionFromMirror_WithHeaders(t *testing.T) {
	binaryName := buildBinaryName()
	browserDownloadURL := "https://github.com/test/repo/releases/download/v4.0.0/" + binaryName
	release := githubRelease{
		TagName: "v4.0.0",
		Assets: []githubAsset{
			{
				Name:               binaryName,
				BrowserDownloadURL: browserDownloadURL,
			},
		},
	}

	var receivedToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.Header.Get("X-XN-Token")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(release)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	cfg := &config.Config{
		Upgrade: config.UpgradeConfig{
			Enabled:    true,
			MirrorURL:  server.URL,
			GithubRepo: "test/repo",
			MirrorHeaders: map[string]string{
				"X-XN-Token": "test-secret",
			},
		},
	}

	u := NewUpdater(cfg, logger, "1.0.0", nil)

	ctx := context.Background()
	_, _, err := u.checkLatestVersionFromMirror(ctx, binaryName)
	if err != nil {
		t.Fatalf("checkLatestVersionFromMirror() error: %v", err)
	}

	if receivedToken != "test-secret" {
		t.Errorf("X-XN-Token header = %q, want %q", receivedToken, "test-secret")
	}
}

func TestCheckLatestVersionFromMirror_ProxyURL(t *testing.T) {
	binaryName := buildBinaryName()
	release := githubRelease{
		TagName: "v5.0.0",
		Assets: []githubAsset{
			{
				Name:               binaryName,
				BrowserDownloadURL: "https://github.com/test/repo/releases/download/v5.0.0/" + binaryName,
			},
		},
	}

	var receivedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(release)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	cfg := &config.Config{
		Upgrade: config.UpgradeConfig{
			Enabled:    true,
			MirrorURL:  server.URL,
			GithubRepo: "test/repo",
		},
	}

	u := NewUpdater(cfg, logger, "1.0.0", nil)

	ctx := context.Background()
	_, _, err := u.checkLatestVersionFromMirror(ctx, binaryName)
	if err != nil {
		t.Fatalf("checkLatestVersionFromMirror() error: %v", err)
	}

	// 验证请求路径为代理 GitHub API URL 的形式
	expectedPath := "/https://api.github.com/repos/test/repo/releases/latest"
	if receivedPath != expectedPath {
		t.Errorf("request path = %q, want %q", receivedPath, expectedPath)
	}
}

func TestCheckAndUpgrade_AlreadyLatest(t *testing.T) {
	// Mock server returns same version as current
	release := githubRelease{
		TagName: "v1.0.0",
		Assets: []githubAsset{
			{
				Name:               buildBinaryName(),
				BrowserDownloadURL: "https://example.com/download",
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(release)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	cfg := &config.Config{
		Upgrade: config.UpgradeConfig{
			Enabled:   true,
			MirrorURL: server.URL,
		},
	}

	u := NewUpdater(cfg, logger, "1.0.0", nil)

	ctx := context.Background()
	upgraded, err := u.CheckAndUpgrade(ctx)
	if err != nil {
		t.Fatalf("CheckAndUpgrade() error: %v", err)
	}
	if upgraded {
		t.Error("CheckAndUpgrade() = true, want false (same version)")
	}
}

func TestCheckAndUpgrade_ConcurrentProtection(t *testing.T) {
	// Slow server to test concurrent protection
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a slow response
		release := githubRelease{TagName: "v1.0.0"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(release)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	cfg := &config.Config{
		Upgrade: config.UpgradeConfig{
			Enabled:   true,
			MirrorURL: server.URL,
		},
	}

	u := NewUpdater(cfg, logger, "1.0.0", nil)

	// Manually set updating to true
	u.mu.Lock()
	u.updating = true
	u.mu.Unlock()

	ctx := context.Background()
	upgraded, err := u.CheckAndUpgrade(ctx)
	if err != nil {
		t.Fatalf("CheckAndUpgrade() error: %v", err)
	}
	if upgraded {
		t.Error("CheckAndUpgrade() should return false when already updating")
	}
}
