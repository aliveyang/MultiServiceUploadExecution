package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"multi-service-deploy/config"
	"multi-service-deploy/logger"
)

// handleWorkspaces 获取所有工作空间列表 (GET) 或删除工作空间 (DELETE)
func (s *Server) handleWorkspaces(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := config.ListWorkspaces(s.workspaceDir)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to list workspaces: %v", err), http.StatusInternalServerError)
			return
		}
		if len(list) == 0 {
			_ = config.EnsureWorkspaceDir(s.workspaceDir, s.configPath)
			list, _ = config.ListWorkspaces(s.workspaceDir)
		}
		active := s.getCurrentWorkspace()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"workspaces": list,
			"active":     active,
		})

	case http.MethodDelete:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			id = strings.TrimSpace(r.URL.Query().Get("name"))
		}
		if id == "" {
			http.Error(w, "missing workspace id", http.StatusBadRequest)
			return
		}
		if err := config.DeleteWorkspace(s.workspaceDir, id); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if s.getCurrentWorkspace() == id {
			s.setCurrentWorkspace(config.DefaultWorkspaceID)
			s.persistActiveWorkspace(config.DefaultWorkspaceID)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
			"active": s.getCurrentWorkspace(),
		})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleWorkspaceSelect 切换当前活动工作空间
func (s *Server) handleWorkspaceSelect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.isDeploying.Load() {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "Cannot switch workspace while deployment is running.",
		})
		return
	}

	var req struct {
		Workspace string `json:"workspace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "JSON parse error", http.StatusBadRequest)
		return
	}

	id := strings.TrimSpace(req.Workspace)
	if !config.IsValidWorkspaceID(id) {
		http.Error(w, "invalid workspace id", http.StatusBadRequest)
		return
	}

	targetPath, err := config.GetWorkspacePath(s.workspaceDir, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := os.Stat(targetPath); err != nil {
		http.Error(w, fmt.Sprintf("workspace %q does not exist", id), http.StatusNotFound)
		return
	}

	s.setCurrentWorkspace(id)
	s.persistActiveWorkspace(id)
	logger.System("Active workspace switched to %q (%s)", id, targetPath)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"active": id,
	})
}

// handleWorkspaceCreate 新建工作空间
func (s *Server) handleWorkspaceCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		From string `json:"from"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "JSON parse error", http.StatusBadRequest)
		return
	}

	id := strings.TrimSpace(req.ID)
	if !config.IsValidWorkspaceID(id) {
		http.Error(w, "invalid workspace id (only letters, numbers, hyphen, underscore allowed)", http.StatusBadRequest)
		return
	}

	var baseCfg *config.DeployConfig
	if req.From != "empty" {
		fromID := strings.TrimSpace(req.From)
		if fromID == "" {
			fromID = s.getCurrentWorkspace()
		}
		fromPath, err := config.GetWorkspacePath(s.workspaceDir, fromID)
		if err == nil {
			if loaded, err := config.LoadRawConfig(fromPath); err == nil {
				baseCfg = loaded
			}
		}
	}

	info, err := config.CreateWorkspace(s.workspaceDir, id, req.Name, baseCfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.setCurrentWorkspace(id)
	s.persistActiveWorkspace(id)
	logger.Success("Created and switched to new workspace: %s", id)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(info)
}
