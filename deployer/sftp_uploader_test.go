package deployer

import (
	"strings"
	"testing"

	"multi-service-deploy/config"
	"multi-service-deploy/logger"
)

func TestShouldExclude(t *testing.T) {
	patterns := []string{".git", "*.log", "node_modules", "temp*"}

	cases := []struct {
		name     string
		expected bool
	}{
		{".git", true},
		{"error.log", true},
		{"app.LOG", false}, // filepath.Match on Linux is case-sensitive
		{"node_modules", true},
		{"temp_dir", true},
		{"main.go", false},
		{"build.js", false},
	}

	for _, c := range cases {
		result := shouldExclude(c.name, patterns)
		if result != c.expected {
			t.Errorf("shouldExclude(%q) = %v, expected %v", c.name, result, c.expected)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	if formatBytes(500) != "500 B" {
		t.Errorf("unexpected 500 B format: %s", formatBytes(500))
	}
	if formatBytes(1024) != "1.00 KB" {
		t.Errorf("unexpected 1.00 KB format: %s", formatBytes(1024))
	}
		if formatBytes(1048576*2) != "2.00 MB" {
			t.Errorf("unexpected 2.00 MB format: %s", formatBytes(1048576*2))
		}
	}

	func TestCleanRemoteDangerousPathRejected(t *testing.T) {
		uploader := &SFTPUploader{
			log: logger.NewServiceLogger("test-svc", 0),
		}

		// 测试当 localPath 存在但 remotePath 为危险目录时，Upload 能够拒绝
		tmpDir := t.TempDir()
		cfg := config.UploadConfig{
			LocalPath:   tmpDir,
			RemotePath:  "/etc",
			CleanRemote: true,
		}

		_, err := uploader.Upload(cfg)
		if err == nil {
			t.Fatalf("expected error when cleanRemote is true on /etc, got nil")
		}
		if !strings.Contains(err.Error(), "dangerous remote path") {
			t.Errorf("expected dangerous path error, got: %v", err)
		}
	}
