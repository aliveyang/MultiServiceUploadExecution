package deployer

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"multi-service-deploy/config"
	"multi-service-deploy/logger"

	"github.com/pkg/sftp"
)

// UploadStats 上传统计信息
type UploadStats struct {
	TotalFiles int
	TotalBytes int64
	Duration   time.Duration
}

// SFTPUploader 封装 SFTP 文件与目录传输
type SFTPUploader struct {
	sftpClient *sftp.Client
	log        *logger.ServiceLogger
}

// NewSFTPUploader 创建 SFTP 上传器
func NewSFTPUploader(sshClient *SSHClient, log *logger.ServiceLogger) (*SFTPUploader, error) {
	client, err := sftp.NewClient(sshClient.client)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize SFTP subsystem: %w", err)
	}

	return &SFTPUploader{
		sftpClient: client,
		log:        log,
	}, nil
}

// Close 关闭 SFTP 客户端
func (u *SFTPUploader) Close() error {
	if u.sftpClient != nil {
		return u.sftpClient.Close()
	}
	return nil
}

// Upload 执行上传任务（支持单个文件或整个目录递归上传）
func (u *SFTPUploader) Upload(cfg config.UploadConfig) (*UploadStats, error) {
	localPath := cfg.LocalPath
	remotePath := cfg.RemotePath

	// 统一转换远端路径为 linux 风格正斜杠
	remotePath = strings.ReplaceAll(remotePath, "\\", "/")

	localInfo, err := os.Stat(localPath)
	if err != nil {
		return nil, fmt.Errorf("local path %q not accessible: %w", localPath, err)
	}

	startTime := time.Now()
	stats := &UploadStats{}

		// 可选：清理远端目录
		if cfg.CleanRemote {
			if config.IsDangerousRemotePath(remotePath) {
				return nil, fmt.Errorf("refusing to clean dangerous remote path %q", remotePath)
			}
			u.log.Info("Cleaning remote path: %s", remotePath)
			_ = u.sftpClient.RemoveAll(remotePath)
		}

	if localInfo.IsDir() {
		u.log.Info("Uploading directory [SFTP]: %s -> %s", localPath, remotePath)
		if err := u.uploadDir(localPath, remotePath, cfg.Exclude, stats); err != nil {
			return nil, err
		}
	} else {
		u.log.Info("Uploading single file [SFTP]: %s -> %s", localPath, remotePath)
		targetFile := remotePath
		// 如果远端路径以 / 结尾，则保留本地文件名
		if strings.HasSuffix(remotePath, "/") {
			targetFile = path.Join(remotePath, filepath.Base(localPath))
		}
		if err := u.uploadFile(localPath, targetFile, stats); err != nil {
			return nil, err
		}
	}

	stats.Duration = time.Since(startTime)
	u.log.Success("Uploaded %d files (%s) in %v", stats.TotalFiles, formatBytes(stats.TotalBytes), stats.Duration.Round(time.Millisecond))
	return stats, nil
}

// uploadDir 递归上传本地目录
func (u *SFTPUploader) uploadDir(localDir, remoteDir string, excludePatterns []string, stats *UploadStats) error {
	// 确保远端根目录存在
	if err := u.sftpClient.MkdirAll(remoteDir); err != nil {
		return fmt.Errorf("failed to create remote dir %q: %w", remoteDir, err)
	}

	entries, err := os.ReadDir(localDir)
	if err != nil {
		return fmt.Errorf("failed to read local dir %q: %w", localDir, err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if shouldExclude(name, excludePatterns) {
			u.log.Info("  [Excluded] %s", filepath.Join(localDir, name))
			continue
		}

		localSubPath := filepath.Join(localDir, name)
		remoteSubPath := path.Join(remoteDir, name)

		if entry.IsDir() {
			if err := u.uploadDir(localSubPath, remoteSubPath, excludePatterns, stats); err != nil {
				return err
			}
		} else {
			if err := u.uploadFile(localSubPath, remoteSubPath, stats); err != nil {
				return err
			}
		}
	}

	return nil
}

// uploadFile 上传单个文件
func (u *SFTPUploader) uploadFile(localFilePath, remoteFilePath string, stats *UploadStats) error {
	remoteDir := path.Dir(remoteFilePath)
	if err := u.sftpClient.MkdirAll(remoteDir); err != nil {
		return fmt.Errorf("failed to create remote parent dir %q: %w", remoteDir, err)
	}

	srcFile, err := os.Open(localFilePath)
	if err != nil {
		return fmt.Errorf("failed to open local file %q: %w", localFilePath, err)
	}
	defer srcFile.Close()

	srcStat, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat local file %q: %w", localFilePath, err)
	}

	dstFile, err := u.sftpClient.Create(remoteFilePath)
	if err != nil {
		return fmt.Errorf("failed to create remote file %q: %w", remoteFilePath, err)
	}
	defer dstFile.Close()

	n, err := io.Copy(dstFile, srcFile)
	if err != nil {
		return fmt.Errorf("failed to transfer file to %q: %w", remoteFilePath, err)
	}

	// 尝试同步文件权限
	_ = u.sftpClient.Chmod(remoteFilePath, srcStat.Mode())

	stats.TotalFiles++
	stats.TotalBytes += n
	return nil
}

// shouldExclude 判断当前文件/目录名是否匹配排除规则
func shouldExclude(name string, patterns []string) bool {
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		// 精确匹配
		if name == pattern {
			return true
		}
		// 通配符匹配 (如 *.log, .git*)
		if matched, _ := filepath.Match(pattern, name); matched {
			return true
		}
	}
	return false
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
