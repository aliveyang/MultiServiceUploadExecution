package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateAndNormalizeHosts(t *testing.T) {
	cfg := &DeployConfig{
		Hosts: []HostConfig{
			{Name: "edge-01", Server: ServerConfig{Host: "10.0.0.1", Username: "root", Password: "pwd"}},
		},
		Services: []ServiceConfig{
			{Name: "svc", Server: ServerConfig{Host: "10.0.0.9", Username: "root", Password: "pwd"}},
		},
	}
	if err := ValidateAndNormalize(cfg); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
	if cfg.Hosts[0].Server.Port != 22 {
		t.Errorf("expected default port 22, got %d", cfg.Hosts[0].Server.Port)
	}

	// 重复主机名
	dup := &DeployConfig{
		Hosts: []HostConfig{
			{Name: "edge-01", Server: ServerConfig{Host: "10.0.0.1", Username: "root", Password: "p"}},
			{Name: "EDGE-01", Server: ServerConfig{Host: "10.0.0.2", Username: "root", Password: "p"}},
		},
		Services: []ServiceConfig{{Name: "svc", Server: ServerConfig{Host: "10.0.0.9", Username: "r", Password: "p"}}},
	}
	if err := ValidateAndNormalize(dup); err == nil || !strings.Contains(err.Error(), "duplicate host") {
		t.Errorf("expected duplicate host error, got %v", err)
	}

	// 缺认证凭据
	noAuth := &DeployConfig{
		Hosts:    []HostConfig{{Name: "edge-01", Server: ServerConfig{Host: "10.0.0.1", Username: "root"}}},
		Services: []ServiceConfig{{Name: "svc", Server: ServerConfig{Host: "10.0.0.9", Username: "r", Password: "p"}}},
	}
	if err := ValidateAndNormalize(noAuth); err == nil || !strings.Contains(err.Error(), "privateKeyPath") {
		t.Errorf("expected missing credential error, got %v", err)
	}

	// 悬空 hostRef
	dangling := &DeployConfig{
		Hosts:    []HostConfig{{Name: "edge-01", Server: ServerConfig{Host: "10.0.0.1", Username: "root", Password: "p"}}},
		Services: []ServiceConfig{{Name: "svc", HostRef: "ghost", Server: ServerConfig{Username: "r", Password: "p"}}},
	}
	if err := ValidateAndNormalize(dangling); err == nil || !strings.Contains(err.Error(), "hostRef") {
		t.Errorf("expected dangling hostRef error, got %v", err)
	}
}

func TestResolveHostReferences(t *testing.T) {
	cfg := &DeployConfig{
		Hosts: []HostConfig{
			{Name: "edge-01", Server: ServerConfig{
				Host: "10.0.0.1", Port: 2222, Username: "deploy", Password: "${HOST_PWD}",
				HostKeyFingerprint: "SHA256:abc", ConnectTimeout: 15,
			}},
		},
		Services: []ServiceConfig{
			// 仅引用：完全继承库值
			{Name: "svc-a", HostRef: "edge-01"},
			// 引用 + 内联覆盖：内联非零字段优先
			{Name: "svc-b", HostRef: "edge-01", Server: ServerConfig{Username: "override"}},
		},
	}

	t.Setenv("HOST_PWD", "resolved-pwd")
	ExpandEnvVariables(cfg)
	if err := cfg.ResolveHostReferences(); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	a := cfg.Services[0].Server
	if a.Host != "10.0.0.1" || a.Port != 2222 || a.Username != "deploy" || a.Password != "resolved-pwd" || a.HostKeyFingerprint != "SHA256:abc" {
		t.Errorf("expected full inheritance from library, got %+v", a)
	}
	b := cfg.Services[1].Server
	if b.Username != "override" {
		t.Errorf("expected inline override to win, got %q", b.Username)
	}
	if b.Host != "10.0.0.1" || b.Port != 2222 {
		t.Errorf("expected non-overridden fields inherited, got %+v", b)
	}

	// 未知引用报错
	bad := &DeployConfig{Services: []ServiceConfig{{Name: "svc", HostRef: "ghost"}}}
	if err := bad.ResolveHostReferences(); err == nil {
		t.Errorf("expected error for unknown hostRef")
	}
}

func TestMaskAndMergePreserveHosts(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "deploy.json")
	orig := DeployConfig{
		Hosts: []HostConfig{
			{Name: "edge-01", Server: ServerConfig{Host: "10.0.0.1", Username: "root", Password: "${HOST_SECRET}", Passphrase: "real-pp"}},
		},
		Services: []ServiceConfig{{Name: "svc", Server: ServerConfig{Host: "10.0.0.1", Username: "root", Password: "p"}}},
	}
	data, _ := json.MarshalIndent(orig, "", "  ")
	if err := os.WriteFile(cfgPath, data, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	loaded, err := LoadRawConfig(cfgPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// 脱敏：主机库凭证同样掩码，不泄露环境变量占位符或明文
	masked := MaskConfig(loaded)
	if masked.Hosts[0].Server.Password != MaskSecret || masked.Hosts[0].Server.Passphrase != MaskSecret {
		t.Errorf("expected hosts credentials masked, got %+v", masked.Hosts[0].Server)
	}

	// 前端掩码原样回传后保存：恢复磁盘原值
	MergePreservingSecrets(masked, loaded)
	if masked.Hosts[0].Server.Password != "${HOST_SECRET}" || masked.Hosts[0].Server.Passphrase != "real-pp" {
		t.Errorf("expected hosts secrets preserved on save, got %+v", masked.Hosts[0].Server)
	}
}
