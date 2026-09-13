package deployer

import (
	"context"
	"strings"
	"testing"
	"time"

	"multi-service-deploy/config"
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
	resAll, err := mgrAll.filterServices()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resAll) != 3 {
		t.Fatalf("expected 3 services, got %d", len(resAll))
	}

	// 2. 指定只部署 web-2
	mgrFiltered := NewDeployManager(cfg, DeployOptions{
		TargetServices: []string{"web-2"},
	})
	resFiltered, err := mgrFiltered.filterServices()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resFiltered) != 1 || resFiltered[0].Name != "web-2" {
		t.Fatalf("expected only 'web-2', got %v", resFiltered)
	}
}

func TestFilterServicesWithTags(t *testing.T) {
	cfg := &config.DeployConfig{
		Services: []config.ServiceConfig{
			{Name: "web-1", Tags: []string{"frontend", "cdn"}, Type: "standard"},
			{Name: "api-1", Tags: []string{"backend"}, Type: "standard"},
			{Name: "db-1", Tags: []string{"infra", "data"}, Type: "exec_only"},
		},
	}

	// 1. 单标签过滤
	mgrTag := NewDeployManager(cfg, DeployOptions{
		TargetTags: []string{"frontend"},
	})
	resTag, err := mgrTag.filterServices()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resTag) != 1 || resTag[0].Name != "web-1" {
		t.Fatalf("expected only 'web-1', got %v", resTag)
	}

	// 2. 多标签并集语义：backend 或 data 任一命中
	mgrUnion := NewDeployManager(cfg, DeployOptions{
		TargetTags: []string{"backend", "data"},
	})
	resUnion, err := mgrUnion.filterServices()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resUnion) != 2 || resUnion[0].Name != "api-1" || resUnion[1].Name != "db-1" {
		t.Fatalf("expected union of 'api-1' and 'db-1', got %v", resUnion)
	}

	// 3. 大小写不敏感
	mgrCase := NewDeployManager(cfg, DeployOptions{
		TargetTags: []string{"BACKEND"},
	})
	resCase, err := mgrCase.filterServices()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resCase) != 1 || resCase[0].Name != "api-1" {
		t.Fatalf("expected case-insensitive tag match on 'api-1', got %v", resCase)
	}

	// 4. 按 Type 过滤
	mgrType := NewDeployManager(cfg, DeployOptions{
		TargetTypes: []string{"exec_only"},
	})
	resType, err := mgrType.filterServices()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resType) != 1 || resType[0].Name != "db-1" {
		t.Fatalf("expected only 'db-1', got %v", resType)
	}
}

func TestStageExecutionAndCircuitBreaker(t *testing.T) {
	buf := captureDeployerLog(t)

	// Stage 1 服务由于无效主机失败，Stage 2 服务应当被熔断跳过
	cfg := &config.DeployConfig{
		Services: []config.ServiceConfig{
			{
				Name:  "stage1-svc",
				Tags:  []string{"infra"},
				Stage: 1,
				Server: config.ServerConfig{
					Host:           "127.0.0.1",
					Port:           1, // 无效端口，快速失败
					Username:       "root",
					Password:       "pwd",
					ConnectTimeout: 1,
				},
			},
			{
				Name:  "stage2-svc",
				Tags:  []string{"backend"},
				Stage: 2,
				Server: config.ServerConfig{
					Host:           "127.0.0.1",
					Port:           22,
					Username:       "root",
					Password:       "pwd",
					ConnectTimeout: 1,
				},
			},
		},
	}

	mgr := NewDeployManager(cfg, DeployOptions{})
	allSuccess, err := mgr.RunWithContext(context.Background())
	if allSuccess {
		t.Fatalf("expected allSuccess to be false due to stage 1 failure")
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Aborting subsequent stages") {
		t.Errorf("expected circuit breaker abort message, got %s", out)
	}
}

func TestPrintSummary(t *testing.T) {
	buf := captureDeployerLog(t)

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

func TestTagPreDeployHookExecutionAndAbort(t *testing.T) {
	cfg := &config.DeployConfig{
		TagHooks: []config.TagHookConfig{
			{
				Name: "frontend",
				Hooks: config.BatchHooks{
					PreDeploy: []string{"exit 1"}, // 前端标签构建命令失败
				},
			},
			{
				Name: "backend",
				Hooks: config.BatchHooks{
					PreDeploy: []string{"echo 'backend pre ok'"},
				},
			},
		},
		Services: []config.ServiceConfig{
			{
				Name: "web-1",
				Tags: []string{"frontend"},
				Server: config.ServerConfig{
					Host:     "127.0.0.1",
					Username: "root",
					Password: "pwd",
				},
			},
		},
	}

	mgr := NewDeployManager(cfg, DeployOptions{})
	allSuccess, err := mgr.RunWithContext(context.Background())
	if allSuccess {
		t.Fatalf("expected allSuccess to be false when tag preDeploy fails")
	}
	if err == nil {
		t.Fatalf("expected error from tag preDeploy failure")
	}
	if !strings.Contains(err.Error(), "frontend") || !strings.Contains(err.Error(), "pre-deploy hook failed") {
		t.Errorf("expected frontend tag pre-deploy hook failed error, got %v", err)
	}
}

func TestGlobalPreDeployHookAbort(t *testing.T) {
	cfg := &config.DeployConfig{
		Hooks: config.BatchHooks{
			PreDeploy: []string{"exit 1"}, // 模拟前置命令失败
		},
		Services: []config.ServiceConfig{
			{Name: "svc-should-not-run", Server: config.ServerConfig{Host: "127.0.0.1", Username: "root", Password: "123"}},
		},
	}

	mgr := NewDeployManager(cfg, DeployOptions{})
	allSuccess, err := mgr.RunWithContext(context.Background())
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
