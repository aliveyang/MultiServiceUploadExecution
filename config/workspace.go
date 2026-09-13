package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	// DefaultWorkspaceID 默认工作空间唯一标识符
	DefaultWorkspaceID = "default"
	// DefaultWorkspaceDir 默认工作空间存放目录
	DefaultWorkspaceDir = "workspaces"
)

var (
	// workspaceIDPattern 校验工作空间ID合法性：只允许字母、数字、下划线、短横线及中文，1-64位，防御路径穿透
	workspaceIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_\-\x{4e00}-\x{9fa5}]{1,64}$`)

	// reservedWorkspaceIDs 运行时保留的工作空间ID（与工作空间根目录下的运行时文件同名，禁止占用）
	reservedWorkspaceIDs = map[string]bool{
		"settings": true, // settings.json 运行时设置
	}
)

// WorkspaceInfo 工作空间元数据
type WorkspaceInfo struct {
	ID        string `json:"id"`        // 工作空间ID（唯一标识）
	Name      string `json:"name"`      // 显示名称
	Path      string `json:"path"`      // 物理配置文件路径
	IsDefault bool   `json:"isDefault"` // 是否为默认空间
	UpdatedAt int64  `json:"updatedAt"` // 最近更新时间戳（毫秒）
}

// IsValidWorkspaceID 校验工作空间标识是否合法安全。
// workspaceIDPattern 仅允许字母/数字/下划线/短横线/中文（1-64 位），
// 已覆盖路径穿透与特殊字符防御，无需额外黑名单。
func IsValidWorkspaceID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	if reservedWorkspaceIDs[strings.ToLower(id)] {
		return false
	}
	return workspaceIDPattern.MatchString(id)
}

// EnsureWorkspaceDir 确保工作空间目录存在，若无 default 空间则自动检测旧配置文件进行平滑迁移
func EnsureWorkspaceDir(workspaceDir, fallbackConfigPath string) error {
	if strings.TrimSpace(workspaceDir) == "" {
		workspaceDir = DefaultWorkspaceDir
	}

	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		return fmt.Errorf("failed to create workspace dir %q: %w", workspaceDir, err)
	}

	defaultPath := filepath.Join(workspaceDir, DefaultWorkspaceID+".json")
	if _, err := os.Stat(defaultPath); err == nil {
		// 已存在 default 工作空间
		return nil
	}

	// 检查是否存在可平滑纳管的旧版配置
	var initConfig *DeployConfig
	candidates := []string{}
	if fallbackConfigPath != "" {
		candidates = append(candidates, fallbackConfigPath)
	}
	candidates = append(candidates, "deploy.json", "deploy.yaml", "deploy.yml")

	for _, cand := range candidates {
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			if cfg, err := LoadRawConfig(cand); err == nil && cfg != nil && len(cfg.Services) > 0 {
				initConfig = cfg
				break
			}
		}
	}

	// 若未检测到任何旧有配置文件，暂不预写 default.json，待首次保存或通过 Web UI 配置时写入
	if initConfig == nil {
		return nil
	}

	data, err := json.MarshalIndent(initConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal default workspace config: %w", err)
	}

	if err := os.WriteFile(defaultPath, data, 0644); err != nil {
		return fmt.Errorf("failed to initialize default workspace config: %w", err)
	}

	return nil
}

// ResolveWorkspaceDir 根据配置文件路径解析工作空间根目录（配置文件所在目录下的 workspaces/）。
// 若配置文件本就位于工作空间根目录（如自动探测到的 workspaces/<空间>.json），直接返回该目录，避免嵌套出 workspaces/workspaces。
func ResolveWorkspaceDir(configPath string) string {
	baseDir := filepath.Dir(configPath)
	if baseDir == "." || baseDir == "" {
		return DefaultWorkspaceDir
	}
	if strings.EqualFold(filepath.Base(baseDir), DefaultWorkspaceDir) {
		return baseDir
	}
	return filepath.Join(baseDir, DefaultWorkspaceDir)
}

// GetWorkspacePath 获取指定工作空间配置文件的绝对/相对安全路径
func GetWorkspacePath(workspaceDir, id string) (string, error) {
	if strings.TrimSpace(workspaceDir) == "" {
		workspaceDir = DefaultWorkspaceDir
	}
	id = strings.TrimSpace(id)
	if id == "" {
		id = DefaultWorkspaceID
	}
	if !IsValidWorkspaceID(id) {
		return "", fmt.Errorf("invalid workspace id %q (only letters, numbers, hyphen, underscore allowed)", id)
	}

	// 优先匹配已有的扩展名
	for _, ext := range []string{".json", ".yaml", ".yml"} {
		p := filepath.Join(workspaceDir, id+ext)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}

	// 默认返回 .json
	return filepath.Join(workspaceDir, id+".json"), nil
}

// ListWorkspaces 列出指定目录下所有工作空间信息，保证 default 永远位于首位
func ListWorkspaces(workspaceDir string) ([]WorkspaceInfo, error) {
	if strings.TrimSpace(workspaceDir) == "" {
		workspaceDir = DefaultWorkspaceDir
	}

	entries, err := os.ReadDir(workspaceDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []WorkspaceInfo{}, nil
		}
		return nil, fmt.Errorf("failed to read workspace dir: %w", err)
	}

	var list []WorkspaceInfo
	seen := make(map[string]bool)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// 运行时设置文件不是工作空间
		if strings.EqualFold(name, SettingsFileName) {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".json" && ext != ".yaml" && ext != ".yml" {
			continue
		}

		id := strings.TrimSuffix(name, filepath.Ext(name))
		if seen[id] || !IsValidWorkspaceID(id) {
			continue
		}
		seen[id] = true

		filePath := filepath.Join(workspaceDir, name)
		var updatedAt int64
		if fi, err := entry.Info(); err == nil {
			updatedAt = fi.ModTime().UnixMilli()
		} else {
			updatedAt = time.Now().UnixMilli()
		}

		dispName := id
		if id == DefaultWorkspaceID {
			dispName = "默认工作区 (Default)"
		}

		list = append(list, WorkspaceInfo{
			ID:        id,
			Name:      dispName,
			Path:      filePath,
			IsDefault: id == DefaultWorkspaceID,
			UpdatedAt: updatedAt,
		})
	}

	// 排序：默认空间在最前，其余按 ID 字母序
	sort.Slice(list, func(i, j int) bool {
		if list[i].IsDefault {
			return true
		}
		if list[j].IsDefault {
			return false
		}
		return strings.ToLower(list[i].ID) < strings.ToLower(list[j].ID)
	})

	return list, nil
}

// CreateWorkspace 创建新的工作空间
func CreateWorkspace(workspaceDir, id, name string, baseConfig *DeployConfig) (WorkspaceInfo, error) {
	if strings.TrimSpace(workspaceDir) == "" {
		workspaceDir = DefaultWorkspaceDir
	}
	id = strings.TrimSpace(id)
	if !IsValidWorkspaceID(id) {
		return WorkspaceInfo{}, fmt.Errorf("invalid workspace id %q", id)
	}

	targetPath := filepath.Join(workspaceDir, id+".json")
	for _, ext := range []string{".json", ".yaml", ".yml"} {
		p := filepath.Join(workspaceDir, id+ext)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return WorkspaceInfo{}, fmt.Errorf("workspace %q already exists at %s", id, p)
		}
	}

	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		return WorkspaceInfo{}, fmt.Errorf("failed to create workspace dir: %w", err)
	}

	cfg := baseConfig
	if cfg == nil {
		cfg = ExampleConfig()
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return WorkspaceInfo{}, fmt.Errorf("failed to serialize workspace config: %w", err)
	}

	if err := os.WriteFile(targetPath, data, 0644); err != nil {
		return WorkspaceInfo{}, fmt.Errorf("failed to write workspace file: %w", err)
	}

	dispName := strings.TrimSpace(name)
	if dispName == "" {
		dispName = id
	}

	return WorkspaceInfo{
		ID:        id,
		Name:      dispName,
		Path:      targetPath,
		IsDefault: id == DefaultWorkspaceID,
		UpdatedAt: time.Now().UnixMilli(),
	}, nil
}

// DeleteWorkspace 删除指定工作空间（默认工作空间禁止删除）
func DeleteWorkspace(workspaceDir, id string) error {
	if strings.TrimSpace(workspaceDir) == "" {
		workspaceDir = DefaultWorkspaceDir
	}
	id = strings.TrimSpace(id)
	if id == DefaultWorkspaceID {
		return fmt.Errorf("default workspace %q cannot be deleted", DefaultWorkspaceID)
	}
	if !IsValidWorkspaceID(id) {
		return fmt.Errorf("invalid workspace id %q", id)
	}

	deleted := false
	for _, ext := range []string{".json", ".yaml", ".yml"} {
		p := filepath.Join(workspaceDir, id+ext)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			if err := os.Remove(p); err != nil {
				return fmt.Errorf("failed to remove workspace file %s: %w", p, err)
			}
			deleted = true
		}
	}

	if !deleted {
		return fmt.Errorf("workspace %q not found", id)
	}
	return nil
}

// SaveWorkspaceConfig 安全保存指定工作空间配置
func SaveWorkspaceConfig(workspaceDir, id string, cfg *DeployConfig) error {
	path, err := GetWorkspacePath(workspaceDir, id)
	if err != nil {
		return err
	}

	if err := ValidateAndNormalize(cfg); err != nil {
		return fmt.Errorf("validation error: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write workspace file: %w", err)
	}

	// 如果保存的是 default 工作空间，且根目录存在旧版 deploy.json，同步更新根目录 deploy.json 以保障双向兼容
	if id == DefaultWorkspaceID {
		if fi, err := os.Stat("deploy.json"); err == nil && !fi.IsDir() {
			_ = os.WriteFile("deploy.json", data, 0644)
		}
	}

	return nil
}
