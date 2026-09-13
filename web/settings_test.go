package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"multi-service-deploy/config"
)

func TestSettingsGetDefaults(t *testing.T) {
	srv, _ := newTestServer(t)

	w := httptest.NewRecorder()
	srv.handleSettings(w, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for settings GET, got %d", w.Code)
	}

	var view settingsView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatalf("failed to parse settings view: %v", err)
	}
	if view.MaxWorkers != config.DefaultMaxWorkers {
		t.Errorf("expected default maxWorkers %d, got %d", config.DefaultMaxWorkers, view.MaxWorkers)
	}
	if view.ListenAddrDefault != config.DefaultListenAddr {
		t.Errorf("expected listenAddrDefault %s, got %s", config.DefaultListenAddr, view.ListenAddrDefault)
	}
}

func TestSettingsSaveAndRestartHint(t *testing.T) {
	srv, wsDir := newTestServer(t)

	body := `{"maxWorkers":5,"listenAddr":"127.0.0.1:9090","autoOpen":false}`
	w := httptest.NewRecorder()
	srv.handleSettings(w, httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for settings save, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Status          string `json:"status"`
		RestartRequired bool   `json:"restartRequired"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse save response: %v", err)
	}
	if resp.Status != "ok" || !resp.RestartRequired {
		t.Errorf("expected ok + restartRequired=true, got %+v", resp)
	}

	// 设置已持久化到 settings.json
	if _, err := os.Stat(filepath.Join(wsDir, config.SettingsFileName)); err != nil {
		t.Errorf("expected settings.json persisted in workspace dir: %v", err)
	}

	// GET 回读确认
	wGet := httptest.NewRecorder()
	srv.handleSettings(wGet, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	var view settingsView
	_ = json.Unmarshal(wGet.Body.Bytes(), &view)
	if view.MaxWorkers != 5 || view.ListenAddrSetting != "127.0.0.1:9090" || !view.ListenAddrRestart {
		t.Errorf("expected saved settings to round-trip, got %+v", view)
	}
	if view.AutoOpenBrowser {
		t.Errorf("expected autoOpen=false to round-trip, got %+v", view)
	}
}

func TestSettingsValidation(t *testing.T) {
	srv, _ := newTestServer(t)

	// 非法监听地址格式
	w1 := httptest.NewRecorder()
	srv.handleSettings(w1, httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(`{"listenAddr":"not-an-addr"}`)))
	if w1.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid listenAddr, got %d", w1.Code)
	}

	// 并发度越界
	w2 := httptest.NewRecorder()
	srv.handleSettings(w2, httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(`{"maxWorkers":99999}`)))
	if w2.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for out-of-range maxWorkers, got %d", w2.Code)
	}
}

func TestSettingsConflictWhileDeploying(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.isDeploying.Store(true)

	w := httptest.NewRecorder()
	srv.handleSettings(w, httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(`{"maxWorkers":5}`)))
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 while deploying, got %d", w.Code)
	}
	srv.isDeploying.Store(false)
}

func TestActiveWorkspacePersistAcrossRestart(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "deploy.json")

	// 准备两个工作空间配置文件
	if err := os.MkdirAll(filepath.Join(tmpDir, "workspaces"), 0755); err != nil {
		t.Fatalf("mkdir workspaces: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "workspaces", "default.json"), []byte(`{"services":[{"name":"a","server":{"host":"10.0.0.1","username":"r","password":"p"}}]}`), 0644); err != nil {
		t.Fatalf("seed default ws: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "workspaces", "staging.json"), []byte(`{"services":[{"name":"b","server":{"host":"10.0.0.2","username":"r","password":"p"}}]}`), 0644); err != nil {
		t.Fatalf("seed staging ws: %v", err)
	}

	srv := NewServer(":0", configPath)
	reqSelect := httptest.NewRequest(http.MethodPost, "/api/workspaces/select", strings.NewReader(`{"workspace":"staging"}`))
	wSelect := httptest.NewRecorder()
	srv.handleWorkspaceSelect(wSelect, reqSelect)
	if wSelect.Code != http.StatusOK {
		t.Fatalf("expected 200 on select, got %d: %s", wSelect.Code, wSelect.Body.String())
	}

	// 模拟进程重启：新建 Server 实例应从 settings.json 恢复活动空间
	restarted := NewServer(":0", configPath)
	if restarted.getCurrentWorkspace() != "staging" {
		t.Errorf("expected active workspace staging restored after restart, got %q", restarted.getCurrentWorkspace())
	}
}
