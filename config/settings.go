package config

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// SettingsFileName 运行时设置文件名（存放于工作空间根目录，全局生效）
const SettingsFileName = "settings.json"

// 运行时设置默认值（与 CLI 启动参数默认值保持一致）
const (
	DefaultMaxWorkers     = 10
	DefaultConnectTimeout = 15
	DefaultListenAddr     = "127.0.0.1:8080"
)

// Settings 控制台运行时设置（settings.json）。
// 注意红线：监听地址默认值恒为本地回环 127.0.0.1:8080，修改仅落盘、需重启进程生效。
type Settings struct {
	MaxWorkers      int    `json:"maxWorkers,omitempty"`      // 默认最大并发度（Worker Pool 容量，部署请求未指定时生效）
	ConnectTimeout  int    `json:"connectTimeout,omitempty"`  // 默认 SSH 连接超时（秒）
	ListenAddr      string `json:"listenAddr,omitempty"`      // Web 控制台监听地址（需重启生效）
	AutoOpenBrowser *bool  `json:"autoOpen,omitempty"`        // 启动时自动打开浏览器
	ActiveWorkspace string `json:"activeWorkspace,omitempty"` // 活动工作空间 ID（重启后恢复，空表示 default）
}

// SettingsPath 解析 settings.json 的存放路径
func SettingsPath(workspaceDir string) string {
	if strings.TrimSpace(workspaceDir) == "" {
		workspaceDir = DefaultWorkspaceDir
	}
	return filepath.Join(workspaceDir, SettingsFileName)
}

// LoadSettings 读取运行时设置；文件不存在或字段缺省时返回带默认值的配置（err 仅在解析失败时非 nil）
func LoadSettings(workspaceDir string) (*Settings, error) {
	st := &Settings{
		MaxWorkers:      DefaultMaxWorkers,
		ConnectTimeout:  DefaultConnectTimeout,
		ListenAddr:      "",
		AutoOpenBrowser: nil,
	}

	data, err := os.ReadFile(SettingsPath(workspaceDir))
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return st, fmt.Errorf("failed to read settings file: %w", err)
	}

	var loaded Settings
	if err := json.Unmarshal(data, &loaded); err != nil {
		return st, fmt.Errorf("failed to parse settings file: %w", err)
	}

	if loaded.MaxWorkers > 0 {
		st.MaxWorkers = loaded.MaxWorkers
	}
	if loaded.ConnectTimeout > 0 {
		st.ConnectTimeout = loaded.ConnectTimeout
	}
	if strings.TrimSpace(loaded.ListenAddr) != "" {
		st.ListenAddr = strings.TrimSpace(loaded.ListenAddr)
	}
	if loaded.AutoOpenBrowser != nil {
		st.AutoOpenBrowser = loaded.AutoOpenBrowser
	}
	if IsValidWorkspaceID(strings.TrimSpace(loaded.ActiveWorkspace)) {
		st.ActiveWorkspace = strings.TrimSpace(loaded.ActiveWorkspace)
	}
	return st, nil
}

// SaveSettings 校验并保存运行时设置到 settings.json
func SaveSettings(workspaceDir string, st *Settings) error {
	if st == nil {
		return fmt.Errorf("settings must not be nil")
	}
	if err := st.Validate(); err != nil {
		return err
	}

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	path := SettingsPath(workspaceDir)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write settings file: %w", err)
	}
	return nil
}

// Validate 校验设置字段合法性
func (st *Settings) Validate() error {
	if st.MaxWorkers < 0 || st.MaxWorkers > 200 {
		return fmt.Errorf("maxWorkers must be between 1 and 200, got %d", st.MaxWorkers)
	}
	if st.ConnectTimeout < 0 || st.ConnectTimeout > 600 {
		return fmt.Errorf("connectTimeout must be between 1 and 600 seconds, got %d", st.ConnectTimeout)
	}
	if addr := strings.TrimSpace(st.ListenAddr); addr != "" {
		_, portStr, err := net.SplitHostPort(addr)
		if err != nil {
			return fmt.Errorf("listenAddr must be in host:port format, got %q", st.ListenAddr)
		}
		port, err := strconv.Atoi(portStr)
		if err != nil || port <= 0 || port > 65535 {
			return fmt.Errorf("listenAddr port is invalid, got %q", portStr)
		}
	}
	return nil
}
