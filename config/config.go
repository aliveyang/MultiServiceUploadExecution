package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"multi-service-deploy/logger"

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
	Host               string `json:"host" yaml:"host"`
	Port               int    `json:"port" yaml:"port"`
	Username           string `json:"username" yaml:"username"`
	Password           string `json:"password" yaml:"password"`
	PrivateKeyPath     string `json:"privateKeyPath,omitempty" yaml:"privateKeyPath,omitempty"`
	Passphrase         string `json:"passphrase,omitempty" yaml:"passphrase,omitempty"`
	ConnectTimeout     int    `json:"connectTimeout,omitempty" yaml:"connectTimeout,omitempty"`         // 秒，默认 15s
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
	DefaultTag         = "default"   // 默认标签名称（未打标服务的兜底归类）
)

// ServiceConfig 单个配置单元 (1. 服务ip/用户/密码 + 2. 上传前本地/远端命令 + 3. 上传后远端/本地命令)
type ServiceConfig struct {
	Name    string       `json:"name" yaml:"name"`
	Group   string       `json:"group,omitempty" yaml:"group,omitempty"`     // [已废弃] 旧版单分组字段，仅用于解析旧配置；加载时自动迁移至 tags
	Type    string       `json:"type,omitempty" yaml:"type,omitempty"`       // 部署类型: standard, exec_only, sync_only
	Stage   int          `json:"stage,omitempty" yaml:"stage,omitempty"`     // 执行波次/阶段 (默认 1，按升序批次执行)
	Tags    []string     `json:"tags,omitempty" yaml:"tags,omitempty"`       // 标签列表：多维度归类/筛选，多选部署时按并集匹配
	HostRef string       `json:"hostRef,omitempty" yaml:"hostRef,omitempty"` // 引用主机库条目名称；内联 server 字段优先级高于库值
	Server  ServerConfig `json:"server" yaml:"server"`
	Upload  UploadConfig `json:"upload" yaml:"upload"`
	Hooks   HooksConfig  `json:"hooks" yaml:"hooks"`
	Enabled *bool        `json:"enabled,omitempty" yaml:"enabled,omitempty"`
}

// HostConfig 主机库条目：可被多个服务通过 HostRef 复用的连接定义
type HostConfig struct {
	Name   string       `json:"name" yaml:"name"`     // 主机库唯一名称（服务 hostRef 引用该名称）
	Server ServerConfig `json:"server" yaml:"server"` // 连接与认证定义
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

// GroupConfig [已废弃] 旧版业务分组定义，仅用于解析旧配置；
// 加载时自动迁移为 TagHookConfig（分组名 → 标签名，批次钩子原样保留）
type GroupConfig struct {
	Name        string     `json:"name" yaml:"name"`                                   // 分组唯一标识（如 frontend, backend, infra）
	Description string     `json:"description,omitempty" yaml:"description,omitempty"` // 分组描述
	Hooks       BatchHooks `json:"hooks,omitempty" yaml:"hooks,omitempty"`             // 该分组专属的批次前后置生命周期钩子
}

// TagHookConfig 标签批次钩子：为指定标签定义批次前后置生命周期钩子（本地各执行一次）。
// 标签本身无需预先声明（服务打标即生效）；仅当需要为标签挂批次钩子时才在此声明。
type TagHookConfig struct {
	Name        string     `json:"name" yaml:"name"`                                   // 标签唯一标识（与服务 tags 中的标签名匹配，大小写不敏感）
	Description string     `json:"description,omitempty" yaml:"description,omitempty"` // 标签描述
	Hooks       BatchHooks `json:"hooks,omitempty" yaml:"hooks,omitempty"`             // 该标签专属的批次前后置生命周期钩子
}

// DeployConfig 全局部署配置，包含多个配置单元与全局钩子
type DeployConfig struct {
	Parallel *bool           `json:"parallel,omitempty" yaml:"parallel,omitempty"` // 默认并发
	Hooks    BatchHooks      `json:"hooks,omitempty" yaml:"hooks,omitempty"`       // 全局批次钩子
	Groups   []GroupConfig   `json:"groups,omitempty" yaml:"groups,omitempty"`     // [已废弃] 旧版分组定义，仅用于解析旧配置；加载时迁移至 TagHooks 后置空
	TagHooks []TagHookConfig `json:"tagHooks,omitempty" yaml:"tagHooks,omitempty"` // 标签批次钩子定义（可选，按标签挂载批次前后置钩子）
	Hosts    []HostConfig    `json:"hosts,omitempty" yaml:"hosts,omitempty"`       // 主机库：可复用的连接定义，服务经 hostRef 引用
	Services []ServiceConfig `json:"services" yaml:"services"`
}

// FindHost 根据名称查找主机库条目（大小写不敏感）
func (c *DeployConfig) FindHost(name string) *HostConfig {
	target := strings.ToLower(strings.TrimSpace(name))
	if target == "" {
		return nil
	}
	for i := range c.Hosts {
		if strings.ToLower(c.Hosts[i].Name) == target {
			return &c.Hosts[i]
		}
	}
	return nil
}

// ResolveHostReferences 将服务声明的 hostRef 展开为实际连接配置：
// 以主机库条目为基底，服务内联 server 中的非零字段优先覆盖（保持内联覆盖语义）。
// 未找到引用时返回错误，避免部署期才暴露配置错误。
func (c *DeployConfig) ResolveHostReferences() error {
	for i := range c.Services {
		svc := &c.Services[i]
		ref := strings.TrimSpace(svc.HostRef)
		if ref == "" {
			continue
		}
		host := c.FindHost(ref)
		if host == nil {
			return fmt.Errorf("service %q: hostRef %q not found in hosts[]", svc.Name, ref)
		}
		merged := host.Server
		if svc.Server.Host != "" {
			merged.Host = svc.Server.Host
		}
		if svc.Server.Port != 0 {
			merged.Port = svc.Server.Port
		}
		if svc.Server.Username != "" {
			merged.Username = svc.Server.Username
		}
		if svc.Server.Password != "" {
			merged.Password = svc.Server.Password
		}
		if svc.Server.PrivateKeyPath != "" {
			merged.PrivateKeyPath = svc.Server.PrivateKeyPath
		}
		if svc.Server.Passphrase != "" {
			merged.Passphrase = svc.Server.Passphrase
		}
		if svc.Server.ConnectTimeout != 0 {
			merged.ConnectTimeout = svc.Server.ConnectTimeout
		}
		if svc.Server.HostKeyFingerprint != "" {
			merged.HostKeyFingerprint = svc.Server.HostKeyFingerprint
		}
		svc.Server = merged
	}
	return nil
}

// FindTagHook 根据标签名查找标签批次钩子配置（大小写不敏感）
func (c *DeployConfig) FindTagHook(name string) *TagHookConfig {
	target := strings.ToLower(strings.TrimSpace(name))
	if target == "" {
		return nil
	}
	for i := range c.TagHooks {
		if strings.ToLower(c.TagHooks[i].Name) == target {
			return &c.TagHooks[i]
		}
	}
	return nil
}

// normalizeTags 规范化标签列表：trim、小写、去空、去重（保持首次出现顺序）
func normalizeTags(tags []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// HasTag 检查服务是否携带指定标签（大小写不敏感）
func (s *ServiceConfig) HasTag(name string) bool {
	target := strings.ToLower(strings.TrimSpace(name))
	if target == "" {
		return false
	}
	for _, t := range s.Tags {
		if strings.ToLower(strings.TrimSpace(t)) == target {
			return true
		}
	}
	return false
}

// migrateLegacyGroups 将旧版分组体系平滑迁移至标签体系（幂等，可重复调用）：
// 1) groups[].hooks → tagHooks（同名标签钩子已显式声明时以新配置为准，跳过迁移）；
// 2) 服务遗留 group 字段的迁移在服务归一化阶段处理（group → tags）。
// 迁移完成后清空 Groups，避免旧字段再次序列化落盘。
func migrateLegacyGroups(cfg *DeployConfig) {
	for i := range cfg.Groups {
		g := &cfg.Groups[i]
		name := strings.ToLower(strings.TrimSpace(g.Name))
		if name == "" || cfg.FindTagHook(name) != nil {
			continue
		}
		cfg.TagHooks = append(cfg.TagHooks, TagHookConfig{
			Name:        name,
			Description: g.Description,
			Hooks:       g.Hooks,
		})
	}
	cfg.Groups = nil
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
	format := ""

	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse YAML config: %w", err)
		}
		format = "yaml"
	case ".json":
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse JSON config: %w", err)
		}
		format = "json"
	default:
		// 先尝试 JSON，若失败再尝试 YAML
		if errJSON := json.Unmarshal(data, &cfg); errJSON != nil {
			if errYAML := yaml.Unmarshal(data, &cfg); errYAML != nil {
				return nil, fmt.Errorf("unknown config format (JSON error: %v; YAML error: %v)", errJSON, errYAML)
			}
			format = "yaml"
		} else {
			format = "json"
		}
	}

	warnDeprecatedSections(data, format)

	if err := ValidateAndNormalize(&cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

// warnDeprecatedSections 加载期探测旧版 groups / scenarios 字段并输出迁移提示（仅提示，不阻断）
func warnDeprecatedSections(data []byte, format string) {
	var raw map[string]any
	var err error
	if format == "yaml" {
		err = yaml.Unmarshal(data, &raw)
	} else {
		err = json.Unmarshal(data, &raw)
	}
	if err != nil {
		return
	}
	if _, ok := raw["groups"]; ok {
		logger.System("[config] 提示：检测到旧版 \"groups\" 字段，已自动迁移为 \"tagHooks\"（按标签挂载批次钩子），下次保存配置后即为新格式。")
	}
	if _, ok := raw["scenarios"]; ok {
		logger.System("[config] 提示：检测到旧版 \"scenarios\" 字段，场景预设已移除，请改用服务标签(tags)进行筛选部署。")
	}
}

// LoadConfig 从指定路径加载并解析配置文件（支持 .json, .yaml, .yml），并动态展开凭据中的 ${ENV} 环境变量
func LoadConfig(filePath string) (*DeployConfig, error) {
	cfg, err := LoadRawConfig(filePath)
	if err != nil {
		return nil, err
	}

	// 自动扩展敏感凭证中的 ${ENV} 环境变量，并展开主机库 hostRef 引用
	ExpandEnvVariables(cfg)
	if err := cfg.ResolveHostReferences(); err != nil {
		return nil, err
	}

	warnUnsafeHookQuoting(cfg)
	return cfg, nil
}

// warnUnsafeHookQuoting 部署加载期告警：cmd.exe 不把单引号视为引用符，
// 单引号文本中的裸 ">" 会被解析为输出重定向，在控制机工作目录产生游离文件（如 "[Backend"）。
// 该模式在 POSIX shell 下合法，因此仅告警不阻断，建议统一改用双引号。
func warnUnsafeHookQuoting(cfg *DeployConfig) {
	check := func(owner string, cmds []string) {
		for _, cmd := range cmds {
			if strings.Contains(cmd, "'") && strings.Contains(cmd, ">") {
				logger.System("[config] 警告：%s 的钩子命令在单引号内包含 \">\"，Windows cmd.exe 会将其解析为输出重定向，建议改用双引号：%s", owner, cmd)
			}
		}
	}
	check("全局批次钩子", append(append([]string{}, cfg.Hooks.PreDeploy...), cfg.Hooks.PostDeploy...))
	for i := range cfg.TagHooks {
		t := &cfg.TagHooks[i]
		check(fmt.Sprintf("标签 %q 钩子", t.Name), append(append([]string{}, t.Hooks.PreDeploy...), t.Hooks.PostDeploy...))
	}
	for i := range cfg.Services {
		svc := &cfg.Services[i]
		h := &svc.Hooks
		check(fmt.Sprintf("服务 %q 钩子", svc.Name), append(append(append(append([]string{}, h.PreUploadLocal...), h.PreUploadRemote...), h.PostUploadRemote...), h.PostUploadLocal...))
	}
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

	// 主机库条目中的凭证同步脱敏
	for i := range masked.Hosts {
		s := &masked.Hosts[i].Server
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

	// 主机库条目凭证同步保留（前端回传掩码时恢复磁盘原值/环境变量占位符）
	origHostMap := make(map[string]*ServerConfig)
	for i := range originalCfg.Hosts {
		origHostMap[originalCfg.Hosts[i].Name] = &originalCfg.Hosts[i].Server
	}
	for i := range newCfg.Hosts {
		h := &newCfg.Hosts[i]
		if origServer, exists := origHostMap[h.Name]; exists {
			if h.Server.Password == MaskSecret {
				h.Server.Password = origServer.Password
			}
			if h.Server.Passphrase == MaskSecret {
				h.Server.Passphrase = origServer.Passphrase
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

// IsDangerousRemotePath 检查远端清理路径是否属于高危系统目录或向上逃逸路径（使用标准 POSIX 规范路径计算）
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
	// 相对路径向上逃逸（如 ".."、 "../x"、"a/../.."）会越过 SFTP 登录家目录，同样禁止递归清理
	if clean == ".." || strings.HasPrefix(clean, "../") {
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
	for i := range cfg.Hosts {
		h := &cfg.Hosts[i]
		h.Server.Password = expandEnv(h.Server.Password)
		h.Server.Passphrase = expandEnv(h.Server.Passphrase)
		h.Server.PrivateKeyPath = expandEnv(h.Server.PrivateKeyPath)
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

	// 旧版 groups[].hooks → tagHooks 平滑迁移（幂等），随后进入统一校验
	migrateLegacyGroups(cfg)

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

		// 标签归一化与旧版 group 字段平滑迁移：
		// 未显式声明 tags 时沿用旧 group 值；两者皆空回退默认标签 "default"。
		// 迁移后清空 group，保存配置时不再序列化旧字段。
		svc.Tags = normalizeTags(svc.Tags)
		if len(svc.Tags) == 0 {
			if g := strings.ToLower(strings.TrimSpace(svc.Group)); g != "" {
				svc.Tags = []string{g}
			} else {
				svc.Tags = []string{DefaultTag}
			}
		}
		svc.Group = ""

		// 任务类型默认值
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

		// 服务器连接校验（hostRef 引用主机库的服务可省略内联连接信息，部署时由库值解析补齐）
		ref := strings.TrimSpace(svc.HostRef)
		if strings.TrimSpace(svc.Server.Host) == "" && ref == "" {
			return fmt.Errorf("service %q: server.host is required", svc.Name)
		}
		if svc.Server.Port <= 0 {
			svc.Server.Port = 22
		}
		if strings.TrimSpace(svc.Server.Username) == "" && ref == "" {
			return fmt.Errorf("service %q: server.username is required", svc.Name)
		}
		if strings.TrimSpace(svc.Server.Password) == "" && strings.TrimSpace(svc.Server.PrivateKeyPath) == "" && ref == "" {
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

	// 主机库校验
	hostNames := make(map[string]bool)
	for i := range cfg.Hosts {
		h := &cfg.Hosts[i]
		hName := strings.TrimSpace(h.Name)
		if hName == "" {
			return fmt.Errorf("host at index %d: name is required", i)
		}
		lowerHost := strings.ToLower(hName)
		if hostNames[lowerHost] {
			return fmt.Errorf("duplicate host name %q", h.Name)
		}
		hostNames[lowerHost] = true
		if strings.TrimSpace(h.Server.Host) == "" {
			return fmt.Errorf("host %q: server.host is required", h.Name)
		}
		if h.Server.Port <= 0 {
			h.Server.Port = 22
		}
		if strings.TrimSpace(h.Server.Username) == "" {
			return fmt.Errorf("host %q: server.username is required", h.Name)
		}
		if strings.TrimSpace(h.Server.Password) == "" && strings.TrimSpace(h.Server.PrivateKeyPath) == "" {
			return fmt.Errorf("host %q: either server.password or server.privateKeyPath must be provided", h.Name)
		}
	}

	// hostRef 引用存在性校验（防止部署期才暴露悬空引用）
	for i := range cfg.Services {
		if ref := strings.TrimSpace(cfg.Services[i].HostRef); ref != "" && cfg.FindHost(ref) == nil {
			return fmt.Errorf("service %q: hostRef %q not found in hosts[]", cfg.Services[i].Name, ref)
		}
	}

	// 标签批次钩子校验
	tagHookNames := make(map[string]bool)
	for i := range cfg.TagHooks {
		th := &cfg.TagHooks[i]
		thName := strings.TrimSpace(th.Name)
		if thName == "" {
			return fmt.Errorf("tagHook at index %d: name is required", i)
		}
		th.Name = thName
		lowerName := strings.ToLower(thName)
		if tagHookNames[lowerName] {
			return fmt.Errorf("duplicate tagHook name %q", th.Name)
		}
		tagHookNames[lowerName] = true
	}

	return nil
}

// ExampleConfig 返回示例配置结构体
func ExampleConfig() *DeployConfig {
	example := DeployConfig{
		Parallel: boolPtr(true),
		Hooks: BatchHooks{
			PreDeploy: []string{
				"echo \"[Global Pre-Hook] 全局顶层前置检查（仅执行一次）...\"",
			},
			PostDeploy: []string{
				"echo \"[Global Post-Hook] 全局顶层后置通知（全部成功后仅执行一次）...\"",
			},
		},
		TagHooks: []TagHookConfig{
			{
				Name:        "infra",
				Description: "基础设施与数据库维护（部署携带 infra 标签的服务时触发）",
				Hooks: BatchHooks{
					PreDeploy: []string{
						"echo \"[Infra Tag Pre-Hook] 基础设施前置预检...\"",
					},
					PostDeploy: []string{
						"echo \"[Infra Tag Post-Hook] 基础设施就绪！\"",
					},
				},
			},
			{
				Name:        "backend",
				Description: "后端核心微服务集群（本地仅编译打包一次）",
				Hooks: BatchHooks{
					PreDeploy: []string{
						"echo \"[Backend Tag Pre-Hook] 后端本地编译构建打包...\"",
					},
					PostDeploy: []string{
						"echo \"[Backend Tag Post-Hook] 后端集群全部节点上线成功！\"",
					},
				},
			},
			{
				Name:        "frontend",
				Description: "前端静态与 CDN 资源（本地统一构建一次）",
				Hooks: BatchHooks{
					PreDeploy: []string{
						"echo \"[Frontend Tag Pre-Hook] 前端本地 npm run build 统一打包...\"",
					},
					PostDeploy: []string{
						"echo \"[Frontend Tag Post-Hook] 前端全部节点发布成功！\"",
					},
				},
			},
		},
		Services: []ServiceConfig{
			{
				Name:  "db-migrate-01",
				Tags:  []string{"infra", "data"},
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
				Tags:  []string{"backend"},
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
				Tags:  []string{"frontend", "cdn"},
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
