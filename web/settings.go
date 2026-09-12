package web

import (
	"encoding/json"
	"net/http"
	"strings"

	"multi-service-deploy/config"
	"multi-service-deploy/logger"
)

// settingsView GET 返回的运行时设置视图（合并默认值与监听地址的生效状态）
type settingsView struct {
	MaxWorkers        int    `json:"maxWorkers"`
	ConnectTimeout    int    `json:"connectTimeout"`
	ListenAddr        string `json:"listenAddr"`        // 当前生效的监听地址
	ListenAddrSetting string `json:"listenAddrSetting"` // settings.json 中保存的监听地址（为空表示使用默认）
	ListenAddrRestart bool   `json:"listenAddrRestart"` // 已保存的监听地址与当前生效值不一致，需重启生效
	AutoOpenBrowser   bool   `json:"autoOpenBrowser"`
	ListenAddrDefault string `json:"listenAddrDefault"` // 默认监听地址（红线恒为回环）
}

// handleSettings 运行时设置读写：
// GET  /api/settings  读取当前设置（合并默认值）
// POST /api/settings  保存设置（部署进行中拒绝并发相关项变更，返回 409）
func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.writeSettingsView(w)
	case http.MethodPost:
		s.saveSettings(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// currentSettings 线程安全读取运行时设置快照
func (s *Server) currentSettings() config.Settings {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.Settings
}

// persistActiveWorkspace 将活动工作空间写入运行时设置并落盘（失败仅记录日志，不阻断切换）
func (s *Server) persistActiveWorkspace(ws string) {
	s.settingsMu.Lock()
	s.Settings.ActiveWorkspace = ws
	st := s.Settings
	s.settingsMu.Unlock()
	if err := config.SaveSettings(s.workspaceDir, &st); err != nil {
		logger.Error("Failed to persist active workspace: %v", err)
	}
}

// writeSettingsView 输出合并默认值后的设置视图
func (s *Server) writeSettingsView(w http.ResponseWriter) {
	st := s.currentSettings()

	view := settingsView{
		MaxWorkers:        st.MaxWorkers,
		ConnectTimeout:    st.ConnectTimeout,
		ListenAddr:        s.addr,
		ListenAddrSetting: strings.TrimSpace(st.ListenAddr),
		AutoOpenBrowser:   st.AutoOpenBrowser == nil || *st.AutoOpenBrowser,
		ListenAddrDefault: config.DefaultListenAddr,
	}
	if view.MaxWorkers <= 0 {
		view.MaxWorkers = config.DefaultMaxWorkers
	}
	if view.ConnectTimeout <= 0 {
		view.ConnectTimeout = config.DefaultConnectTimeout
	}
	view.ListenAddrRestart = view.ListenAddrSetting != "" && view.ListenAddrSetting != s.addr

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(view)
}

// saveSettings 校验并持久化设置；部署进行中拒绝保存（并发项变更会影响运行中的批次）
func (s *Server) saveSettings(w http.ResponseWriter, r *http.Request) {
	var req config.Settings
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "JSON parse error", http.StatusBadRequest)
		return
	}
	if err := req.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if s.isDeploying.Load() {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "Cannot change settings while deployment is running.",
		})
		return
	}

	// 活动工作空间仅由工作空间端点维护，设置表单保存时保持现状
	req.ActiveWorkspace = s.currentSettings().ActiveWorkspace

	s.settingsMu.Lock()
	s.Settings = req
	s.settingsMu.Unlock()

	if err := config.SaveSettings(s.workspaceDir, &req); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	restartRequired := strings.TrimSpace(req.ListenAddr) != "" && strings.TrimSpace(req.ListenAddr) != s.addr
	if restartRequired {
		logger.System("Listen address setting updated to %q; restart required to take effect.", req.ListenAddr)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":          "ok",
		"restartRequired": restartRequired,
	})
}
