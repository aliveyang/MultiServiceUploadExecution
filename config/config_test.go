package config

import (
	"os"
	"path/filepath"
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
			}
			for _, p := range escapes {
				if !IsDangerousRemotePath(p) {
					t.Errorf("expected escaped path %q to be detected as dangerous", p)
				}
			}
		}

