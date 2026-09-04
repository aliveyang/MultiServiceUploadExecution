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

// ServiceConfig 单个配置单元 (1. 服务ip/用户/密码 + 2. 上传前本地/远端命令 + 3. 上传后远端/本地命令)
type ServiceConfig struct {
	Name    string       `json:"name" yaml:"name"`
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

// GlobalHooks 全局批次生命周期钩子（在所有节点执行前后各执行一次）
type GlobalHooks struct {
	PreDeploy  CommandList `json:"preDeploy,omitempty" yaml:"preDeploy,omitempty"`   // 批次并发执行前在本地仅执行一次
	PostDeploy CommandList `json:"postDeploy,omitempty" yaml:"postDeploy,omitempty"` // 批次全部成功后在本地仅执行一次
}

// DeployConfig 全局部署配置，包含多个配置单元与全局钩子
type DeployConfig struct {
	Parallel *bool           `json:"parallel,omitempty" yaml:"parallel,omitempty"` // 默认并发
	Hooks    GlobalHooks     `json:"hooks,omitempty" yaml:"hooks,omitempty"`       // 全局批次钩子
	Services []ServiceConfig `json:"services" yaml:"services"`
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

		return nil
	}

// ExampleConfig 返回示例配置结构体
func ExampleConfig() *DeployConfig {
	example := DeployConfig{
		Parallel: boolPtr(true),
		Hooks: GlobalHooks{
			PreDeploy: []string{
				"echo '==> [Batch Pre-Hook] 批次前置：全局统一执行构建（仅执行一次）...'",
			},
			PostDeploy: []string{
				"echo '==> [Batch Post-Hook] 批次后置：所有节点部署完成（仅执行一次）...'",
			},
		},
		Services: []ServiceConfig{
			{
				Name: "api-server-01",
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
				Name: "web-server-02",
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
