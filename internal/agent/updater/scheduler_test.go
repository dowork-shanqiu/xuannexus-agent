package updater

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"
	"github.com/dowork-shanqiu/xuannexus-agent/internal/agent/config"
)

func TestScheduler_DisabledUpgrade(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cfg := &config.Config{
		Upgrade: config.UpgradeConfig{
			Enabled: false,
		},
	}

	u := NewUpdater(cfg, logger, "1.0.0", nil)
	s := NewScheduler(u, cfg, logger)

	ctx := context.Background()
	err := s.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Should work fine even if disabled
	s.Stop()
}

func TestScheduler_InvalidCron(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cfg := &config.Config{
		Upgrade: config.UpgradeConfig{
			Enabled:  true,
			Schedule: "invalid cron expression",
		},
	}

	u := NewUpdater(cfg, logger, "1.0.0", nil)
	s := NewScheduler(u, cfg, logger)

	ctx := context.Background()
	err := s.Start(ctx)
	if err == nil {
		t.Error("Start() should return error for invalid cron expression")
		s.Stop()
	}
}

func TestScheduler_ValidCron(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cfg := &config.Config{
		Upgrade: config.UpgradeConfig{
			Enabled:  true,
			Schedule: "0 3 * * *",
		},
	}

	u := NewUpdater(cfg, logger, "1.0.0", nil)
	s := NewScheduler(u, cfg, logger)

	ctx, cancel := context.WithCancel(context.Background())
	err := s.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Stop after a short period
	time.Sleep(100 * time.Millisecond)
	cancel()
	s.Stop()
}

func TestScheduler_TriggerCheck(t *testing.T) {
	binaryName := buildBinaryName()
	release := githubRelease{
		TagName: "v1.0.0",
		Assets: []githubAsset{
			{
				Name:               binaryName,
				BrowserDownloadURL: "https://github.com/test/repo/releases/download/v1.0.0/" + binaryName,
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
			Schedule:  "0 3 * * *",
			MirrorURL: server.URL,
		},
	}

	u := NewUpdater(cfg, logger, "1.0.0", nil)
	s := NewScheduler(u, cfg, logger)

	ctx := context.Background()
	upgraded, err := s.TriggerCheck(ctx)
	if err != nil {
		t.Fatalf("TriggerCheck() error: %v", err)
	}
	if upgraded {
		t.Error("TriggerCheck() = true, want false (same version)")
	}
}
