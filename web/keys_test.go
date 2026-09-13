package web

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// genTestPrivateKey 在内存中生成测试用 RSA 私钥 PEM
func genTestPrivateKey(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate rsa key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

func TestKeysImportListDelete(t *testing.T) {
	srv, _ := newTestServer(t)
	keyPEM := genTestPrivateKey(t)

	// 1. 导入私钥
	importBody := `{"name":"id_ed25519_test","content":"` + pemToJSONString(keyPEM) + `"}`
	reqImport := httptest.NewRequest(http.MethodPost, "/api/keys", strings.NewReader(importBody))
	wImport := httptest.NewRecorder()
	srv.handleKeys(wImport, reqImport)
	if wImport.Code != http.StatusOK {
		t.Fatalf("expected 200 for key import, got %d: %s", wImport.Code, wImport.Body.String())
	}
	var imported keyInfo
	if err := json.Unmarshal(wImport.Body.Bytes(), &imported); err != nil {
		t.Fatalf("failed to parse import response: %v", err)
	}
	if imported.Algorithm != "ssh-rsa" {
		t.Errorf("expected algorithm ssh-rsa, got %s", imported.Algorithm)
	}
	if !strings.HasPrefix(imported.Fingerprint, "SHA256:") {
		t.Errorf("expected SHA256 fingerprint prefix, got %q", imported.Fingerprint)
	}

	// 2. 列表响应绝不包含私钥内容（安全红线）
	reqList := httptest.NewRequest(http.MethodGet, "/api/keys", nil)
	wList := httptest.NewRecorder()
	srv.handleKeys(wList, reqList)
	if wList.Code != http.StatusOK {
		t.Fatalf("expected 200 for key list, got %d", wList.Code)
	}
	if strings.Contains(wList.Body.String(), "PRIVATE KEY") {
		t.Errorf("security violation: key list leaked private key material:\n%s", wList.Body.String())
	}
	if !strings.Contains(wList.Body.String(), `"name":"id_ed25519_test"`) {
		t.Errorf("expected key list to contain imported key, got %s", wList.Body.String())
	}

	// 3. 重复导入同名密钥返回 409（必须使用全新的请求体：上一个请求体已被消费）
	wDup := httptest.NewRecorder()
	srv.handleKeys(wDup, httptest.NewRequest(http.MethodPost, "/api/keys", strings.NewReader(importBody)))
	if wDup.Code != http.StatusConflict {
		t.Errorf("expected 409 for duplicate key import, got %d", wDup.Code)
	}

	// 4. 删除私钥
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/keys?id=id_ed25519_test", nil)
	wDel := httptest.NewRecorder()
	srv.handleKeys(wDel, reqDel)
	if wDel.Code != http.StatusOK {
		t.Fatalf("expected 200 for key delete, got %d: %s", wDel.Code, wDel.Body.String())
	}

	// 5. 非法文件名（路径穿透）拦截
	reqBad := httptest.NewRequest(http.MethodDelete, "/api/keys?id=..%2Fevil", nil)
	wBad := httptest.NewRecorder()
	srv.handleKeys(wBad, reqBad)
	if wBad.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for path traversal key name, got %d", wBad.Code)
	}
}

func TestKeysImportInvalidContent(t *testing.T) {
	srv, wsDir := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/keys", strings.NewReader(`{"name":"bad-key","content":"not-a-pem"}`))
	w := httptest.NewRecorder()
	srv.handleKeys(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid key content, got %d: %s", w.Code, w.Body.String())
	}
	// 拒绝后不得留下半成品文件
	if _, err := os.Stat(filepath.Join(wsDir, "default", "keys", "bad-key")); !os.IsNotExist(err) {
		t.Errorf("expected no key file persisted for invalid content")
	}
}

func TestKeysReferencedByServices(t *testing.T) {
	srv, _ := newTestServer(t)
	keyPEM := genTestPrivateKey(t)

	// 先保存一份引用该私钥的服务配置
	cfg := `{"services":[{"name":"api-svc","server":{"host":"10.0.0.1","username":"root","privateKeyPath":"workspaces/default/keys/deploy_key"},"upload":{}}]}`
	cfgPath := filepath.Join(srv.workspaceDir, "default.json")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	importBody := `{"name":"deploy_key","content":"` + pemToJSONString(keyPEM) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/keys", strings.NewReader(importBody))
	w := httptest.NewRecorder()
	srv.handleKeys(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	wList := httptest.NewRecorder()
	srv.handleKeys(wList, httptest.NewRequest(http.MethodGet, "/api/keys", nil))
	if !strings.Contains(wList.Body.String(), `"usedBy":["api-svc"]`) {
		t.Errorf("expected key list to include referenced service api-svc, got %s", wList.Body.String())
	}
}

// pemToJSONString 将 PEM 内容转义为可直接嵌入 JSON 字符串字面量的形式
func pemToJSONString(pemBytes []byte) string {
	data, _ := json.Marshal(string(pemBytes))
	return strings.Trim(string(data), `"`)
}

// TestKeysImportFromPath 验证"填写本地路径后入库"：服务端代读控制机本地私钥文件，
// 未显式提供 name 时默认取文件名；列表响应携带存储目录 dir 供前端拼装引用路径。
func TestKeysImportFromPath(t *testing.T) {
	srv, wsDir := newTestServer(t)
	keyPEM := genTestPrivateKey(t)

	// 先把私钥写到控制机本地任意路径（模拟用户手填的源文件）
	srcPath := filepath.Join(t.TempDir(), "my-source-key.pem")
	if err := os.WriteFile(srcPath, keyPEM, 0600); err != nil {
		t.Fatalf("failed to write source key: %v", err)
	}

	// json.Marshal 负责正确转义 Windows 路径中的反斜杠
	pathJSON, err := json.Marshal(srcPath)
	if err != nil {
		t.Fatalf("failed to marshal path: %v", err)
	}
	importBody := `{"path":` + string(pathJSON) + `}`
	reqImport := httptest.NewRequest(http.MethodPost, "/api/keys", strings.NewReader(importBody))
	wImport := httptest.NewRecorder()
	srv.handleKeys(wImport, reqImport)
	if wImport.Code != http.StatusOK {
		t.Fatalf("expected 200 for path import, got %d: %s", wImport.Code, wImport.Body.String())
	}
	var imported keyInfo
	if err := json.Unmarshal(wImport.Body.Bytes(), &imported); err != nil {
		t.Fatalf("failed to parse import response: %v", err)
	}
	if imported.Name != "my-source-key.pem" {
		t.Errorf("expected default name from file base, got %q", imported.Name)
	}

	// 列表响应应包含存储目录 dir，且不泄露私钥内容
	reqList := httptest.NewRequest(http.MethodGet, "/api/keys", nil)
	wList := httptest.NewRecorder()
	srv.handleKeys(wList, reqList)
	if wList.Code != http.StatusOK {
		t.Fatalf("expected 200 for key list, got %d", wList.Code)
	}
	if !strings.Contains(wList.Body.String(), `"dir"`) || !strings.Contains(wList.Body.String(), "keys") {
		t.Errorf("expected keys dir in list response, got: %s", wList.Body.String())
	}
	if strings.Contains(wList.Body.String(), "PRIVATE KEY") {
		t.Errorf("security violation: key list leaked private key material")
	}

	// 库文件真实落盘（默认空间）
	stored := filepath.Join(wsDir, "default", "keys", "my-source-key.pem")
	if _, err := os.Stat(stored); err != nil {
		t.Errorf("expected key stored at %s: %v", stored, err)
	}

	// content 与 path 互斥
	dup := `{"path":` + string(pathJSON) + `,"content":"x"}`
	reqDup := httptest.NewRequest(http.MethodPost, "/api/keys", strings.NewReader(dup))
	wDup := httptest.NewRecorder()
	srv.handleKeys(wDup, reqDup)
	if wDup.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when both path and content provided, got %d", wDup.Code)
	}

	// 路径不存在返回 400
	missing := `{"path":"Z:/definitely/not/exist.pem"}`
	reqMissing := httptest.NewRequest(http.MethodPost, "/api/keys", strings.NewReader(missing))
	wMissing := httptest.NewRecorder()
	srv.handleKeys(wMissing, reqMissing)
	if wMissing.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing path, got %d", wMissing.Code)
	}
}
