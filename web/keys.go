package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"multi-service-deploy/config"
	"multi-service-deploy/deployer"
	"multi-service-deploy/logger"
)

// keyNamePattern 密钥文件名合法性：字母数字开头，仅含字母数字点下划线短横线，防路径穿透
var keyNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

// maxKeyBytes 导入私钥内容大小上限（64KB，远超任何合理 SSH 私钥体积）
const maxKeyBytes = 64 * 1024

// keysDir 指定工作空间的私钥存储目录（workspaces/<ws>/keys/）
func keysDir(workspaceDir, ws string) string {
	return filepath.Join(workspaceDir, ws, "keys")
}

// keyInfo 密钥元数据（安全红线：永不包含私钥内容本身）
type keyInfo struct {
	Name        string   `json:"name"`
	Algorithm   string   `json:"algorithm"`
	Fingerprint string   `json:"fingerprint,omitempty"` // SHA256 公钥指纹（加密私钥未提供口令时为空）
	SizeBytes   int64    `json:"sizeBytes"`
	ModifiedAt  int64    `json:"modifiedAt"`
	Encrypted   bool     `json:"encrypted,omitempty"`
	UsedBy      []string `json:"usedBy,omitempty"` // 引用该私钥的服务名列表
}

// handleKeys 工作空间私钥库管理：
// GET    /api/keys?workspace=           列出密钥元数据
// POST   /api/keys?workspace=           导入私钥 {"name","content","passphrase"?}
// DELETE /api/keys?workspace=&id=<name> 删除私钥
func (s *Server) handleKeys(w http.ResponseWriter, r *http.Request) {
	_, wsID := s.resolveWorkspacePath(r)

	switch r.Method {
	case http.MethodGet:
		s.listKeys(w, r, wsID)
	case http.MethodPost:
		s.importKey(w, r, wsID)
	case http.MethodDelete:
		s.deleteKey(w, r, wsID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// listKeys 列出密钥元数据并附被引用服务列表
func (s *Server) listKeys(w http.ResponseWriter, r *http.Request, wsID string) {
	dir := keysDir(s.workspaceDir, wsID)
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		http.Error(w, fmt.Sprintf("Failed to read keys dir: %v", err), http.StatusInternalServerError)
		return
	}

	usedBy := s.keyReferences(wsID)
	list := make([]keyInfo, 0)
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		name := entry.Name()
		if !keyNamePattern.MatchString(name) {
			continue
		}
		info, err := s.buildKeyInfo(dir, name)
		if err != nil {
			// 单个文件损坏不阻塞列表，跳过并记录
			logger.Error("Skip unreadable key %q: %v", name, err)
			continue
		}
		info.UsedBy = usedBy[name]
		list = append(list, info)
	}

	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Workspace-ID", wsID)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"keys": list})
}

// buildKeyInfo 读取私钥文件并解析元数据（内容仅用于本地解析，绝不进入响应）
func (s *Server) buildKeyInfo(dir, name string) (keyInfo, error) {
	path := filepath.Join(dir, name)
	fi, err := os.Stat(path)
	if err != nil {
		return keyInfo{}, fmt.Errorf("stat key file: %w", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return keyInfo{}, fmt.Errorf("read key file: %w", err)
	}

	info := keyInfo{
		Name:       name,
		Algorithm:  "unknown",
		SizeBytes:  fi.Size(),
		ModifiedAt: fi.ModTime().UnixMilli(),
	}

	algo, fp, err := deployer.PrivateKeyMeta(content, "")
	if err != nil {
		// 非法/无法解析的文件仍展示在列表中但标注为未知算法，便于用户清理
		return info, nil
	}
	info.Algorithm = algo
	info.Fingerprint = fp
	info.Encrypted = fp == ""
	return info, nil
}

// keyReferences 扫描指定工作空间配置，返回 私钥文件名 → 引用服务名列表 的映射
func (s *Server) keyReferences(wsID string) map[string][]string {
	refs := make(map[string][]string)
	cfgPath, err := config.GetWorkspacePath(s.workspaceDir, wsID)
	if err != nil {
		return refs
	}
	cfg, err := config.LoadRawConfig(cfgPath)
	if err != nil || cfg == nil {
		return refs
	}
	for _, svc := range cfg.Services {
		keyPath := strings.TrimSpace(svc.Server.PrivateKeyPath)
		if keyPath == "" {
			continue
		}
		base := filepath.Base(filepath.FromSlash(keyPath))
		refs[base] = append(refs[base], svc.Name)
	}
	return refs
}

// importKey 导入私钥：校验名称与内容，解析元数据后以 0600 权限落盘
func (s *Server) importKey(w http.ResponseWriter, r *http.Request, wsID string) {
	var req struct {
		Name       string `json:"name"`
		Content    string `json:"content"`
		Passphrase string `json:"passphrase,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("JSON parse error: %v", err), http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(req.Name)
	if !keyNamePattern.MatchString(name) {
		http.Error(w, "invalid key name (letters, digits, dot, underscore, hyphen only)", http.StatusBadRequest)
		return
	}
	content := []byte(req.Content)
	if len(content) == 0 || len(content) > maxKeyBytes {
		http.Error(w, fmt.Sprintf("key content must be between 1 and %d bytes", maxKeyBytes), http.StatusBadRequest)
		return
	}

	// 导入前解析元数据；内容非法直接拒绝，避免密钥库存入不可用文件
	algo, fp, err := deployer.PrivateKeyMeta(content, req.Passphrase)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid private key: %v", err), http.StatusBadRequest)
		return
	}

	dir := keysDir(s.workspaceDir, wsID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		http.Error(w, fmt.Sprintf("Failed to create keys dir: %v", err), http.StatusInternalServerError)
		return
	}
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err == nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("key %q already exists", name)})
		return
	}
	if err := os.WriteFile(path, content, 0600); err != nil {
		http.Error(w, fmt.Sprintf("Failed to write key file: %v", err), http.StatusInternalServerError)
		return
	}

	logger.System("SSH private key %q imported into workspace [%s]", name, wsID)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(keyInfo{
		Name:        name,
		Algorithm:   algo,
		Fingerprint: fp,
		SizeBytes:   int64(len(content)),
		ModifiedAt:  time.Now().UnixMilli(),
		Encrypted:   fp == "",
	})
}

// deleteKey 删除指定私钥文件
func (s *Server) deleteKey(w http.ResponseWriter, r *http.Request, wsID string) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if !keyNamePattern.MatchString(id) {
		http.Error(w, "invalid key name", http.StatusBadRequest)
		return
	}

	path := filepath.Join(keysDir(s.workspaceDir, wsID), id)
	if _, err := os.Stat(path); err != nil {
		http.Error(w, fmt.Sprintf("key %q not found", id), http.StatusNotFound)
		return
	}
	if err := os.Remove(path); err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete key: %v", err), http.StatusInternalServerError)
		return
	}

	logger.System("SSH private key %q deleted from workspace [%s]", id, wsID)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
