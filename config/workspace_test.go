package config

import (
	"path/filepath"
	"testing"
)

// TestResolveWorkspaceDir 验证工作空间根目录解析不产生 workspaces/workspaces 嵌套
func TestResolveWorkspaceDir(t *testing.T) {
	cases := map[string]string{
		"deploy.json":             "workspaces",
		"conf/deploy.yaml":        filepath.Join("conf", "workspaces"),
		"workspaces/default.json": "workspaces",
		"workspaces/prod.yaml":    "workspaces",
	}
	for in, want := range cases {
		if got := ResolveWorkspaceDir(in); got != want {
			t.Errorf("ResolveWorkspaceDir(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsValidWorkspaceID(t *testing.T) {
	validIDs := []string{
		"default",
		"prod",
		"dev-01",
		"staging_test",
		"集群A",
	}
	for _, id := range validIDs {
		if !IsValidWorkspaceID(id) {
			t.Errorf("expected %q to be valid workspace id", id)
		}
	}

	invalidIDs := []string{
		"",
		" ",
		"../hack",
		"a/b",
		`a\b`,
		"workspace:1",
		"workspace*test",
		"space in name",
	}
	for _, id := range invalidIDs {
		if IsValidWorkspaceID(id) {
			t.Errorf("expected %q to be invalid workspace id", id)
		}
	}
}

func TestWorkspaceLifecycleAndMigration(t *testing.T) {
	tmpDir := t.TempDir()
	wsDir := filepath.Join(tmpDir, "workspaces")
	oldConfigFile := filepath.Join(tmpDir, "old_deploy.json")

	// 1. 创建一份模拟的旧配置文件
	oldCfg := ExampleConfig()
	oldCfg.Services[0].Name = "migrated-legacy-service"
	if err := SaveWorkspaceConfig(tmpDir, "old_deploy", oldCfg); err != nil {
		t.Fatalf("failed to prepare old config: %v", err)
	}

	// 2. EnsureWorkspaceDir 并自动纳管旧配置至 default 空间
	if err := EnsureWorkspaceDir(wsDir, oldConfigFile); err != nil {
		t.Fatalf("EnsureWorkspaceDir failed: %v", err)
	}

	// 3. 验证 default 空间是否包含旧配置中的服务
	defaultPath, err := GetWorkspacePath(wsDir, DefaultWorkspaceID)
	if err != nil {
		t.Fatalf("GetWorkspacePath failed: %v", err)
	}
	loadedCfg, err := LoadRawConfig(defaultPath)
	if err != nil {
		t.Fatalf("failed to load migrated default config: %v", err)
	}
	if len(loadedCfg.Services) == 0 || loadedCfg.Services[0].Name != "migrated-legacy-service" {
		t.Errorf("expected migrated service name 'migrated-legacy-service', got %+v", loadedCfg.Services)
	}

	// 4. ListWorkspaces 检查
	list, err := ListWorkspaces(wsDir)
	if err != nil {
		t.Fatalf("ListWorkspaces failed: %v", err)
	}
	if len(list) != 1 || list[0].ID != DefaultWorkspaceID || !list[0].IsDefault {
		t.Fatalf("unexpected workspace list: %+v", list)
	}

	// 5. CreateWorkspace 创建新工作区
	prodInfo, err := CreateWorkspace(wsDir, "prod", "生产环境", loadedCfg)
	if err != nil {
		t.Fatalf("CreateWorkspace failed: %v", err)
	}
	if prodInfo.ID != "prod" || prodInfo.IsDefault {
		t.Errorf("unexpected prod info: %+v", prodInfo)
	}

	// 重复创建应报错
	if _, err := CreateWorkspace(wsDir, "prod", "生产环境", nil); err == nil {
		t.Errorf("expected error when creating duplicate workspace")
	}

	// 6. 再次 ListWorkspaces，验证排序：default 必须在第 0 位
	list2, err := ListWorkspaces(wsDir)
	if err != nil {
		t.Fatalf("ListWorkspaces 2 failed: %v", err)
	}
	if len(list2) != 2 || list2[0].ID != DefaultWorkspaceID {
		t.Errorf("expected default at index 0, got %+v", list2)
	}

	// 7. DeleteWorkspace：禁止删除 default
	if err := DeleteWorkspace(wsDir, DefaultWorkspaceID); err == nil {
		t.Errorf("expected error when deleting default workspace, but got nil")
	}

	// 删除 prod 工作区
	if err := DeleteWorkspace(wsDir, "prod"); err != nil {
		t.Fatalf("DeleteWorkspace prod failed: %v", err)
	}

	// 再次删除不存在的工作区应报错
	if err := DeleteWorkspace(wsDir, "prod"); err == nil {
		t.Errorf("expected error when deleting non-existent workspace")
	}
}
