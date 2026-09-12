package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"multi-service-deploy/config"
	"multi-service-deploy/deployer"
	"multi-service-deploy/logger"
)

// newTestServer 在临时目录创建 Server，返回 Server 与工作空间根目录
func newTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	tmpDir := t.TempDir()
	srv := NewServer(":0", filepath.Join(tmpDir, "deploy.json"))
	return srv, config.ResolveWorkspaceDir(filepath.Join(tmpDir, "deploy.json"))
}

func TestBatchCollectorRescuePersist(t *testing.T) {
	srv, wsDir := newTestServer(t)

	c := srv.history.begin("default", "20260912-080000")
	c.observe(deployer.EventBatchStarted, deployer.BatchStartedPayload{
		ID: "20260912-080000", Workspace: "default", Total: 2,
		Services: []deployer.ServiceNodeInput{
			{Name: "svc-1", Host: "h1:22"}, {Name: "svc-2", Host: "h2:22"},
		},
	})
	c.observe(deployer.EventServiceFinished, deployer.ServiceOutcome{Name: "svc-1", Status: deployer.ServiceStatusOK, DurationMs: 100})
	c.observe(deployer.EventServiceFinished, deployer.ServiceOutcome{Name: "svc-2", Status: deployer.ServiceStatusFailed, Error: "SSH connection failed: boom"})
	srv.history.end(c, deployer.BatchStatusFailed)

	data, err := os.ReadFile(filepath.Join(wsDir, "default", "history", "batch-20260912-080000.json"))
	if err != nil {
		t.Fatalf("expected batch record persisted, got error: %v", err)
	}
	var rec deployer.BatchRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("failed to parse batch record: %v", err)
	}
	if rec.ID != "20260912-080000" || rec.Total != 2 || rec.Success != 1 || rec.Failed != 1 {
		t.Errorf("unexpected record summary: %+v", rec)
	}
	if rec.Status != deployer.BatchStatusFailed {
		t.Errorf("expected status failed, got %s", rec.Status)
	}
	if len(rec.Services) != 2 || rec.Services[1].Error == "" {
		t.Errorf("expected 2 service outcomes with error preserved, got %+v", rec.Services)
	}
}

func TestBatchCollectorBatchFinishedIdempotent(t *testing.T) {
	srv, wsDir := newTestServer(t)

	c := srv.history.begin("default", "20260912-080001")
	c.observe(deployer.EventBatchStarted, deployer.BatchStartedPayload{ID: "20260912-080001", Workspace: "default", Total: 1})
	record := deployer.BatchRecord{
		ID: "20260912-080001", Workspace: "default", Start: time.Now(),
		Status: deployer.BatchStatusSuccess, Total: 1, Success: 1,
		Services: []deployer.ServiceOutcome{{Name: "svc-1", Status: deployer.ServiceStatusOK}},
	}
	c.observe(deployer.EventBatchFinished, record)
	// end 的状态提示不得覆盖 batch_finished 已确立的终态
	srv.history.end(c, deployer.BatchStatusCanceled)

	data, err := os.ReadFile(filepath.Join(wsDir, "default", "history", "batch-20260912-080001.json"))
	if err != nil {
		t.Fatalf("expected batch record persisted: %v", err)
	}
	var rec deployer.BatchRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("failed to parse batch record: %v", err)
	}
	if rec.Status != deployer.BatchStatusSuccess {
		t.Errorf("expected status success to be preserved, got %s", rec.Status)
	}
}

func TestHistoryTeeLog(t *testing.T) {
	srv, wsDir := newTestServer(t)

	c := srv.history.begin("default", "20260912-080002")
	c.observe(deployer.EventBatchStarted, deployer.BatchStartedPayload{ID: "20260912-080002", Workspace: "default", Total: 1})
	srv.history.teeLog("[08:00:01] [DEPLOY] hello from tee")
	srv.history.end(c, deployer.BatchStatusSuccess)
	// 无活跃批次后 teeLog 应为空操作且不 panic
	srv.history.teeLog("[08:00:02] [DEPLOY] after end")

	data, err := os.ReadFile(filepath.Join(wsDir, "default", "history", "batch-20260912-080002.log"))
	if err != nil {
		t.Fatalf("expected batch log archived: %v", err)
	}
	if !strings.Contains(string(data), "hello from tee") {
		t.Errorf("expected archived log to contain tee'd line, got:\n%s", data)
	}
	if strings.Contains(string(data), "after end") {
		t.Errorf("expected teeLog to be no-op after collector ended")
	}
}

// TestDeployTeeLogNoDeadlock 回归测试：批次归档日志链路（logger.OnLog → teeLog）
// 曾在持久化回调持锁期间引发 c.mu 自死锁，导致部署 goroutine 冻结、isDeploying 永久为 true。
// 本测试接通真实日志回调后运行完整部署，断言部署 goroutine 最终正常退出。
func TestDeployTeeLogNoDeadlock(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "deploy.json")
	cfgJSON := `{"services":[{"name":"mock-svc","server":{"host":"127.0.0.1","port":1,"username":"r","password":"p","connectTimeout":1}}]}`
	if err := os.WriteFile(configPath, []byte(cfgJSON), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	srv := NewServer(":0", configPath)
	// 模拟 StartContext 的日志接线（该链路是死锁的触发前提）
	prev := logger.OnLog
	logger.OnLog = func(line string) { srv.history.teeLog(line) }
	defer func() { logger.OnLog = prev }()

	req := httptest.NewRequest(http.MethodPost, "/api/deploy", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	srv.handleDeploy(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 started, got %d: %s", w.Code, w.Body.String())
	}

	deadline := time.Now().Add(10 * time.Second)
	for srv.isDeploying.Load() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if srv.isDeploying.Load() {
		t.Fatalf("deploy goroutine deadlocked: isDeploying still true after %v", 10*time.Second)
	}

	// 批次记录与日志均应已归档
	wsDir := config.ResolveWorkspaceDir(configPath)
	entries, err := os.ReadDir(filepath.Join(wsDir, "default", "history"))
	if err != nil || len(entries) < 2 {
		t.Fatalf("expected batch json+log archived, entries=%d err=%v", len(entries), err)
	}
}

func TestDeployHistoryEndpoints(t *testing.T) {
	srv, wsDir := newTestServer(t)
	histDir := filepath.Join(wsDir, "prod", "history")
	if err := os.MkdirAll(histDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	record := deployer.BatchRecord{
		ID: "20260912-090000", Workspace: "prod", Start: time.Now(), End: time.Now(),
		DurationMs: 1500, Total: 2, Success: 2, Status: deployer.BatchStatusSuccess,
		Services: []deployer.ServiceOutcome{{Name: "a", Status: deployer.ServiceStatusOK}},
	}
	data, _ := json.MarshalIndent(record, "", "  ")
	if err := os.WriteFile(filepath.Join(histDir, "batch-20260912-090000.json"), data, 0644); err != nil {
		t.Fatalf("write record: %v", err)
	}

	// 1. 列表端点
	reqList := httptest.NewRequest(http.MethodGet, "/api/deploy/history?workspace=prod", nil)
	wList := httptest.NewRecorder()
	srv.handleDeployHistory(wList, reqList)
	if wList.Code != http.StatusOK {
		t.Fatalf("expected 200 for history list, got %d: %s", wList.Code, wList.Body.String())
	}
	if !strings.Contains(wList.Body.String(), `"id":"20260912-090000"`) {
		t.Errorf("expected history list to contain batch id, got %s", wList.Body.String())
	}

	// 2. 详情端点
	reqDetail := httptest.NewRequest(http.MethodGet, "/api/deploy/history/20260912-090000?workspace=prod", nil)
	reqDetail.SetPathValue("id", "20260912-090000")
	wDetail := httptest.NewRecorder()
	srv.handleDeployHistoryDetail(wDetail, reqDetail)
	if wDetail.Code != http.StatusOK {
		t.Fatalf("expected 200 for history detail, got %d: %s", wDetail.Code, wDetail.Body.String())
	}
	if !strings.Contains(wDetail.Body.String(), `"services":[{`) || !strings.Contains(wDetail.Body.String(), `"status":"success"`) {
		t.Errorf("expected full record fields in detail response, got %s", wDetail.Body.String())
	}

	// 3. 非法批次 ID 拦截（防路径穿透）
	reqBad := httptest.NewRequest(http.MethodGet, "/api/deploy/history/..%2Fetc", nil)
	reqBad.SetPathValue("id", "../etc")
	wBad := httptest.NewRecorder()
	srv.handleDeployHistoryDetail(wBad, reqBad)
	if wBad.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for path traversal batch id, got %d", wBad.Code)
	}

	// 4. 不存在的批次返回 404
	reqMiss := httptest.NewRequest(http.MethodGet, "/api/deploy/history/20990101-000000", nil)
	reqMiss.SetPathValue("id", "20990101-000000")
	wMiss := httptest.NewRecorder()
	srv.handleDeployHistoryDetail(wMiss, reqMiss)
	if wMiss.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing batch, got %d", wMiss.Code)
	}
}
