package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"multi-service-deploy/config"
)

func TestWebServerEndpoints(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "deploy.json")

	srv := NewServer(":0", configPath)

	// 1. 测试静态首页加载
	reqIndex := httptest.NewRequest(http.MethodGet, "/", nil)
	wIndex := httptest.NewRecorder()
	subFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		t.Fatalf("failed to sub static files: %v", err)
	}
	http.FileServer(http.FS(subFS)).ServeHTTP(wIndex, reqIndex)

	if wIndex.Code != http.StatusOK {
		t.Fatalf("expected status 200 for index, got %d", wIndex.Code)
	}
	if !strings.Contains(wIndex.Body.String(), "多服务器部署控制台") {
		t.Errorf("expected index to contain tool title")
	}

	// 2. 测试获取默认配置
	reqGet := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	wGet := httptest.NewRecorder()
	srv.handleConfig(wGet, reqGet)
	if wGet.Code != http.StatusOK {
		t.Fatalf("expected status 200 for GET config, got %d", wGet.Code)
	}
	if !strings.Contains(wGet.Body.String(), "services") {
		t.Errorf("expected config response to contain 'services'")
	}

	// 3. 测试保存新配置
	newCfgJSON := `{
		"parallel": true,
		"services": [
			{
				"name": "api-web-test",
				"server": {
					"host": "10.0.0.1",
					"username": "root",
					"password": "pass"
				}
			}
		]
	}`
	reqPost := httptest.NewRequest(http.MethodPost, "/api/config", bytes.NewBufferString(newCfgJSON))
	wPost := httptest.NewRecorder()
	srv.handleConfig(wPost, reqPost)
	if wPost.Code != http.StatusOK {
		t.Fatalf("expected status 200 for POST config, got %d: %s", wPost.Code, wPost.Body.String())
	}

	savedBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}
		if !strings.Contains(string(savedBytes), "api-web-test") {
			t.Errorf("saved config missing 'api-web-test'")
		}
	}

	func TestDeployConcurrencyConflict(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "deploy.json")

		cfgJSON := `{
			"services": [
				{
					"name": "mock-svc",
					"server": {
						"host": "127.0.0.1",
						"username": "root",
						"password": "pwd"
					}
				}
			]
		}`
		if err := os.WriteFile(configPath, []byte(cfgJSON), 0644); err != nil {
			t.Fatalf("failed to write config: %v", err)
		}

		srv := NewServer(":0", configPath)
		// 模拟当前已有任务在部署中
		srv.isDeploying.Store(true)

		req := httptest.NewRequest(http.MethodPost, "/api/deploy", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		srv.handleDeploy(w, req)

		if w.Code != http.StatusConflict {
			t.Fatalf("expected HTTP 409 Conflict when already deploying, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "already running") {
			t.Errorf("expected error message to contain 'already running', got %q", w.Body.String())
		}
	}

	func TestConfigMaskAndPreserveOnSave(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "deploy.json")

		// 初始写入包含环境变量占位符与真实敏感密码的配置
		initJSON := `{
			"services": [
				{
					"name": "secret-svc",
					"server": {
						"host": "192.168.1.10",
						"username": "admin",
						"password": "${SERVER_PWD_FROM_ENV}",
						"passphrase": "real-passphrase"
					}
				}
			]
		}`
		if err := os.WriteFile(configPath, []byte(initJSON), 0644); err != nil {
			t.Fatalf("failed to write initial config: %v", err)
		}

		srv := NewServer(":0", configPath)

		// 1. 测试 GET /api/config 是否成功脱敏为 ******，不泄露环境变量占位符或明文
		reqGet := httptest.NewRequest(http.MethodGet, "/api/config", nil)
		wGet := httptest.NewRecorder()
		srv.handleConfig(wGet, reqGet)

		if wGet.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", wGet.Code)
		}

		var getResp config.DeployConfig
		if err := json.Unmarshal(wGet.Body.Bytes(), &getResp); err != nil {
			t.Fatalf("failed to decode GET resp: %v", err)
		}
		if getResp.Services[0].Server.Password != config.MaskSecret {
			t.Errorf("expected password to be masked as %s, got %s", config.MaskSecret, getResp.Services[0].Server.Password)
		}
		if getResp.Services[0].Server.Passphrase != config.MaskSecret {
			t.Errorf("expected passphrase to be masked as %s, got %s", config.MaskSecret, getResp.Services[0].Server.Passphrase)
		}

		// 2. 前端表单在掩码未修改的情况下点击保存 (POST /api/config)，验证原密码/环境变量不被冲掉
		wGetRespBody := wGet.Body.String()
		reqPost := httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(wGetRespBody))
		wPost := httptest.NewRecorder()
		srv.handleConfig(wPost, reqPost)

		if wPost.Code != http.StatusOK {
			t.Fatalf("expected status 200 on post, got %d: %s", wPost.Code, wPost.Body.String())
		}

		// 验证磁盘写入的文件保留了 ${SERVER_PWD_FROM_ENV}
		savedBytes, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("failed to read config file: %v", err)
		}
		savedContent := string(savedBytes)
		if !strings.Contains(savedContent, "${SERVER_PWD_FROM_ENV}") {
			t.Errorf("expected saved file to preserve '${SERVER_PWD_FROM_ENV}', got:\n%s", savedContent)
		}
		if !strings.Contains(savedContent, "real-passphrase") {
			t.Errorf("expected saved file to preserve 'real-passphrase', got:\n%s", savedContent)
		}
	}

	func TestDeployCancelEndpoint(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "deploy.json")
		srv := NewServer(":0", configPath)

		// 1. 当前无任务时请求取消应返回 400 Bad Request
		reqCancel := httptest.NewRequest(http.MethodPost, "/api/deploy/cancel", nil)
		wCancel := httptest.NewRecorder()
		srv.handleDeployCancel(wCancel, reqCancel)

		if wCancel.Code != http.StatusBadRequest {
			t.Errorf("expected HTTP 400 when no task running, got %d", wCancel.Code)
		}

		// 2. 模拟有任务正在运行并测试取消
		canceled := false
		srv.deployMu.Lock()
		srv.deployCancel = func() {
			canceled = true
		}
		srv.deployMu.Unlock()

		wCancel2 := httptest.NewRecorder()
		srv.handleDeployCancel(wCancel2, reqCancel)

		if wCancel2.Code != http.StatusOK {
			t.Errorf("expected HTTP 200 when canceling running task, got %d", wCancel2.Code)
		}
		if !canceled {
			t.Errorf("expected cancelFunc to be invoked")
		}
	}

	func TestServerGracefulShutdown(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "deploy.json")
		srv := NewServer("127.0.0.1:0", configPath)

		ctx, cancel := context.WithCancel(context.Background())
		serverErr := make(chan error, 1)

		go func() {
			serverErr <- srv.StartContext(ctx, false)
		}()

		// 等待服务器启动监听
		time.Sleep(100 * time.Millisecond)

		// 触发上下文取消
		cancel()

		select {
		case err := <-serverErr:
			if err != nil && err != http.ErrServerClosed {
				t.Errorf("unexpected error on graceful shutdown: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Errorf("server graceful shutdown timed out")
		}
	}

	func TestDeployHTTPMethodsAndInvalidConfig(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "deploy.json")
		srv := NewServer(":0", configPath)

		// 1. 测试 GET 请求 /api/deploy 应返回 405 Method Not Allowed
		reqGet := httptest.NewRequest(http.MethodGet, "/api/deploy", nil)
		wGet := httptest.NewRecorder()
		srv.handleDeploy(wGet, reqGet)
		if wGet.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405 for GET /api/deploy, got %d", wGet.Code)
		}

		// 2. 测试 GET 请求 /api/deploy/cancel 应返回 405 Method Not Allowed
		reqCancelGet := httptest.NewRequest(http.MethodGet, "/api/deploy/cancel", nil)
		wCancelGet := httptest.NewRecorder()
		srv.handleDeployCancel(wCancelGet, reqCancelGet)
		if wCancelGet.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405 for GET /api/deploy/cancel, got %d", wCancelGet.Code)
		}

		// 3. 测试配置文件不存在时的 POST /api/deploy 应返回 500
		reqPost := httptest.NewRequest(http.MethodPost, "/api/deploy", strings.NewReader(`{}`))
		wPost := httptest.NewRecorder()
		srv.handleDeploy(wPost, reqPost)
		if wPost.Code != http.StatusInternalServerError {
			t.Errorf("expected 500 when config missing, got %d", wPost.Code)
		}
	}

	func TestSSEBroadcastAndReceive(t *testing.T) {
		msgChan := make(chan string, 10)
		hub.register(msgChan)
		defer hub.unregister(msgChan)

		testMsg := "test broadcast log"
		hub.Broadcast(testMsg)

		select {
		case received := <-msgChan:
			if received != testMsg {
				t.Errorf("expected %q, got %q", testMsg, received)
			}
		case <-time.After(1 * time.Second):
			t.Errorf("timed out waiting for broadcast message")
		}
	}

	func TestDeployWithScenarioAndGroupPayload(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "deploy.json")

		cfgJSON := `{
			"scenarios": [
				{
					"name": "prod",
					"groups": ["backend"]
				}
			],
			"services": [
				{
					"name": "api-svc",
					"group": "backend",
					"type": "standard",
					"server": {
						"host": "127.0.0.1",
						"username": "root",
						"password": "pwd"
					}
				}
			]
		}`
		if err := os.WriteFile(configPath, []byte(cfgJSON), 0644); err != nil {
			t.Fatalf("failed to write config: %v", err)
		}

		srv := NewServer(":0", configPath)

		// 触发带 scenario 和 targetGroups 的部署请求
		reqBody := `{"scenario":"prod","targetGroups":["backend"],"targetTypes":["standard"]}`
		req := httptest.NewRequest(http.MethodPost, "/api/deploy", strings.NewReader(reqBody))
		w := httptest.NewRecorder()
		srv.handleDeploy(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"started"`) {
			t.Errorf("expected response to contain 'started', got %s", w.Body.String())
		}
	}

func TestHandleTestConnect(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "deploy.json")

	cfgJSON := `{
		"services": [
			{
				"name": "mock-svc",
				"server": {
					"host": "127.0.0.1",
					"port": 65431,
					"username": "tester",
					"password": "real_password",
					"connectTimeout": 1
				}
			}
		]
	}`
	if err := os.WriteFile(configPath, []byte(cfgJSON), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	srv := NewServer(":0", configPath)

	// 1. GET 请求拦截
	reqGet := httptest.NewRequest(http.MethodGet, "/api/server/test-connect", nil)
	wGet := httptest.NewRecorder()
	srv.handleTestConnect(wGet, reqGet)
	if wGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", wGet.Code)
	}

	// 2. 格式错误 JSON 请求拦截
	reqBadJSON := httptest.NewRequest(http.MethodPost, "/api/server/test-connect", strings.NewReader("bad-json"))
	wBadJSON := httptest.NewRecorder()
	srv.handleTestConnect(wBadJSON, reqBadJSON)
	if wBadJSON.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", wBadJSON.Code)
	}

	// 3. 提交带掩码密码请求，应从现有配置继承密码并尝试探测（因端口未开启返回 status: error）
	reqBody := `{
		"serviceName": "mock-svc",
		"server": {
			"host": "127.0.0.1",
			"port": 65431,
			"username": "tester",
			"password": "******",
			"connectTimeout": 1
		}
	}`
	reqPost := httptest.NewRequest(http.MethodPost, "/api/server/test-connect", strings.NewReader(reqBody))
	wPost := httptest.NewRecorder()
	srv.handleTestConnect(wPost, reqPost)

	if wPost.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", wPost.Code, wPost.Body.String())
	}
	var res map[string]interface{}
	if err := json.Unmarshal(wPost.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to parse JSON response: %v", err)
	}
	if res["status"] != "error" {
		t.Errorf("expected status 'error' for unreachable port, got %v", res["status"])
	}
	if res["error"] == nil || res["error"] == "" {
		t.Errorf("expected non-empty error message, got nil")
	}
}


