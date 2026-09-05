package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"multi-service-deploy/config"
	"multi-service-deploy/deployer"
	"multi-service-deploy/logger"
)

//go:embed static/*
var staticFiles embed.FS

// SSEHub 管理 SSE 客户端连接与事件广播
type SSEHub struct {
	mu      sync.Mutex
	clients map[chan string]bool
}

var hub = &SSEHub{
	clients: make(map[chan string]bool),
}

func (h *SSEHub) register(ch chan string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[ch] = true
}

func (h *SSEHub) unregister(ch chan string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, ch)
	close(ch)
}

func (h *SSEHub) Broadcast(msg string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- msg:
		default:
		}
	}
}

// Server Web 管理服务
type Server struct {
	configPath   string
	addr         string
	isDeploying  atomic.Bool
	deployMu     sync.Mutex
	deployCancel context.CancelFunc
}

// NewServer 创建 Web 服务器
func NewServer(addr, configPath string) *Server {
	if configPath == "" {
		configPath = "deploy.json"
	}
	return &Server{
		addr:       addr,
		configPath: configPath,
	}
}

// Start 启动 HTTP 服务器并注册路由（向后兼容）
func (s *Server) Start(autoOpen bool) error {
	return s.StartContext(context.Background(), autoOpen)
}

// StartContext 启动 HTTP 服务器并注册路由，支持外部 Context 优雅关闭与部署任务联动中断
func (s *Server) StartContext(ctx context.Context, autoOpen bool) error {
	// 将全局日志事件连通到 SSE 广播
	logger.OnLog = func(line string) {
		hub.Broadcast(line)
	}

	mux := http.NewServeMux()

	// 静态前端资源
	subFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return fmt.Errorf("failed to load static files: %w", err)
	}
	fileServer := http.FileServer(http.FS(subFS))
	mux.Handle("/", fileServer)

		// API 路由
		mux.HandleFunc("/api/config", s.handleConfig)
		mux.HandleFunc("/api/deploy", s.handleDeploy)
		mux.HandleFunc("/api/deploy/cancel", s.handleDeployCancel)
		mux.HandleFunc("/api/server/test-connect", s.handleTestConnect)
		mux.HandleFunc("/api/system/pick-path", s.handlePickPath)
		mux.HandleFunc("/api/deploy/events", s.handleSSE)

	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.addr, err)
	}

	actualAddr := listener.Addr().String()
	url := fmt.Sprintf("http://localhost:%d", listener.Addr().(*net.TCPAddr).Port)
	logger.Success("Web UI is running at %s (bound to %s)", url, actualAddr)
	logger.System("Open your browser to configure services and deploy visually.")

	if autoOpen {
		go openBrowser(url)
	}

	httpSrv := &http.Server{
		Handler: mux,
	}

	// 监听 Context 取消实现优雅停机
	go func() {
		<-ctx.Done()
		// 中止可能正在进行的后台部署任务
		s.deployMu.Lock()
		if s.deployCancel != nil {
			s.deployCancel()
		}
		s.deployMu.Unlock()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()

	err = httpSrv.Serve(listener)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// handleConfig GET 获取当前配置（脱敏处理），POST 保存更新配置（保留未修改的凭证与环境变量占位符）
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		var cfg *config.DeployConfig
		if _, err := os.Stat(s.configPath); err == nil {
			// 加载未展开环境变量的原始配置，防止向网络前端暴露敏感凭证
			loaded, err := config.LoadRawConfig(s.configPath)
			if err == nil {
				cfg = loaded
			}
		}

		if cfg == nil {
			cfg = config.ExampleConfig()
		}

		// 敏感凭证字段脱敏为 ******
		maskedCfg := config.MaskConfig(cfg)

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(maskedCfg)

	case http.MethodPost:
		var newCfg config.DeployConfig
		if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
			http.Error(w, fmt.Sprintf("JSON parse error: %v", err), http.StatusBadRequest)
			return
		}

		// 若磁盘存在旧配置，当且仅当提交的值为掩码或空时保留原配置中的密码/环境变量占位符
		if _, err := os.Stat(s.configPath); err == nil {
			if origCfg, err := config.LoadRawConfig(s.configPath); err == nil {
				config.MergePreservingSecrets(&newCfg, origCfg)
			}
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

		if err := os.WriteFile(s.configPath, data, 0644); err != nil {
			http.Error(w, fmt.Sprintf("Write file error: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleDeploy 触发多服务部署流水线，支持 Worker Pool 限流与 Context 主动取消
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

	cfg, err := config.LoadConfig(s.configPath)
	if err != nil {
		s.isDeploying.Store(false)
		http.Error(w, fmt.Sprintf("Failed to load config: %v", err), http.StatusInternalServerError)
		return
	}

	var req struct {
		Scenario       string   `json:"scenario,omitempty"`
		TargetGroups   []string `json:"targetGroups,omitempty"`
		TargetTypes    []string `json:"targetTypes,omitempty"`
		TargetServices []string `json:"targetServices,omitempty"`
		Parallel       *bool    `json:"parallel,omitempty"`
		MaxWorkers     *int     `json:"maxWorkers,omitempty"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	maxWorkers := 10
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
	}

	// 创建与当前部署绑定的可取消 Context
	deployCtx, cancel := context.WithCancel(context.Background())
	s.deployMu.Lock()
	s.deployCancel = cancel
	s.deployMu.Unlock()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"started"}`))

	// 异步启动部署
	go func() {
		defer func() {
			s.deployMu.Lock()
			s.deployCancel = nil
			s.deployMu.Unlock()
			s.isDeploying.Store(false)
		}()

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
			if _, err := os.Stat(s.configPath); err == nil {
				if origCfg, err := config.LoadConfig(s.configPath); err == nil {
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

// handleSSE 处理实时日志推送事件流
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	msgChan := make(chan string, 1024)
	hub.register(msgChan)
	defer hub.unregister(msgChan)

	// 发送初始连接通知
	fmt.Fprintf(w, "data: [CONNECTED] Real-time log stream ready\n\n")
	flusher.Flush()

	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			return
		case msg, ok := <-msgChan:
			if !ok {
				return
			}
			// 多行日志需兼容 SSE 格式
			lines := strings.Split(msg, "\n")
			for _, line := range lines {
				if line != "" {
					fmt.Fprintf(w, "data: %s\n", line)
				}
			}
			fmt.Fprintf(w, "\n")
			flusher.Flush()
		}
	}
}

// openBrowser 跨平台自动打开浏览器
func openBrowser(url string) {
	time.Sleep(300 * time.Millisecond)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

// handlePickPath 唤起操作系统原生的文件/文件夹选择框，获取宿主机真实绝对路径
func (s *Server) handlePickPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "folder" // 默认选择文件夹
	}

	selectedPath, err := pickNativeSystemPath(mode)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	if selectedPath == "" {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "canceled",
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"path":   selectedPath,
	})
}

// pickNativeSystemPath 跨平台调用系统原生对话框，Windows 下调用 System.Windows.Forms
func pickNativeSystemPath(mode string) (string, error) {
	switch runtime.GOOS {
	case "windows":
		var psScript string
		if mode == "file" {
			psScript = `Add-Type -AssemblyName System.Windows.Forms; $f = New-Object System.Windows.Forms.OpenFileDialog; $f.Title = '请选择要部署上传的本地文件'; $f.Filter = '所有文件 (*.*)|*.*'; if ($f.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8; [Console]::Write($f.FileName) }`
		} else {
			psScript = `Add-Type -AssemblyName System.Windows.Forms; $d = New-Object System.Windows.Forms.FolderBrowserDialog; $d.Description = '请选择要部署上传的本地目录 (文件夹)'; $d.ShowNewFolderButton = $true; if ($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8; [Console]::Write($d.SelectedPath) }`
		}
		cmd := exec.Command("powershell", "-NoProfile", "-STA", "-Command", psScript)
		out, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("failed to open Windows file dialog: %w", err)
		}
		return strings.TrimSpace(string(out)), nil

	case "darwin":
		var appleScript string
		if mode == "file" {
			appleScript = `POSIX path of (choose file with prompt "请选择要上传的本地文件")`
		} else {
			appleScript = `POSIX path of (choose folder with prompt "请选择要上传的本地目录")`
		}
		cmd := exec.Command("osascript", "-e", appleScript)
		out, err := cmd.Output()
		if err != nil {
			// 用户点击取消时 osascript 会返回非零退出码
			return "", nil
		}
		return strings.TrimSpace(string(out)), nil

	default:
		// Linux: 优先尝试 zenity，若无则尝试 kdialog
		var cmd *exec.Cmd
		if mode == "file" {
			cmd = exec.Command("zenity", "--file-selection", "--title=请选择要上传的本地文件")
		} else {
			cmd = exec.Command("zenity", "--file-selection", "--directory", "--title=请选择要上传的本地目录")
		}
		out, err := cmd.Output()
		if err == nil {
			return strings.TrimSpace(string(out)), nil
		}
		return "", fmt.Errorf("native file dialog requires 'zenity' or desktop environment on Linux: %w", err)
	}
}
