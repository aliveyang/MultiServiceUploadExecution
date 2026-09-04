package deployer

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"multi-service-deploy/config"
	"multi-service-deploy/logger"
)

func TestFilterServices(t *testing.T) {
	cfg := &config.DeployConfig{
		Services: []config.ServiceConfig{
			{Name: "web-1"},
			{Name: "web-2"},
			{Name: "api-1"},
		},
	}

	// 1. 无指定目标时返回全部
	mgrAll := NewDeployManager(cfg, DeployOptions{})
	resAll := mgrAll.filterServices()
	if len(resAll) != 3 {
		t.Fatalf("expected 3 services, got %d", len(resAll))
	}

	// 2. 指定只部署 web-2
	mgrFiltered := NewDeployManager(cfg, DeployOptions{
		TargetServices: []string{"web-2"},
	})
	resFiltered := mgrFiltered.filterServices()
	if len(resFiltered) != 1 || resFiltered[0].Name != "web-2" {
		t.Fatalf("expected only 'web-2', got %v", resFiltered)
	}
}

func TestPrintSummary(t *testing.T) {
	buf := &bytes.Buffer{}
	logger.SetOutput(buf)

	results := []ServiceResult{
		{
			ServiceName: "svc-1",
			Host:        "1.1.1.1:22",
			Success:     true,
			Duration:    100 * time.Millisecond,
		},
		{
			ServiceName: "svc-2",
			Host:        "1.1.1.2:22",
			Success:     false,
			Duration:    50 * time.Millisecond,
		},
	}

	ok := PrintSummary(results, 150*time.Millisecond)
	if ok {
		t.Errorf("expected overall success to be false because svc-2 failed")
	}

	out := buf.String()
		if !strings.Contains(out, "DEPLOYMENT SUMMARY") {
			t.Errorf("expected summary header, got %q", out)
		}
	}

	func TestRunWithContextCancellation(t *testing.T) {
		buf := &bytes.Buffer{}
		logger.SetOutput(buf)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // 立即取消

		cfg := &config.DeployConfig{
			Services: []config.ServiceConfig{
				{Name: "svc-canceled", Server: config.ServerConfig{Host: "127.0.0.1", Username: "root", Password: "123"}},
			},
		}

		mgr := NewDeployManager(cfg, DeployOptions{})
		allSuccess, err := mgr.RunWithContext(ctx)
		if allSuccess {
			t.Errorf("expected allSuccess to be false when context is canceled")
		}
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	}

	func TestGlobalPreDeployHookAbort(t *testing.T) {
		buf := &bytes.Buffer{}
		logger.SetOutput(buf)

		cfg := &config.DeployConfig{
			Hooks: config.GlobalHooks{
				PreDeploy: []string{"exit 1"}, // 模拟前置命令失败
			},
			Services: []config.ServiceConfig{
				{Name: "svc-should-not-run", Server: config.ServerConfig{Host: "127.0.0.1", Username: "root", Password: "123"}},
			},
		}

		mgr := NewDeployManager(cfg, DeployOptions{})
		allSuccess, err := mgr.Run()
		if allSuccess {
			t.Fatalf("expected allSuccess to be false when preDeploy fails")
		}
		if err == nil {
			t.Fatalf("expected error when preDeploy fails, got nil")
		}
		if !strings.Contains(err.Error(), "pre-deploy hook failed") {
			t.Errorf("expected pre-deploy hook failed error, got %v", err)
		}
	}
