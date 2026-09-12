package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"multi-service-deploy/config"
	"multi-service-deploy/deployer"
	"multi-service-deploy/logger"
)

// handleDeploy 触发多服务部署流水线，支持 Worker Pool 限流、Context 主动取消与结构化批次事件推送
func (s *Server) handleDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 部署并发防重入锁检查
	if !s.isDeploying.CompareAndSwap(false, true) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "A deployment task is already running. Please wait for it to finish.",
		})
		return
	}

	var req struct {
		Workspace      string   `json:"workspace,omitempty"`
		Scenario       string   `json:"scenario,omitempty"`
		TargetGroups   []string `json:"targetGroups,omitempty"`
		TargetTypes    []string `json:"targetTypes,omitempty"`
		TargetServices []string `json:"targetServices,omitempty"`
		Parallel       *bool    `json:"parallel,omitempty"`
		MaxWorkers     *int     `json:"maxWorkers,omitempty"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	cfgPath, wsID := s.resolveWorkspacePath(r)
	if req.Workspace != "" && config.IsValidWorkspaceID(req.Workspace) {
		if customPath, err := config.GetWorkspacePath(s.workspaceDir, req.Workspace); err == nil {
			if _, err := os.Stat(customPath); err == nil {
				cfgPath = customPath
				wsID = req.Workspace
			}
		}
	}

	var cfg *config.DeployConfig
	var err error
	if _, statErr := os.Stat(cfgPath); statErr == nil {
		cfg, err = config.LoadConfig(cfgPath)
	} else if wsID == config.DefaultWorkspaceID && s.configPath != "" {
		if _, statErr := os.Stat(s.configPath); statErr == nil {
			cfg, err = config.LoadConfig(s.configPath)
		}
	}

	if cfg == nil {
		if err == nil {
			err = fmt.Errorf("configuration file not found for workspace %q", wsID)
		}
		s.isDeploying.Store(false)
		http.Error(w, fmt.Sprintf("Failed to load config: %v", err), http.StatusInternalServerError)
		return
	}

	// 未显式指定并发度时回落到运行时设置中的默认并发（settings.json，缺省 10）
	maxWorkers := s.currentSettings().MaxWorkers
	if maxWorkers <= 0 {
		maxWorkers = 10
	}
	if req.MaxWorkers != nil && *req.MaxWorkers > 0 {
		maxWorkers = *req.MaxWorkers
	}

	opts := deployer.DeployOptions{
		Parallel:       req.Parallel,
		Scenario:       req.Scenario,
		TargetGroups:   req.TargetGroups,
		TargetTypes:    req.TargetTypes,
		TargetServices: req.TargetServices,
		MaxWorkers:     maxWorkers,
		Workspace:      wsID,
		BatchID:        deployer.NewBatchID(),
	}

	// 创建与当前部署绑定的可取消 Context
	deployCtx, cancel := context.WithCancel(context.Background())
	s.deployMu.Lock()
	s.deployCancel = cancel
	s.deployMu.Unlock()

	logger.System("Triggered deployment on workspace [%s] (%s)", wsID, cfgPath)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"started","workspace":"` + wsID + `"}`))

	// 异步启动部署：结构化事件同时广播给 SSE 客户端与历史收集器
	go func() {
		defer func() {
			s.deployMu.Lock()
			s.deployCancel = nil
			s.deployMu.Unlock()
			s.isDeploying.Store(false)
		}()

		collector := s.history.begin(wsID, opts.BatchID)
		opts.OnEvent = func(name string, payload any) {
			hub.BroadcastEvent(name, payload)
			collector.observe(name, payload)
		}

		mgr := deployer.NewDeployManager(cfg, opts)
		allSuccess, _ := mgr.RunWithContext(deployCtx)
		time.Sleep(200 * time.Millisecond)

		if deployCtx.Err() != nil {
			hub.Broadcast("[[DEPLOY_CANCELED]]")
		} else if allSuccess {
			hub.Broadcast("[[DEPLOY_COMPLETED_SUCCESS]]")
		} else {
			hub.Broadcast("[[DEPLOY_COMPLETED_FAILED]]")
		}

		// 兜底落盘批次记录：正常路径 batch_finished 事件已完成归档，此处仅覆盖异常/取消路径
		finalStatus := deployer.BatchStatusFailed
		if deployCtx.Err() != nil {
			finalStatus = deployer.BatchStatusCanceled
		} else if allSuccess {
			finalStatus = deployer.BatchStatusSuccess
		}
		s.history.end(collector, finalStatus)
	}()
}

// handleDeployCancel 处理主动中断/取消当前部署任务的请求
func (s *Server) handleDeployCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.deployMu.Lock()
	cancel := s.deployCancel
	s.deployMu.Unlock()

	if cancel != nil {
		cancel()
		logger.System("Deployment cancellation triggered by user via Web UI.")
		hub.Broadcast("⚠️ [SYSTEM] Deployment cancellation requested by user. Aborting...")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"canceling"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": "No deployment task is currently running.",
	})
}

// handleTestConnect 轻量探测 SSH 服务器连通性，不执行任何远程命令，支持脱敏密码自动继承
func (s *Server) handleTestConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ServiceName string              `json:"serviceName,omitempty"`
		Server      config.ServerConfig `json:"server"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("JSON parse error: %v", err), http.StatusBadRequest)
		return
	}

	targetServer := req.Server
	// 如果密码或 passphrase 带有掩码，尝试从既有配置继承原真实密码
	if targetServer.Password == config.MaskSecret || targetServer.Passphrase == config.MaskSecret || (targetServer.Password == "" && targetServer.PrivateKeyPath == "") {
		cfgPath, _ := s.resolveWorkspacePath(r)
		checkPaths := []string{cfgPath, s.configPath}
		for _, p := range checkPaths {
			if _, err := os.Stat(p); err == nil {
				if origCfg, err := config.LoadConfig(p); err == nil {
					for _, svc := range origCfg.Services {
						if (req.ServiceName != "" && strings.EqualFold(svc.Name, req.ServiceName)) ||
							(strings.EqualFold(svc.Server.Host, targetServer.Host) && svc.Server.Port == targetServer.Port && svc.Server.Username == targetServer.Username) {
							if targetServer.Password == config.MaskSecret || targetServer.Password == "" {
								targetServer.Password = svc.Server.Password
							}
							if targetServer.Passphrase == config.MaskSecret || targetServer.Passphrase == "" {
								targetServer.Passphrase = svc.Server.Passphrase
							}
							if targetServer.PrivateKeyPath == "" && svc.Server.PrivateKeyPath != "" {
								targetServer.PrivateKeyPath = svc.Server.PrivateKeyPath
							}
							break
						}
					}
				}
				if targetServer.Password != config.MaskSecret && targetServer.Password != "" {
					break
				}
			}
		}
	}

	// 环境变量展开
	targetServer.Host = os.ExpandEnv(targetServer.Host)
	targetServer.Username = os.ExpandEnv(targetServer.Username)
	targetServer.Password = os.ExpandEnv(targetServer.Password)
	targetServer.Passphrase = os.ExpandEnv(targetServer.Passphrase)
	targetServer.PrivateKeyPath = os.ExpandEnv(targetServer.PrivateKeyPath)

	if targetServer.Port <= 0 {
		targetServer.Port = 22
	}
	timeoutSec := targetServer.ConnectTimeout
	if timeoutSec <= 0 || timeoutSec > 10 {
		timeoutSec = 5
	}
	targetServer.ConnectTimeout = timeoutSec

	probeCtx, cancel := context.WithTimeout(r.Context(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	start := time.Now()
	err := deployer.TestSSHConnectivity(probeCtx, targetServer, nil)
	latency := time.Since(start).Milliseconds()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ok",
		"latencyMs": latency,
	})
}
