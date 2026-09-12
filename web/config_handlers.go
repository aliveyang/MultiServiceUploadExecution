package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"multi-service-deploy/config"
)

// handleConfig GET 获取配置（脱敏处理），POST 保存更新配置（保留未修改的凭证与环境变量占位符）
// 支持 URL query 参数 ?workspace=xxx 指定目标工作空间，缺省使用当前活动空间
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	cfgPath, wsID := s.resolveWorkspacePath(r)

	switch r.Method {
	case http.MethodGet:
		var cfg *config.DeployConfig
		// 如果是默认空间，检查 s.configPath 与 cfgPath
		if wsID == config.DefaultWorkspaceID && s.configPath != "" {
			statRoot, errRoot := os.Stat(s.configPath)
			statWs, errWs := os.Stat(cfgPath)
			if errRoot == nil && (errWs != nil || statRoot.ModTime().After(statWs.ModTime())) {
				if loaded, err := config.LoadRawConfig(s.configPath); err == nil {
					cfg = loaded
					// 同步写入 cfgPath 保持一致
					if data, err := json.MarshalIndent(cfg, "", "  "); err == nil {
						_ = os.WriteFile(cfgPath, data, 0644)
					}
				}
			}
		}

		if cfg == nil {
			if _, err := os.Stat(cfgPath); err == nil {
				// 加载未展开环境变量的原始配置，防止向网络前端暴露敏感凭证
				loaded, err := config.LoadRawConfig(cfgPath)
				if err == nil {
					cfg = loaded
				}
			} else if _, err := os.Stat(s.configPath); err == nil {
				// 回退尝试加载初始配置文件
				loaded, err := config.LoadRawConfig(s.configPath)
				if err == nil {
					cfg = loaded
				}
			}
		}

		if cfg == nil {
			cfg = config.ExampleConfig()
		}

		// 敏感凭证字段脱敏为 ******
		maskedCfg := config.MaskConfig(cfg)

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Workspace-ID", wsID)
		_ = json.NewEncoder(w).Encode(maskedCfg)

	case http.MethodPost:
		var newCfg config.DeployConfig
		if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
			http.Error(w, fmt.Sprintf("JSON parse error: %v", err), http.StatusBadRequest)
			return
		}

		// 若目标工作空间磁盘存在旧配置，当且仅当提交的值为掩码或空时保留原配置中的密码/环境变量占位符
		var origCfg *config.DeployConfig
		if _, err := os.Stat(cfgPath); err == nil {
			if loaded, err := config.LoadRawConfig(cfgPath); err == nil {
				origCfg = loaded
			}
		}
		if origCfg == nil && s.configPath != "" {
			if _, err := os.Stat(s.configPath); err == nil {
				if loaded, err := config.LoadRawConfig(s.configPath); err == nil {
					origCfg = loaded
				}
			}
		}
		if origCfg != nil {
			config.MergePreservingSecrets(&newCfg, origCfg)
		}

		if err := config.ValidateAndNormalize(&newCfg); err != nil {
			http.Error(w, fmt.Sprintf("Validation error: %v", err), http.StatusBadRequest)
			return
		}

		data, err := json.MarshalIndent(newCfg, "", "  ")
		if err != nil {
			http.Error(w, fmt.Sprintf("Marshal error: %v", err), http.StatusInternalServerError)
			return
		}

		if err := os.WriteFile(cfgPath, data, 0644); err != nil {
			http.Error(w, fmt.Sprintf("Write file error: %v", err), http.StatusInternalServerError)
			return
		}

		// 若操作的是 default 空间，同步更新根目录下的 deploy.json 保证双向一致
		if wsID == config.DefaultWorkspaceID && s.configPath != "" {
			_ = os.WriteFile(s.configPath, data, 0644)
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "ok",
			"workspace": wsID,
		})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
