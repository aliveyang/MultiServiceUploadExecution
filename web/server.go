package web

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"multi-service-deploy/config"
	"multi-service-deploy/logger"
)

//go:embed static/*
var staticFiles embed.FS

// devStaticDir 开发模式（DEPLOY_DEV=1）下前端静态资源的磁盘目录（相对启动时工作目录）
const devStaticDir = "web/static"

// newStaticHandler 构建前端静态资源处理器：
// 开发模式（DEPLOY_DEV=1）且磁盘目录存在时直读磁盘，前端改动刷新浏览器即生效、无需重新编译；
// 目录缺失或未开启开发模式时回退到编译期内嵌资源，生产交付行为保持不变。
func newStaticHandler(devDir string, devEnabled bool) (http.Handler, error) {
	if devEnabled {
		if st, err := os.Stat(devDir); err == nil && st.IsDir() {
			logger.System("Dev mode enabled: serving static assets from disk (%s), page refresh picks up edits without rebuild.", devDir)
			return http.FileServer(http.Dir(devDir)), nil
		}
		logger.System("Dev mode requested but %s not found, falling back to embedded assets.", devDir)
	}
	subFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return nil, fmt.Errorf("failed to load static files: %w", err)
	}
	return http.FileServer(http.FS(subFS)), nil
}

// Server Web 管理服务（仅负责装配：路由注册、生命周期与共享状态；
// 具体业务 handler 按资源域拆分在各自文件中）
type Server struct {
	configPath       string
	workspaceDir     string
	currentWorkspace string
	wsMu             sync.RWMutex
	addr             string
	isDeploying      atomic.Bool
	deployMu         sync.Mutex
	deployCancel     context.CancelFunc

	// history 批次历史收集与落盘（见 history.go）
	history *historyStore

	// settings 运行时设置缓存（见 settings.go）
	settingsMu sync.RWMutex
	config.Settings
}

// NewServer 创建 Web 服务器
func NewServer(addr, configPath string) *Server {
	if configPath == "" {
		configPath = "deploy.json"
	}
	wsDir := config.ResolveWorkspaceDir(configPath)
	// 确保工作空间目录存在并自动平滑迁移/纳管已有配置到默认工作空间
	_ = config.EnsureWorkspaceDir(wsDir, configPath)

	s := &Server{
		addr:             addr,
		configPath:       configPath,
		workspaceDir:     wsDir,
		currentWorkspace: config.DefaultWorkspaceID,
	}
	s.history = newHistoryStore(func() string { return s.workspaceDir })

	// 加载运行时设置（读失败时使用零值+默认值兜底，不影响启动），
	// 并恢复上次的活动工作空间（活跃空间持久化）
	if st, err := config.LoadSettings(wsDir); err == nil && st != nil {
		s.Settings = *st
		if config.IsValidWorkspaceID(st.ActiveWorkspace) {
			if _, statErr := os.Stat(filepath.Join(wsDir, st.ActiveWorkspace+".json")); statErr == nil {
				s.currentWorkspace = st.ActiveWorkspace
			}
		}
	}
	return s
}

// getCurrentWorkspace 安全获取当前活动工作空间ID
func (s *Server) getCurrentWorkspace() string {
	s.wsMu.RLock()
	defer s.wsMu.RUnlock()
	if s.currentWorkspace == "" {
		return config.DefaultWorkspaceID
	}
	return s.currentWorkspace
}

// SetCurrentWorkspace 安全切换当前活动工作空间ID
func (s *Server) SetCurrentWorkspace(ws string) {
	s.setCurrentWorkspace(ws)
}

// setCurrentWorkspace 安全切换当前活动工作空间ID
func (s *Server) setCurrentWorkspace(ws string) {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	s.currentWorkspace = ws
}

// resolveWorkspacePath 根据 HTTP 请求中的 Query/Header 或当前活动上下文解析目标工作空间路径与ID
func (s *Server) resolveWorkspacePath(r *http.Request) (string, string) {
	ws := ""
	if r != nil {
		ws = strings.TrimSpace(r.URL.Query().Get("workspace"))
		if ws == "" {
			ws = strings.TrimSpace(r.Header.Get("X-Workspace-ID"))
		}
	}
	if ws == "" {
		ws = s.getCurrentWorkspace()
	}
	if !config.IsValidWorkspaceID(ws) {
		ws = config.DefaultWorkspaceID
	}

	wsPath, err := config.GetWorkspacePath(s.workspaceDir, ws)
	if err != nil {
		return s.configPath, ws
	}
	return wsPath, ws
}

// StartContext 启动 HTTP 服务器并注册路由，支持外部 Context 优雅关闭与部署任务联动中断
func (s *Server) StartContext(ctx context.Context, autoOpen bool) error {
	// 将全局日志事件连通到 SSE 广播与当前批次的日志归档
	logger.OnLog = func(line string) {
		hub.Broadcast(line)
		s.history.teeLog(line)
	}

	mux := http.NewServeMux()

	// 静态前端资源（DEPLOY_DEV=1 开发模式下直读磁盘 web/static，改完刷新即生效）
	fileServer, err := newStaticHandler(devStaticDir, os.Getenv("DEPLOY_DEV") == "1")
	if err != nil {
		return err
	}
	mux.Handle("/", fileServer)

	// API 路由（按资源域拆分在各 handler 文件中）
	mux.HandleFunc("/api/workspaces", s.handleWorkspaces)
	mux.HandleFunc("/api/workspaces/select", s.handleWorkspaceSelect)
	mux.HandleFunc("/api/workspaces/create", s.handleWorkspaceCreate)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/deploy", s.handleDeploy)
	mux.HandleFunc("/api/deploy/cancel", s.handleDeployCancel)
	mux.HandleFunc("GET /api/deploy/history", s.handleDeployHistory)
	mux.HandleFunc("GET /api/deploy/history/{id}", s.handleDeployHistoryDetail)
	mux.HandleFunc("/api/server/test-connect", s.handleTestConnect)
	mux.HandleFunc("/api/system/pick-path", s.handlePickPath)
	mux.HandleFunc("/api/keys", s.handleKeys)
	mux.HandleFunc("/api/settings", s.handleSettings)
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
