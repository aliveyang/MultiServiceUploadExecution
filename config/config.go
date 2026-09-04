package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// CommandList 灵活支持单个字符串或字符串数组
type CommandList []string

// UnmarshalJSON 兼容单个字符串和字符串数组
func (c *CommandList) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		if strings.TrimSpace(single) != "" {
			*c = []string{single}
		} else {
			*c = []string{}
		}
		return nil
	}

	var slice []string
	if err := json.Unmarshal(data, &slice); err == nil {
		*c = slice
		return nil
	}
	return fmt.Errorf("command must be either a string or an array of strings")
}

// UnmarshalYAML 兼容 YAML 下的单字符串或列表：解码为通用值后复用 JSON 解析
func (c *CommandList) UnmarshalYAML(node *yaml.Node) error {
	var v interface{}
	if err := node.Decode(&v); err != nil {
		return err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.UnmarshalJSON(data)
}

// ServerConfig 服务器连接配置 (服务的ip, 用户名, 密码)
type ServerConfig struct {
	Host           string `json:"host" yaml:"host"`
	Port           int    `json:"port" yaml:"port"`
	Username       string `json:"username" yaml:"username"`
	Password       string `json:"password" yaml:"password"`
	PrivateKeyPath string `json:"privateKeyPath,omitempty" yaml:"privateKeyPath,omitempty"`
	Passphrase         string `json:"passphrase,omitempty" yaml:"passphrase,omitempty"`
	ConnectTimeout     int    `json:"connectTimeout,omitempty" yaml:"connectTimeout,omitempty"` // 秒，默认 15s
	HostKeyFingerprint string `json:"hostKeyFingerprint,omitempty" yaml:"hostKeyFingerprint,omitempty"` // 可选 SHA256 指纹校验，如 "SHA256:..."
}

// UploadConfig 上传路径配置
type UploadConfig struct {
	LocalPath   string   `json:"localPath" yaml:"localPath"`
	RemotePath  string   `json:"remotePath" yaml:"remotePath"`
	Exclude     []string `json:"exclude,omitempty" yaml:"exclude,omitempty"`
	CleanRemote bool     `json:"cleanRemote,omitempty" yaml:"cleanRemote,omitempty"`
}

	// HooksConfig 命令执行钩子
	// 包含：上传前本地/远端执行命令，上传后远端/本地执行命令
	type HooksConfig struct {
		PreUploadLocal   CommandList `json:"preUploadLocal,omitempty" yaml:"preUploadLocal,omitempty"`
		PreUploadRemote  CommandList `json:"preUploadRemote,omitempty" yaml:"preUploadRemote,omitempty"`
		PostUploadRemote CommandList `json:"postUploadRemote,omitempty" yaml:"postUploadRemote,omitempty"`
		PostUploadLocal  CommandList `json:"postUploadLocal,omitempty" yaml:"postUploadLocal,omitempty"`
	}

	// 部署任务类型常量
	const (
		DeployTypeStandard = "standard"  // 经典全流程：构建 -> SSH -> 远端前置 -> SFTP上传 -> 远端后置 -> 本地后置
		DeployTypeExecOnly = "exec_only" // 纯执行型：仅执行命令，跳过 SFTP 文件传输
		DeployTypeSyncOnly = "sync_only" // 纯同步型：仅传输文件，跳过远程重启/执行命令
		DefaultGroup       = "default"   // 默认分组名称
	)

	// ServiceConfig 单个配置单元 (1. 服务ip/用户/密码 + 2. 上传前本地/远端命令 + 3. 上传后远端/本地命令)
	type ServiceConfig struct {
		Name    string       `json:"name" yaml:"name"`
		Group   string       `json:"group,omitempty" yaml:"group,omitempty"`   // 业务分组 (如 frontend, backend, infra)
		Type    string       `json:"type,omitempty" yaml:"type,omitempty"`     // 部署类型: standard, exec_only, sync_only
		Stage   int          `json:"stage,omitempty" yaml:"stage,omitempty"`   // 执行波次/阶段 (默认 1，按升序批次执行)
		Tags    []string     `json:"tags,omitempty" yaml:"tags,omitempty"`     // 标签标识，便于多维度归类
		Server  ServerConfig `json:"server" yaml:"server"`
		Upload  UploadConfig `json:"upload" yaml:"upload"`
		Hooks   HooksConfig  `json:"hooks" yaml:"hooks"`
		Enabled *bool        `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	}

// IsEnabled 检查服务是否启用（默认启用）
func (s *ServiceConfig) IsEnabled() bool {
	if s.Enabled == nil {
		return true
	}
	return *s.Enabled
}

	// BatchHooks 批次生命周期钩子（在组或全局生命周期执行）
	type BatchHooks struct {
		PreDeploy  CommandList `json:"preDeploy,omitempty" yaml:"preDeploy,omitempty"`   // 批次执行前在本地仅执行一次
		PostDeploy CommandList `json:"postDeploy,omitempty" yaml:"postDeploy,omitempty"` // 批次成功后在本地仅执行一次
	}

	// GlobalHooks 全局批次生命周期钩子（在所有节点执行前后各执行一次）
	type GlobalHooks = BatchHooks

	// GroupConfig 业务分组定义（支持该分组独有的批次前后置生命周期钩子）
	type GroupConfig struct {
		Name        string     `json:"name" yaml:"name"`                                 // 分组唯一标识（如 frontend, backend, infra）
		Description string     `json:"description,omitempty" yaml:"description,omitempty"` // 分组描述
		Hooks       BatchHooks `json:"hooks,omitempty" yaml:"hooks,omitempty"`           // 该分组专属的批次前后置生命周期钩子
	}

	// ScenarioConfig 部署场景预设（如 test, prod, quick-restart 等）
	type ScenarioConfig struct {
		Name        string   `json:"name" yaml:"name"`                                 // 场景唯一标识
		Description string   `json:"description,omitempty" yaml:"description,omitempty"` // 场景描述
		Groups      []string `json:"groups,omitempty" yaml:"groups,omitempty"`           // 包含的分组列表
		Services    []string `json:"services,omitempty" yaml:"services,omitempty"`       // 包含的具体服务名称列表
		Types       []string `json:"types,omitempty" yaml:"types,omitempty"`             // 过滤特定部署类型
		Parallel    *bool    `json:"parallel,omitempty" yaml:"parallel,omitempty"`       // 该场景下的并发覆盖
		MaxWorkers  int      `json:"maxWorkers,omitempty" yaml:"maxWorkers,omitempty"`   // 该场景下的最大 Worker 数
	}

	// DeployConfig 全局部署配置，包含多个配置单元与全局钩子
	type DeployConfig struct {
		Parallel  *bool            `json:"parallel,omitempty" yaml:"parallel,omitempty"`   // 默认并发
		Hooks     GlobalHooks      `json:"hooks,omitempty" yaml:"hooks,omitempty"`         // 全局批次钩子
		Groups    []GroupConfig    `json:"groups,omitempty" yaml:"groups,omitempty"`       // 业务分组定义与分组批次钩子
		Scenarios []ScenarioConfig `json:"scenarios,omitempty" yaml:"scenarios,omitempty"` // 预定义场景列表
		Services  []ServiceConfig  `json:"services" yaml:"services"`
	}

	// FindGroup 根据名称查找分组配置（大小写不敏感）
	func (c *DeployConfig) FindGroup(name string) *GroupConfig {
		target := strings.ToLower(strings.TrimSpace(name))
		if target == "" {
			return nil
		}
		for i := range c.Groups {
			if strings.ToLower(c.Groups[i].Name) == target {
				return &c.Groups[i]
			}
		}
		return nil
	}

	// FindScenario 根据名称查找场景预设（大小写不敏感）
	func (c *DeployConfig) FindScenario(name string) *ScenarioConfig {
		target := strings.ToLower(strings.TrimSpace(name))
		if target == "" {
			return nil
		}
		for i := range c.Scenarios {
			if strings.ToLower(c.Scenarios[i].Name) == target {
				return &c.Scenarios[i]
			}
		}
		return nil
	}

	// IsParallel 检查是否并发执行（默认 true）
func (c *DeployConfig) IsParallel() bool {
	if c.Parallel == nil {
		return true
	}
	return *c.Parallel
}

// LoadRawConfig 从指定路径加载并解析配置文件（支持 .json, .yaml, .yml），不展开环境变量，用于 Web 安全编辑与展示
func LoadRawConfig(filePath string) (*DeployConfig, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %q: %w", filePath, err)
	}

	var cfg DeployConfig
	ext := strings.ToLower(filepath.Ext(filePath))

	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse YAML config: %w", err)
		}
	case ".json":
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse JSON config: %w", err)
		}
	default:
		// 先尝试 JSON，若失败再尝试 YAML
		if errJSON := json.Unmarshal(data, &cfg); errJSON != nil {
			if errYAML := yaml.Unmarshal(data, &cfg); errYAML != nil {
				return nil, fmt.Errorf("unknown config format (JSON error: %v; YAML error: %v)", errJSON, errYAML)
			}
		}
	}

	if err := ValidateAndNormalize(&cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

// LoadConfig 从指定路径加载并解析配置文件（支持 .json, .yaml, .yml），并动态展开凭据中的 ${ENV} 环境变量
func LoadConfig(filePath string) (*DeployConfig, error) {
	cfg, err := LoadRawConfig(filePath)
	if err != nil {
		return nil, err
	}

	// 自动扩展敏感凭证中的 ${ENV} 环境变量
	ExpandEnvVariables(cfg)

	return cfg, nil
}

// MaskSecret 敏感凭据脱敏掩码占位符
const MaskSecret = "******"

// MaskConfig 返回脱敏后的配置深拷贝副本，将密码和私钥 passphrase 替换为 MaskSecret
func MaskConfig(cfg *DeployConfig) *DeployConfig {
	if cfg == nil {
		return nil
	}

	// 深度复制结构体
	data, err := json.Marshal(cfg)
	if err != nil {
		return cfg
	}
	var masked DeployConfig
	if err := json.Unmarshal(data, &masked); err != nil {
		return cfg
	}

	for i := range masked.Services {
		s := &masked.Services[i].Server
		if strings.TrimSpace(s.Password) != "" {
			s.Password = MaskSecret
		}
		if strings.TrimSpace(s.Passphrase) != "" {
			s.Passphrase = MaskSecret
		}
	}

	return &masked
}

// MergePreservingSecrets 合并前端提交的新配置与磁盘原有配置：
// 当 newCfg 某服务的密码或 passphrase 仍为 MaskSecret 时，自动恢复为原配置中对应的真实值（包含 ${ENV} 占位符或原始密码）
func MergePreservingSecrets(newCfg *DeployConfig, originalCfg *DeployConfig) {
	if newCfg == nil || originalCfg == nil {
		return
	}

	origMap := make(map[string]*ServerConfig)
	for i := range originalCfg.Services {
		origMap[originalCfg.Services[i].Name] = &originalCfg.Services[i].Server
	}

	for i := range newCfg.Services {
		svc := &newCfg.Services[i]
		if origServer, exists := origMap[svc.Name]; exists {
			if svc.Server.Password == MaskSecret {
				svc.Server.Password = origServer.Password
			}
			if svc.Server.Passphrase == MaskSecret {
				svc.Server.Passphrase = origServer.Passphrase
			}
		}
	}
}

// DangerousRemotePaths 远端禁止执行 cleanRemote 递归清空的高危系统根路径
var DangerousRemotePaths = map[string]bool{
	"/":      true,
	"/*":     true,
	"/bin":   true,
	"/boot":  true,
	"/dev":   true,
	"/etc":   true,
	"/home":  true,
	"/lib":   true,
	"/lib64": true,
	"/opt":   true,
	"/proc":  true,
	"/root":  true,
	"/sbin":  true,
	"/sys":   true,
	"/usr":   true,
	"/var":   true,
}

// IsDangerousRemotePath 检查远端清理路径是否属于高危系统目录（使用标准 POSIX 规范路径计算）
func IsDangerousRemotePath(remotePath string) bool {
	p := strings.TrimSpace(remotePath)
	if p == "" {
		return true
	}
	p = strings.ReplaceAll(p, "\\", "/")
	clean := path.Clean(p)
	if clean == "." || clean == "/" || clean == "/*" || DangerousRemotePaths[clean] {
		return true
	}
	return false
}

	// ExpandEnvVariables 对密码与私钥等凭据字段支持 ${ENV_VAR} 格式的环境变量解析
	func ExpandEnvVariables(cfg *DeployConfig) {
		for i := range cfg.Services {
			svc := &cfg.Services[i]
			svc.Server.Password = expandEnv(svc.Server.Password)
			svc.Server.Passphrase = expandEnv(svc.Server.Passphrase)
			svc.Server.PrivateKeyPath = expandEnv(svc.Server.PrivateKeyPath)
		}
	}

	func expandEnv(val string) string {
		if !strings.Contains(val, "${") {
			return val
		}
		return os.ExpandEnv(val)
	}

	// ValidateAndNormalize 校验配置并设置默认值
	func ValidateAndNormalize(cfg *DeployConfig) error {
		if len(cfg.Services) == 0 {
			return fmt.Errorf("no services defined in configuration")
		}

		names := make(map[string]bool)
		for i := range cfg.Services {
			svc := &cfg.Services[i]

			// 默认服务名
			if strings.TrimSpace(svc.Name) == "" {
				svc.Name = fmt.Sprintf("service-%d", i+1)
			}
			if names[svc.Name] {
				return fmt.Errorf("duplicate service name %q at index %d", svc.Name, i)
			}
			names[svc.Name] = true

			// 分组与任务类型默认值
			if strings.TrimSpace(svc.Group) == "" {
				svc.Group = DefaultGroup
			}
			if strings.TrimSpace(svc.Type) == "" {
				svc.Type = DeployTypeStandard
			} else {
				svc.Type = strings.ToLower(strings.TrimSpace(svc.Type))
				switch svc.Type {
				case DeployTypeStandard, DeployTypeExecOnly, DeployTypeSyncOnly:
					// 合法类型
				default:
					return fmt.Errorf("service %q: invalid deploy type %q (allowed: standard, exec_only, sync_only)", svc.Name, svc.Type)
				}
			}

			// 波次/阶段默认值 (>=1)
			if svc.Stage <= 0 {
				svc.Stage = 1
			}

			// 服务器连接校验
			if strings.TrimSpace(svc.Server.Host) == "" {
				return fmt.Errorf("service %q: server.host is required", svc.Name)
			}
			if svc.Server.Port <= 0 {
				svc.Server.Port = 22
			}
			if strings.TrimSpace(svc.Server.Username) == "" {
				return fmt.Errorf("service %q: server.username is required", svc.Name)
			}
			if strings.TrimSpace(svc.Server.Password) == "" && strings.TrimSpace(svc.Server.PrivateKeyPath) == "" {
				return fmt.Errorf("service %q: either server.password or server.privateKeyPath must be provided", svc.Name)
			}
			if svc.Server.ConnectTimeout <= 0 {
				svc.Server.ConnectTimeout = 15
			}

			// 上传配置校验（如果有设置本地路径或远端路径，必须两项都提供）
			hasLocal := strings.TrimSpace(svc.Upload.LocalPath) != ""
			hasRemote := strings.TrimSpace(svc.Upload.RemotePath) != ""
			if hasLocal && !hasRemote {
				return fmt.Errorf("service %q: upload.remotePath is required when upload.localPath is specified", svc.Name)
			}
			if !hasLocal && hasRemote {
				return fmt.Errorf("service %q: upload.localPath is required when upload.remotePath is specified", svc.Name)
			}
			if svc.Upload.CleanRemote && IsDangerousRemotePath(svc.Upload.RemotePath) {
				return fmt.Errorf("service %q: cleanRemote is prohibited for high-risk system path %q", svc.Name, svc.Upload.RemotePath)
			}
		}

		// 分组校验
		groupNames := make(map[string]bool)
		for i := range cfg.Groups {
			grp := &cfg.Groups[i]
			grpName := strings.TrimSpace(grp.Name)
			if grpName == "" {
				return fmt.Errorf("group at index %d: name is required", i)
			}
			lowerGrp := strings.ToLower(grpName)
			if groupNames[lowerGrp] {
				return fmt.Errorf("duplicate group name %q", grp.Name)
			}
			groupNames[lowerGrp] = true
		}

		// 场景校验
		scenarioNames := make(map[string]bool)
		for i := range cfg.Scenarios {
			sc := &cfg.Scenarios[i]
			scName := strings.TrimSpace(sc.Name)
			if scName == "" {
				return fmt.Errorf("scenario at index %d: name is required", i)
			}
			lowerName := strings.ToLower(scName)
			if scenarioNames[lowerName] {
				return fmt.Errorf("duplicate scenario name %q", sc.Name)
			}
			scenarioNames[lowerName] = true

			// 检查 types 过滤是否合法
			for _, t := range sc.Types {
				switch strings.ToLower(strings.TrimSpace(t)) {
				case DeployTypeStandard, DeployTypeExecOnly, DeployTypeSyncOnly:
				default:
					return fmt.Errorf("scenario %q: invalid deploy type filter %q", sc.Name, t)
				}
			}
		}

		return nil
	}

	// ExampleConfig 返回示例配置结构体
	func ExampleConfig() *DeployConfig {
		example := DeployConfig{
			Parallel: boolPtr(true),
			Hooks: GlobalHooks{
				PreDeploy: []string{
					"echo \"[Global Pre-Hook] 全局顶层前置检查（仅执行一次）...\"",
				},
				PostDeploy: []string{
					"echo \"[Global Post-Hook] 全局顶层后置通知（全部成功后仅执行一次）...\"",
				},
			},
			Groups: []GroupConfig{
				{
					Name:        "infra",
					Description: "基础设施与数据库维护组",
					Hooks: BatchHooks{
						PreDeploy: []string{
							"echo \"[Infra Group Pre-Hook] 基础设施组前置预检...\"",
						},
						PostDeploy: []string{
							"echo \"[Infra Group Post-Hook] 基础设施就绪！\"",
						},
					},
				},
				{
					Name:        "backend",
					Description: "后端核心微服务集群",
					Hooks: BatchHooks{
						PreDeploy: []string{
							"echo \"[Backend Group Pre-Hook] 后端组本地编译构建打包...\"",
						},
						PostDeploy: []string{
							"echo \"[Backend Group Post-Hook] 后端集群全部节点上线成功！\"",
						},
					},
				},
				{
					Name:        "frontend",
					Description: "前端静态与 CDN 资源组",
					Hooks: BatchHooks{
						PreDeploy: []string{
							"echo \"[Frontend Group Pre-Hook] 前端本地 npm run build 统一打包...\"",
						},
						PostDeploy: []string{
							"echo \"[Frontend Group Post-Hook] 前端全部节点发布成功！\"",
						},
					},
				},
			},
			Scenarios: []ScenarioConfig{
				{
					Name:        "prod",
					Description: "生产环境全量发布 (先基础设施与后端，最后前端)",
					Groups:      []string{"infra", "backend", "frontend"},
				},
				{
					Name:        "backend-only",
					Description: "仅重启后端与执行迁移",
					Groups:      []string{"infra", "backend"},
				},
				{
					Name:        "quick-restart",
					Description: "快速维护：仅执行纯命令任务",
					Types:       []string{"exec_only"},
				},
			},
			Services: []ServiceConfig{
				{
					Name:  "db-migrate-01",
					Group: "infra",
					Type:  DeployTypeExecOnly,
					Stage: 1, // 先执行数据库迁移与基础配置
					Server: ServerConfig{
						Host:           "192.168.1.100",
						Port:           22,
						Username:       "root",
						Password:       "your_password_here",
						PrivateKeyPath: "",
						ConnectTimeout: 15,
					},
					Hooks: HooksConfig{
						PreUploadRemote: []string{
							"echo '==> [Remote] Running database migration...'",
							"cd /opt/app && ./migrate -env=prod || true",
						},
					},
				},
				{
					Name:  "api-server-01",
					Group: "backend",
					Type:  DeployTypeStandard,
					Stage: 2, // 第二波次：部署核心 API 服务
					Server: ServerConfig{
						Host:           "192.168.1.101",
						Port:           22,
						Username:       "root",
						Password:       "your_password_here",
						PrivateKeyPath: "",
						ConnectTimeout: 15,
					},
					Upload: UploadConfig{
						LocalPath:   "./dist",
						RemotePath:  "/opt/app/api",
						Exclude:     []string{".git", "*.log", "node_modules", ".DS_Store"},
						CleanRemote: false,
					},
					Hooks: HooksConfig{
						PreUploadLocal: []string{
							"echo [Local] Building api project...",
						},
						PreUploadRemote: []string{
							"echo '==> [Remote] Ensuring target dir exists...'",
							"mkdir -p /opt/app/api",
						},
						PostUploadRemote: []string{
							"echo '==> [Remote] Restarting service...'",
							"chmod +x /opt/app/api/run.sh 2>/dev/null || true",
							"systemctl restart my-api 2>/dev/null || echo 'service restart skipped'",
						},
						PostUploadLocal: []string{
							"echo [Local] api-server-01 deployment finished successfully.",
						},
					},
				},
				{
					Name:  "web-server-02",
					Group: "frontend",
					Type:  DeployTypeStandard,
					Stage: 3, // 第三波次：更新前端静态资源
					Server: ServerConfig{
						Host:           "192.168.1.102",
						Port:           22,
						Username:       "root",
						Password:       "your_password_here",
						PrivateKeyPath: "",
						ConnectTimeout: 15,
					},
					Upload: UploadConfig{
						LocalPath:   "./web-dist",
						RemotePath:  "/var/www/html",
						Exclude:     []string{".git", "*.map"},
						CleanRemote: false,
					},
					Hooks: HooksConfig{
						PreUploadLocal: []string{
							"echo [Local] Packing web assets...",
						},
						PreUploadRemote: []string{
							"mkdir -p /var/www/html",
						},
						PostUploadRemote: []string{
							"nginx -s reload 2>/dev/null || echo 'nginx reload skipped'",
						},
						PostUploadLocal: []string{
							"echo [Local] web-server-02 deployment finished successfully.",
						},
					},
				},
			},
		}
		return &example
	}

// GenerateExampleJSON 生成完整的示例配置 JSON 文本
func GenerateExampleJSON() string {
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(ExampleConfig())
	return strings.TrimSpace(buf.String())
}

func boolPtr(b bool) *bool {
	return &b
}
