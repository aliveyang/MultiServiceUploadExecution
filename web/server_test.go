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
	if !strings.Contains(wIndex.Body.String(), "Multi-Service Deployer") {
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
	msgChan := make(chan sseMessage, 10)
	hub.register(msgChan)
	defer hub.unregister(msgChan)

	testMsg := "test broadcast log"
	hub.Broadcast(testMsg)

	select {
	case received := <-msgChan:
		if received.event != "" {
			t.Errorf("expected plain broadcast to use default message event, got event %q", received.event)
		}
		if received.data != testMsg {
			t.Errorf("expected %q, got %q", testMsg, received.data)
		}
	case <-time.After(1 * time.Second):
		t.Errorf("timed out waiting for broadcast message")
	}
}

func TestSSEBroadcastEventNamed(t *testing.T) {
	msgChan := make(chan sseMessage, 10)
	hub.register(msgChan)
	defer hub.unregister(msgChan)

	hub.BroadcastEvent("batch_started", map[string]any{"id": "20260912-080000", "total": 3})

	select {
	case received := <-msgChan:
		if received.event != "batch_started" {
			t.Errorf("expected event name batch_started, got %q", received.event)
		}
		if !strings.Contains(received.data, `"id":"20260912-080000"`) || !strings.Contains(received.data, `"total":3`) {
			t.Errorf("expected single-line JSON payload, got %q", received.data)
		}
		if strings.Contains(received.data, "\n") {
			t.Errorf("event payload must be single-line JSON, got multi-line")
		}
	case <-time.After(1 * time.Second):
		t.Errorf("timed out waiting for named event")
	}
}

func TestSSELastEventIDReplay(t *testing.T) {
	// 广播多条消息建立缓冲
	hub.Broadcast("line-1")
	hub.Broadcast("line-2")
	hub.mu.Lock()
	lastSeq := hub.seq
	hub.mu.Unlock()
	hub.BroadcastEvent("service_finished", map[string]any{"name": "svc-1", "status": "ok"})

	// 断点重放：只应拿到 lastSeq 之后的事件
	hub.mu.Lock()
	replayed := hub.replayLocked(lastSeq)
	hub.mu.Unlock()
	if len(replayed) != 1 || replayed[0].event != "service_finished" {
		t.Fatalf("expected exactly 1 replayed named event after breakpoint, got %+v", replayed)
	}
	if replayed[0].seq != lastSeq+1 {
		t.Errorf("expected seq %d, got %d", lastSeq+1, replayed[0].seq)
	}

	// 全量重放：包含文本行
	hub.mu.Lock()
	all := hub.replayLocked(0)
	hub.mu.Unlock()
	if len(all) < 3 {
		t.Errorf("expected full replay to include text lines, got %d", len(all))
	}
}

func TestDeployWithTagsPayload(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "deploy.json")

	cfgJSON := `{
			"services": [
				{
					"name": "api-svc",
					"tags": ["backend"],
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

	// 触发带 tags 标签筛选的部署请求（并集语义）
	reqBody := `{"tags":["backend"],"targetTypes":["standard"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/deploy", strings.NewReader(reqBody))
	w := httptest.NewRecorder()
	srv.handleDeploy(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"started"`) {
		t.Errorf("expected response to contain 'started', got %s", w.Body.String())
	}

	// 等待后台部署 goroutine 与历史收集器完全收尾，避免与 TempDir 清理产生文件竞态
	deadline := time.Now().Add(5 * time.Second)
	for srv.isDeploying.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if srv.isDeploying.Load() {
		t.Errorf("deployment goroutine did not finish in time")
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

func TestHandlePickPathMethodGuard(t *testing.T) {
	srv := NewServer(":0", "deploy.json")

	// 测试非法 HTTP 方法拦截（如 PUT/DELETE）
	reqPut := httptest.NewRequest(http.MethodPut, "/api/system/pick-path", nil)
	wPut := httptest.NewRecorder()
	srv.handlePickPath(wPut, reqPut)
	if wPut.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 MethodNotAllowed, got %d", wPut.Code)
	}
}

func TestWorkspaceEndpointsAndImportToDefault(t *testing.T) {
	tmpDir := t.TempDir()
	wsDir := filepath.Join(tmpDir, "test_workspaces")
	configPath := filepath.Join(tmpDir, "deploy.json")

	// 准备一个旧版配置文件
	legacyCfg := `{
		"parallel": true,
		"services": [
			{
				"name": "legacy-service-node",
				"server": {
					"host": "192.168.1.100",
					"username": "admin",
					"password": "old_secret_pwd"
				}
			}
		]
	}`
	if err := os.WriteFile(configPath, []byte(legacyCfg), 0644); err != nil {
		t.Fatalf("failed to write legacy config: %v", err)
	}

	srv := NewServer(":0", configPath)
	srv.workspaceDir = wsDir
	_ = config.EnsureWorkspaceDir(wsDir, configPath)

	// 1. GET /api/workspaces：应包含自动纳管的 default 工作空间
	reqList := httptest.NewRequest(http.MethodGet, "/api/workspaces", nil)
	wList := httptest.NewRecorder()
	srv.handleWorkspaces(wList, reqList)
	if wList.Code != http.StatusOK {
		t.Fatalf("expected 200 for list workspaces, got %d", wList.Code)
	}
	var listResp struct {
		Workspaces []config.WorkspaceInfo `json:"workspaces"`
		Active     string                 `json:"active"`
	}
	if err := json.Unmarshal(wList.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("failed to parse workspaces json: %v", err)
	}
	if len(listResp.Workspaces) == 0 || listResp.Workspaces[0].ID != config.DefaultWorkspaceID {
		t.Fatalf("expected default workspace in list, got %+v", listResp.Workspaces)
	}
	if listResp.Active != config.DefaultWorkspaceID {
		t.Fatalf("expected active to be default, got %s", listResp.Active)
	}

	// 2. 检查 default 空间是否已包含旧配置的 legacy-service-node
	reqGetDef := httptest.NewRequest(http.MethodGet, "/api/config?workspace=default", nil)
	wGetDef := httptest.NewRecorder()
	srv.handleConfig(wGetDef, reqGetDef)
	if wGetDef.Code != http.StatusOK {
		t.Fatalf("expected 200 for get default config, got %d", wGetDef.Code)
	}
	if !strings.Contains(wGetDef.Body.String(), "legacy-service-node") {
		t.Errorf("expected default workspace to contain legacy-service-node")
	}

	// 3. POST /api/workspaces/create 创建新空间 staging
	createPayload := `{"id":"staging","name":"预发布环境","from":"empty"}`
	reqCreate := httptest.NewRequest(http.MethodPost, "/api/workspaces/create", strings.NewReader(createPayload))
	wCreate := httptest.NewRecorder()
	srv.handleWorkspaceCreate(wCreate, reqCreate)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("expected 200 for workspace create, got %d: %s", wCreate.Code, wCreate.Body.String())
	}
	if srv.getCurrentWorkspace() != "staging" {
		t.Errorf("expected current workspace to switch to staging, got %s", srv.getCurrentWorkspace())
	}

	// 4. 在当前处于 staging 状态下，向 default 空间导入/更新新的老配置
	importedJSON := `{
		"parallel": false,
		"services": [
			{
				"name": "imported-to-default-app",
				"server": {
					"host": "192.168.1.200",
					"username": "root",
					"password": "new_imported_secret"
				}
			}
		]
	}`
	reqImportToDefault := httptest.NewRequest(http.MethodPost, "/api/config?workspace=default", strings.NewReader(importedJSON))
	wImportToDefault := httptest.NewRecorder()
	srv.handleConfig(wImportToDefault, reqImportToDefault)
	if wImportToDefault.Code != http.StatusOK {
		t.Fatalf("expected 200 for import to default workspace, got %d: %s", wImportToDefault.Code, wImportToDefault.Body.String())
	}

	// 校验 default 空间确实保存了导入的配置
	reqCheckDef := httptest.NewRequest(http.MethodGet, "/api/config?workspace=default", nil)
	wCheckDef := httptest.NewRecorder()
	srv.handleConfig(wCheckDef, reqCheckDef)
	if !strings.Contains(wCheckDef.Body.String(), "imported-to-default-app") {
		t.Errorf("expected default workspace to have imported-to-default-app")
	}

	// 5. POST /api/workspaces/select 切换回 default
	selectPayload := `{"workspace":"default"}`
	reqSelect := httptest.NewRequest(http.MethodPost, "/api/workspaces/select", strings.NewReader(selectPayload))
	wSelect := httptest.NewRecorder()
	srv.handleWorkspaceSelect(wSelect, reqSelect)
	if wSelect.Code != http.StatusOK {
		t.Fatalf("expected 200 for select workspace, got %d", wSelect.Code)
	}
	if srv.getCurrentWorkspace() != "default" {
		t.Errorf("expected current workspace to be default, got %s", srv.getCurrentWorkspace())
	}

	// 6. 部署并发中禁止切换工作空间
	srv.isDeploying.Store(true)
	reqSelectConflict := httptest.NewRequest(http.MethodPost, "/api/workspaces/select", strings.NewReader(`{"workspace":"staging"}`))
	wSelectConflict := httptest.NewRecorder()
	srv.handleWorkspaceSelect(wSelectConflict, reqSelectConflict)
	if wSelectConflict.Code != http.StatusConflict {
		t.Errorf("expected 409 conflict when switching during deployment, got %d", wSelectConflict.Code)
	}
	srv.isDeploying.Store(false)

	// 7. DELETE /api/workspaces：禁止删除 default
	reqDelDefault := httptest.NewRequest(http.MethodDelete, "/api/workspaces?id=default", nil)
	wDelDefault := httptest.NewRecorder()
	srv.handleWorkspaces(wDelDefault, reqDelDefault)
	if wDelDefault.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when deleting default workspace, got %d", wDelDefault.Code)
	}

	// 删除 staging 空间成功
	reqDelStaging := httptest.NewRequest(http.MethodDelete, "/api/workspaces?id=staging", nil)
	wDelStaging := httptest.NewRecorder()
	srv.handleWorkspaces(wDelStaging, reqDelStaging)
	if wDelStaging.Code != http.StatusOK {
		t.Errorf("expected 200 when deleting staging workspace, got %d", wDelStaging.Code)
	}
}

// TestStaticHandlerDevMode 验证静态资源处理器的三种形态：
// 1. 默认（非开发模式）：返回编译期内嵌资源；
// 2. 开发模式（DEPLOY_DEV=1）且磁盘目录存在：直读磁盘，改动无需重编译即可生效；
// 3. 开发模式但磁盘目录缺失：自动回退内嵌资源，保证服务可用。
func TestStaticHandlerDevMode(t *testing.T) {
	// 1. 未开启开发模式：内嵌资源
	h, err := newStaticHandler(devStaticDir, false)
	if err != nil {
		t.Fatalf("failed to build embedded handler: %v", err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Multi-Service Deployer") {
		t.Fatalf("expected embedded index to be served, got %d", w.Code)
	}

	// 2. 开发模式且磁盘目录存在：直读磁盘内容（磁盘标记内容与内嵌版本不同）
	devRoot := t.TempDir()
	diskStaticDir := filepath.Join(devRoot, "static")
	if err := os.MkdirAll(diskStaticDir, 0o755); err != nil {
		t.Fatalf("failed to create dev static dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(diskStaticDir, "index.html"), []byte("<html>dev-disk-marker</html>"), 0o644); err != nil {
		t.Fatalf("failed to write dev index: %v", err)
	}
	h, err = newStaticHandler(diskStaticDir, true)
	if err != nil {
		t.Fatalf("failed to build dev handler: %v", err)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "dev-disk-marker") {
		t.Fatalf("expected index served from disk in dev mode, got %d: %s", w.Code, w.Body.String())
	}

	// 3. 开发模式但磁盘目录缺失：回退内嵌资源
	h, err = newStaticHandler(filepath.Join(devRoot, "missing"), true)
	if err != nil {
		t.Fatalf("failed to build fallback handler: %v", err)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Multi-Service Deployer") {
		t.Fatalf("expected fallback to embedded index when dev dir missing, got %d", w.Code)
	}
}

// TestConfigSaveWithTagHooks 验证 Web 配置保存链路对标签钩子的支持：
// POST 携带 tagHooks 与服务 tags 正常落盘，旧 group 字段不再持久化，GET 脱敏视图可见 tagHooks。
func TestConfigSaveWithTagHooks(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "deploy.json")

	initial := `{"services":[{"name":"svc-a","group":"backend","server":{"host":"127.0.0.1","username":"root","password":"pwd"}}]}`
	if err := os.WriteFile(configPath, []byte(initial), 0644); err != nil {
		t.Fatalf("failed to write initial config: %v", err)
	}

	srv := NewServer(":0", configPath)

	newCfg := `{
		"tagHooks":[{"name":"backend","description":"后端集群","hooks":{"preDeploy":["go build"]}}],
		"services":[{"name":"svc-a","tags":["backend","core"],"server":{"host":"127.0.0.1","username":"root","password":"******"}}]
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(newCfg))
	w := httptest.NewRecorder()
	srv.handleConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on config save, got %d: %s", w.Code, w.Body.String())
	}

	// 落盘校验：tagHooks 持久化，旧 group 字段被迁移清除
	found := false
	_ = filepath.WalkDir(tmpDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if strings.Contains(string(data), `"tagHooks"`) && strings.Contains(string(data), "go build") {
			found = true
			if strings.Contains(string(data), `"group"`) {
				t.Errorf("expected legacy group field dropped in %s, got: %s", path, data)
			}
		}
		return nil
	})
	if !found {
		t.Errorf("expected tagHooks persisted under %s", tmpDir)
	}

	// GET 脱敏视图：tagHooks 可见，掩码密码不泄露原值
	reqGet := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	wGet := httptest.NewRecorder()
	srv.handleConfig(wGet, reqGet)
	if wGet.Code != http.StatusOK {
		t.Fatalf("expected 200 on config get, got %d", wGet.Code)
	}
	body := wGet.Body.String()
	if !strings.Contains(body, "tagHooks") || !strings.Contains(body, "backend") {
		t.Errorf("expected GET config to expose tagHooks, got: %s", body)
	}
	if strings.Contains(body, `"pwd"`) {
		t.Errorf("expected masked password in GET config, got: %s", body)
	}
}
