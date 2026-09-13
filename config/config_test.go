package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseJSONConfig(t *testing.T) {
	jsonContent := `{
		"parallel": true,
		"services": [
			{
				"name": "test-svc",
				"server": {
					"host": "127.0.0.1",
					"port": 2222,
					"username": "deploy",
					"password": "secretpassword"
				},
				"upload": {
					"localPath": "./build",
					"remotePath": "/app/build",
					"exclude": ["*.tmp"]
				},
				"hooks": {
					"preUploadLocal": "echo 'single string command'",
					"preUploadRemote": ["cmd 1", "cmd 2"],
					"postUploadRemote": "systemctl reload app",
					"postUploadLocal": ["echo 'done'"]
				}
			}
		]
	}`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "deploy.json")
	if err := os.WriteFile(configPath, []byte(jsonContent), 0644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if !cfg.IsParallel() {
		t.Errorf("expected IsParallel to be true")
	}

	if len(cfg.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(cfg.Services))
	}

	svc := cfg.Services[0]
	if svc.Name != "test-svc" {
		t.Errorf("expected name 'test-svc', got %q", svc.Name)
	}
	if svc.Server.Port != 2222 {
		t.Errorf("expected port 2222, got %d", svc.Server.Port)
	}
	if svc.Server.ConnectTimeout != 15 {
		t.Errorf("expected default timeout 15, got %d", svc.Server.ConnectTimeout)
	}

	// 检查 single command unmarshaled as slice with 1 element
	if len(svc.Hooks.PreUploadLocal) != 1 || svc.Hooks.PreUploadLocal[0] != "echo 'single string command'" {
		t.Errorf("unexpected PreUploadLocal: %v", svc.Hooks.PreUploadLocal)
	}
	if len(svc.Hooks.PreUploadRemote) != 2 {
		t.Errorf("unexpected PreUploadRemote len: %d", len(svc.Hooks.PreUploadRemote))
	}
}

func TestParseYAMLConfig(t *testing.T) {
	yamlContent := `
parallel: false
services:
  - name: yaml-svc
    server:
      host: 192.168.1.50
      username: admin
      password: pass
    upload:
      localPath: ./dist
      remotePath: /var/www
    hooks:
      preUploadLocal:
        - echo 1
        - echo 2
      postUploadRemote: systemctl restart nginx
`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "deploy.yaml")
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.IsParallel() {
		t.Errorf("expected IsParallel to be false")
	}

	if len(cfg.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(cfg.Services))
	}

	svc := cfg.Services[0]
	if svc.Server.Port != 22 {
		t.Errorf("expected default port 22, got %d", svc.Server.Port)
	}
	if len(svc.Hooks.PostUploadRemote) != 1 || svc.Hooks.PostUploadRemote[0] != "systemctl restart nginx" {
		t.Errorf("unexpected PostUploadRemote: %v", svc.Hooks.PostUploadRemote)
	}
}

func TestValidateConfigErrors(t *testing.T) {
	tests := []struct {
		name    string
		cfg     DeployConfig
		wantErr string
	}{
		{
			name: "no services",
			cfg: DeployConfig{
				Services: []ServiceConfig{},
			},
			wantErr: "no services defined",
		},
		{
			name: "missing host",
			cfg: DeployConfig{
				Services: []ServiceConfig{
					{
						Server: ServerConfig{
							Username: "root",
							Password: "123",
						},
					},
				},
			},
			wantErr: "server.host is required",
		},
		{
			name: "missing auth",
			cfg: DeployConfig{
				Services: []ServiceConfig{
					{
						Server: ServerConfig{
							Host:     "1.1.1.1",
							Username: "root",
						},
					},
				},
			},
			wantErr: "either server.password or server.privateKeyPath must be provided",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAndNormalize(&tt.cfg)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
		})
	}
}

func TestEnvExpansion(t *testing.T) {
	t.Setenv("TEST_DEPLOY_PASS", "super-secret-password-from-env")
	t.Setenv("TEST_KEY_PATH", "/tmp/id_rsa_test")

	jsonContent := `{
			"services": [
				{
					"name": "env-svc",
					"server": {
						"host": "10.0.0.1",
						"username": "root",
						"password": "${TEST_DEPLOY_PASS}",
						"privateKeyPath": "${TEST_KEY_PATH}"
					}
				}
			]
		}`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "deploy.json")
	if err := os.WriteFile(configPath, []byte(jsonContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.Services[0].Server.Password != "super-secret-password-from-env" {
		t.Errorf("expected password to be expanded from env, got %q", cfg.Services[0].Server.Password)
	}
	if cfg.Services[0].Server.PrivateKeyPath != "/tmp/id_rsa_test" {
		t.Errorf("expected privateKeyPath to be expanded from env, got %q", cfg.Services[0].Server.PrivateKeyPath)
	}
}

func TestDangerousPathProtection(t *testing.T) {
	// 1. 验证 IsDangerousRemotePath 识别能力
	dangerousList := []string{"/", "/root", "/var", "/etc", "/usr", "/bin", "/home", "\\etc", "\\root"}
	for _, p := range dangerousList {
		if !IsDangerousRemotePath(p) {
			t.Errorf("expected %q to be identified as dangerous", p)
		}
	}

	safeList := []string{"/opt/app/my-service", "/var/www/html/dist", "/data/projects/node-api"}
	for _, p := range safeList {
		if IsDangerousRemotePath(p) {
			t.Errorf("expected %q to be identified as safe", p)
		}
	}

	// 2. 验证 ValidateAndNormalize 阻断 cleanRemote = true + 高危路径
	cfg := DeployConfig{
		Services: []ServiceConfig{
			{
				Name: "danger-svc",
				Server: ServerConfig{
					Host:     "1.1.1.1",
					Username: "root",
					Password: "123",
				},
				Upload: UploadConfig{
					LocalPath:   "./dist",
					RemotePath:  "/etc",
					CleanRemote: true,
				},
			},
		},
	}

	if err := ValidateAndNormalize(&cfg); err == nil {
		t.Fatalf("expected error when CleanRemote is true on /etc, got nil")
	}
}

func TestParseGlobalHooks(t *testing.T) {
	jsonContent := `{
				"hooks": {
					"preDeploy": ["echo 'pre 1'", "echo 'pre 2'"],
					"postDeploy": "echo 'post single'"
				},
				"services": [
					{
						"name": "svc-1",
						"server": {
							"host": "127.0.0.1",
							"username": "root",
							"password": "pwd"
						}
					}
				]
			}`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "deploy.json")
	if err := os.WriteFile(configPath, []byte(jsonContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if len(cfg.Hooks.PreDeploy) != 2 {
		t.Fatalf("expected 2 preDeploy hooks, got %d", len(cfg.Hooks.PreDeploy))
	}
	if cfg.Hooks.PreDeploy[0] != "echo 'pre 1'" {
		t.Errorf("unexpected preDeploy[0]: %s", cfg.Hooks.PreDeploy[0])
	}
	if len(cfg.Hooks.PostDeploy) != 1 || cfg.Hooks.PostDeploy[0] != "echo 'post single'" {
		t.Fatalf("expected 1 postDeploy hook 'echo 'post single'', got %v", cfg.Hooks.PostDeploy)
	}
}

func TestMaskConfigAndMergeSecrets(t *testing.T) {
	original := &DeployConfig{
		Services: []ServiceConfig{
			{
				Name: "svc-alpha",
				Server: ServerConfig{
					Host:       "1.1.1.1",
					Username:   "root",
					Password:   "${ENV_SECRET_PASS}",
					Passphrase: "my-passphrase",
				},
			},
		},
	}

	// 1. 测试脱敏
	masked := MaskConfig(original)
	if masked.Services[0].Server.Password != MaskSecret {
		t.Errorf("expected password to be masked, got %q", masked.Services[0].Server.Password)
	}
	if masked.Services[0].Server.Passphrase != MaskSecret {
		t.Errorf("expected passphrase to be masked, got %q", masked.Services[0].Server.Passphrase)
	}
	// 原对象不应受影响
	if original.Services[0].Server.Password != "${ENV_SECRET_PASS}" {
		t.Errorf("original config password modified unexpectedly")
	}

	// 2. 测试合并保留原有秘密
	submittedFromWeb := &DeployConfig{
		Services: []ServiceConfig{
			{
				Name: "svc-alpha",
				Server: ServerConfig{
					Host:       "1.1.1.1",
					Username:   "root",
					Password:   MaskSecret, // 前端未修改，原样提交掩码
					Passphrase: MaskSecret,
				},
			},
		},
	}
	MergePreservingSecrets(submittedFromWeb, original)
	if submittedFromWeb.Services[0].Server.Password != "${ENV_SECRET_PASS}" {
		t.Errorf("expected password to be preserved from original, got %q", submittedFromWeb.Services[0].Server.Password)
	}
	if submittedFromWeb.Services[0].Server.Passphrase != "my-passphrase" {
		t.Errorf("expected passphrase to be preserved, got %q", submittedFromWeb.Services[0].Server.Passphrase)
	}

	// 3. 测试用户主动修改了新密码
	submittedNewPass := &DeployConfig{
		Services: []ServiceConfig{
			{
				Name: "svc-alpha",
				Server: ServerConfig{
					Host:     "1.1.1.1",
					Username: "root",
					Password: "newly-entered-password",
				},
			},
		},
	}
	MergePreservingSecrets(submittedNewPass, original)
	if submittedNewPass.Services[0].Server.Password != "newly-entered-password" {
		t.Errorf("expected newly entered password to be preserved, got %q", submittedNewPass.Services[0].Server.Password)
	}
}

func TestDangerousPathProtectionPOSIXEscape(t *testing.T) {
	escapes := []string{
		"/var/../etc",
		"/opt/../root",
		"/usr/bin/../../bin",
		"/data/../../",
		"..",
		"../..",
		"../secrets",
		"app/../../home",
	}
	for _, p := range escapes {
		if !IsDangerousRemotePath(p) {
			t.Errorf("expected escaped path %q to be detected as dangerous", p)
		}
	}
}

func TestServiceGroupTypeStageDefaults(t *testing.T) {
	jsonContent := `{
			"services": [
				{
					"name": "default-svc",
					"server": {
						"host": "127.0.0.1",
						"username": "root",
						"password": "pwd"
					}
				},
				{
					"name": "custom-svc",
					"group": "backend",
					"type": "exec_only",
					"stage": 2,
					"server": {
						"host": "127.0.0.1",
						"username": "root",
						"password": "pwd"
					}
				}
			]
		}`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "deploy.json")
	if err := os.WriteFile(configPath, []byte(jsonContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// 检查默认值回填
	s1 := cfg.Services[0]
	if len(s1.Tags) != 1 || s1.Tags[0] != DefaultTag {
		t.Errorf("expected default tag %q, got %v", DefaultTag, s1.Tags)
	}
	if s1.Type != "" {
		t.Errorf("expected legacy type field cleared after migration, got %q", s1.Type)
	}
	if s1.Steps.TypeLabel() != DeployTypeStandard {
		t.Errorf("expected all steps enabled by default, got %q", s1.Steps.TypeLabel())
	}
	if s1.Stage != 1 {
		t.Errorf("expected default stage 1, got %d", s1.Stage)
	}

	// 检查显式设置：旧 group 字段迁移为标签且原字段清空
	s2 := cfg.Services[1]
	if len(s2.Tags) != 1 || s2.Tags[0] != "backend" {
		t.Errorf("expected tag 'backend', got %v", s2.Tags)
	}
	if s2.Group != "" {
		t.Errorf("expected legacy group field cleared after migration, got %q", s2.Group)
	}
	// 旧 type=exec_only 迁移为关闭 upload 步骤，其余步骤保持启用
	if s2.Type != "" {
		t.Errorf("expected legacy type field cleared after migration, got %q", s2.Type)
	}
	if s2.Steps.IsStepEnabled(StepUpload) {
		t.Errorf("expected upload step disabled after exec_only migration")
	}
	if !s2.Steps.IsStepEnabled(StepPreUploadRemote) || !s2.Steps.IsStepEnabled(StepPostUploadRemote) {
		t.Errorf("expected remote hook steps enabled after exec_only migration")
	}
	if s2.Steps.TypeLabel() != DeployTypeExecOnly {
		t.Errorf("expected migrated steps to label as %q, got %q", DeployTypeExecOnly, s2.Steps.TypeLabel())
	}
	if s2.Stage != 2 {
		t.Errorf("expected stage 2, got %d", s2.Stage)
	}
}

func TestInvalidDeployType(t *testing.T) {
	cfg := &DeployConfig{
		Services: []ServiceConfig{
			{
				Name: "bad-svc",
				Type: "invalid_type",
				Server: ServerConfig{
					Host:     "127.0.0.1",
					Username: "root",
					Password: "pwd",
				},
			},
		},
	}
	err := ValidateAndNormalize(cfg)
	if err == nil {
		t.Fatalf("expected error for invalid deploy type, got nil")
	}
}

// TestLegacySyncOnlyTypeMigration 旧 type=sync_only 迁移为关闭两个远端钩子步骤，且不再序列化 type 字段
func TestLegacySyncOnlyTypeMigration(t *testing.T) {
	cfg := &DeployConfig{
		Services: []ServiceConfig{
			{
				Name:   "sync-svc",
				Type:   DeployTypeSyncOnly,
				Server: ServerConfig{Host: "127.0.0.1", Username: "root", Password: "pwd"},
				Upload: UploadConfig{LocalPath: "./dist", RemotePath: "/opt/app"},
			},
		},
	}
	if err := ValidateAndNormalize(cfg); err != nil {
		t.Fatalf("ValidateAndNormalize failed: %v", err)
	}
	svc := cfg.Services[0]
	if svc.Type != "" {
		t.Errorf("expected legacy type field cleared after migration, got %q", svc.Type)
	}
	if svc.Steps.IsStepEnabled(StepPreUploadRemote) || svc.Steps.IsStepEnabled(StepPostUploadRemote) {
		t.Errorf("expected remote hook steps disabled after sync_only migration")
	}
	if !svc.Steps.IsStepEnabled(StepUpload) || !svc.Steps.IsStepEnabled(StepPreUploadLocal) || !svc.Steps.IsStepEnabled(StepPostUploadLocal) {
		t.Errorf("expected upload and local hook steps enabled after sync_only migration")
	}

	// 迁移结果序列化后不应再出现 type 字段，且 steps 显式落盘
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if strings.Contains(string(data), `"type"`) {
		t.Errorf("expected type field to be dropped after migration, got %s", data)
	}
	if !strings.Contains(string(data), `"preUploadRemote":false`) || !strings.Contains(string(data), `"postUploadRemote":false`) {
		t.Errorf("expected disabled steps serialized as false, got %s", data)
	}
}

// TestExplicitStepsConfig 显式 steps 开关直接生效且展示标签按组合推导
func TestExplicitStepsConfig(t *testing.T) {
	cfg := &DeployConfig{
		Services: []ServiceConfig{
			{
				Name:   "custom-svc",
				Server: ServerConfig{Host: "127.0.0.1", Username: "root", Password: "pwd"},
				Steps: StepsConfig{
					PreUploadRemote: boolPtr(false),
					Upload:          boolPtr(false),
				},
			},
		},
	}
	if err := ValidateAndNormalize(cfg); err != nil {
		t.Fatalf("ValidateAndNormalize failed: %v", err)
	}
	svc := cfg.Services[0]
	if svc.Steps.IsStepEnabled(StepPreUploadRemote) || svc.Steps.IsStepEnabled(StepUpload) {
		t.Errorf("expected explicitly disabled steps to stay disabled")
	}
	if !svc.Steps.IsStepEnabled(StepPostUploadLocal) {
		t.Errorf("expected unspecified steps to default to enabled")
	}
	if svc.Steps.TypeLabel() != DeployTypeCustom {
		t.Errorf("expected mixed steps to label as %q, got %q", DeployTypeCustom, svc.Steps.TypeLabel())
	}
}

// TestLegacyScenariosIgnored 旧版 scenarios 字段加载时被忽略（场景预设已移除），其余配置正常生效
func TestLegacyScenariosIgnored(t *testing.T) {
	jsonContent := `{
			"scenarios": [
				{
					"name": "prod",
					"description": "生产全量发布",
					"groups": ["infra", "backend"]
				},
				{
					"name": "quick",
					"types": ["exec_only"]
				}
			],
			"services": [
				{
					"name": "s1",
					"group": "infra",
					"server": { "host": "127.0.0.1", "username": "root", "password": "pwd" }
				}
			]
		}`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "deploy.json")
	if err := os.WriteFile(configPath, []byte(jsonContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// scenarios 已移除：旧配置不报错，服务按 group → tags 迁移规则正常加载
	if len(cfg.Services) != 1 || !cfg.Services[0].HasTag("infra") {
		t.Fatalf("expected legacy group migrated to tag 'infra', got %+v", cfg.Services[0])
	}
	if len(cfg.TagHooks) != 0 {
		t.Errorf("expected no tagHooks derived from scenarios section, got %v", cfg.TagHooks)
	}
}

// TestTagHookParsingAndLegacyGroupMigration 验证标签钩子解析与旧版 groups[].hooks 的自动迁移
func TestTagHookParsingAndLegacyGroupMigration(t *testing.T) {
	jsonContent := `{
			"tagHooks": [
				{
					"name": "backend",
					"description": "后端集群",
					"hooks": { "preDeploy": "go build" }
				}
			],
			"groups": [
				{
					"name": "frontend",
					"description": "前端组",
					"hooks": {
						"preDeploy": ["npm run build"],
						"postDeploy": "echo 'frontend done'"
					}
				},
				{
					"name": "BACKEND",
					"hooks": { "preDeploy": ["should-not-override"] }
				}
			],
			"services": [
				{
					"name": "web-1",
					"group": "frontend",
					"server": { "host": "127.0.0.1", "username": "root", "password": "pwd" }
				}
			]
		}`

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "deploy.json")
	if err := os.WriteFile(configPath, []byte(jsonContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// 旧 groups 已迁移清空；显式 tagHooks 与迁移结果合并（同名时以显式配置为准）
	if cfg.Groups != nil {
		t.Errorf("expected legacy groups cleared after migration, got %v", cfg.Groups)
	}
	if len(cfg.TagHooks) != 2 {
		t.Fatalf("expected 2 tagHooks after migration, got %d: %+v", len(cfg.TagHooks), cfg.TagHooks)
	}

	frontend := cfg.FindTagHook("FRONTEND")
	if frontend == nil || frontend.Name != "frontend" {
		t.Fatalf("FindTagHook case-insensitive lookup failed")
	}
	if len(frontend.Hooks.PreDeploy) != 1 || frontend.Hooks.PreDeploy[0] != "npm run build" {
		t.Errorf("unexpected preDeploy hook in migrated tag hook: %v", frontend.Hooks.PreDeploy)
	}
	if len(frontend.Hooks.PostDeploy) != 1 || frontend.Hooks.PostDeploy[0] != "echo 'frontend done'" {
		t.Errorf("unexpected postDeploy hook in migrated tag hook: %v", frontend.Hooks.PostDeploy)
	}

	backend := cfg.FindTagHook("backend")
	if backend == nil || len(backend.Hooks.PreDeploy) != 1 || backend.Hooks.PreDeploy[0] != "go build" {
		t.Errorf("expected explicit tagHook 'backend' to win over legacy group, got %+v", backend)
	}

	// 服务旧 group 字段迁移为标签且原字段清空
	if !cfg.Services[0].HasTag("frontend") || cfg.Services[0].Group != "" {
		t.Errorf("expected service group migrated to tags, got tags=%v group=%q", cfg.Services[0].Tags, cfg.Services[0].Group)
	}

	// 重复标签钩子名校验（大小写不敏感）
	dupTagHookCfg := &DeployConfig{
		TagHooks: []TagHookConfig{
			{Name: "backend"},
			{Name: "BACKEND"},
		},
		Services: []ServiceConfig{
			{
				Name:   "s1",
				Server: ServerConfig{Host: "127.0.0.1", Username: "root", Password: "pwd"},
			},
		},
	}
	if err := ValidateAndNormalize(dupTagHookCfg); err == nil {
		t.Fatalf("expected error for duplicate tagHook names, got nil")
	}
}
