package deployer

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"multi-service-deploy/config"
	"multi-service-deploy/logger"

	"golang.org/x/crypto/ssh"
	"golang.org/x/text/encoding/simplifiedchinese"
)

// ExecuteLocalCommand 在本地执行单条命令行，并实时输出日志（向后兼容）
func ExecuteLocalCommand(command string, log *logger.ServiceLogger) error {
	return ExecuteLocalCommandContext(context.Background(), command, log)
}

// ExecuteLocalCommandContext 在本地执行单条命令行，支持 context 取消
func ExecuteLocalCommandContext(ctx context.Context, command string, log *logger.ServiceLogger) error {
	log.Info("[Local Exec] %s", command)

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		// 先切换控制台代码页为 UTF-8（65001），确保 echo 等内置命令的中文输出为 UTF-8；
		// 其余程序若仍输出 GBK，由 streamOutput 中的编码兜底转码处理
		cmd = exec.CommandContext(ctx, "cmd.exe", "/C", "chcp 65001 >nul 2>&1 && "+command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start local command %q: %w", command, err)
	}

	var wg sync.WaitGroup
	wg.Add(2)

	// 流式读取标准输出
	go func() {
		defer wg.Done()
		streamOutput(stdout, log)
	}()

	// 流式读取标准错误
	go func() {
		defer wg.Done()
		streamOutput(stderr, log)
	}()

	wg.Wait()

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("local command %q canceled: %w", command, ctx.Err())
		}
		return fmt.Errorf("local command %q failed: %w", command, err)
	}

	return nil
}

// SSHClient 封装 SSH 连接客户端
type SSHClient struct {
	client *ssh.Client
	server config.ServerConfig
	log    *logger.ServiceLogger
}

// NewSSHClient 创建并建立 SSH 连接（向后兼容）
func NewSSHClient(server config.ServerConfig, log *logger.ServiceLogger) (*SSHClient, error) {
	return NewSSHClientContext(context.Background(), server, log)
}

// NewSSHClientContext 创建并建立 SSH 连接，支持 Context 控制超时与中断
func NewSSHClientContext(ctx context.Context, server config.ServerConfig, log *logger.ServiceLogger) (*SSHClient, error) {
	authMethods := make([]ssh.AuthMethod, 0)

	// 支持私钥认证
	if strings.TrimSpace(server.PrivateKeyPath) != "" {
		keyBytes, err := os.ReadFile(server.PrivateKeyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read private key %q: %w", server.PrivateKeyPath, err)
		}

		var signer ssh.Signer
		if strings.TrimSpace(server.Passphrase) != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(keyBytes, []byte(server.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(keyBytes)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}

	// 支持密码认证
	if strings.TrimSpace(server.Password) != "" {
		authMethods = append(authMethods, ssh.Password(server.Password))
	}

	timeout := time.Duration(server.ConnectTimeout) * time.Second

	var hostKeyCallback ssh.HostKeyCallback
	if strings.TrimSpace(server.HostKeyFingerprint) != "" {
		expected := strings.TrimSpace(server.HostKeyFingerprint)
		hostKeyCallback = func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			fp := ssh.FingerprintSHA256(key)
			if fp != expected {
				return fmt.Errorf("host key fingerprint mismatch for %s: expected %s, got %s", hostname, expected, fp)
			}
			return nil
		}
	} else {
		hostKeyCallback = ssh.InsecureIgnoreHostKey()
	}

	sshConfig := &ssh.ClientConfig{
		User:            server.Username,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         timeout,
	}

	addr := net.JoinHostPort(server.Host, fmt.Sprintf("%d", server.Port))
	log.Info("Connecting to SSH %s@%s ...", server.Username, addr)

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SSH server %s: %w", addr, err)
	}

	ncc, chans, reqs, err := ssh.NewClientConn(conn, addr, sshConfig)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to establish SSH connection to %s: %w", addr, err)
	}
	client := ssh.NewClient(ncc, chans, reqs)

	log.Success("SSH connected to %s", addr)
	return &SSHClient{
		client: client,
		server: server,
		log:    log,
	}, nil
}

// Close 关闭 SSH 连接
func (s *SSHClient) Close() error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

// ExecuteRemoteCommand 在远程服务器上执行单条命令，实时流式输出（向后兼容）
func (s *SSHClient) ExecuteRemoteCommand(command string) error {
	return s.ExecuteRemoteCommandContext(context.Background(), command)
}

// ExecuteRemoteCommandContext 在远程服务器上执行单条命令，支持 Context 控制与优雅中断
func (s *SSHClient) ExecuteRemoteCommandContext(ctx context.Context, command string) error {
	s.log.Info("[Remote Exec] %s", command)

	session, err := s.client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create SSH session: %w", err)
	}
	defer session.Close()

	stdout, err := session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get remote stdout pipe: %w", err)
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to get remote stderr pipe: %w", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		streamOutput(stdout, s.log)
	}()

	go func() {
		defer wg.Done()
		streamOutput(stderr, s.log)
	}()

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = session.Signal(ssh.SIGINT)
			_ = session.Close()
		case <-done:
		}
	}()

	if err := session.Run(command); err != nil {
		wg.Wait()
		if ctx.Err() != nil {
			return fmt.Errorf("remote command canceled: %w", ctx.Err())
		}
		return fmt.Errorf("remote command %q failed: %w", command, err)
	}

	wg.Wait()
	return nil
}

// streamOutput 逐行读取输入流并写入彩色前缀日志
func streamOutput(r io.Reader, log *logger.ServiceLogger) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := decodeConsoleBytes(scanner.Bytes())
		if strings.TrimSpace(line) != "" {
			log.CommandOutput(line)
		}
	}
}

// decodeConsoleBytes 编码兜底转码：合法 UTF-8 直接放行；
// 非 UTF-8 字节（如中文 Windows 的 GBK/CP936 控制台输出）按 GBK 解码为 UTF-8，避免网页终端中文乱码
func decodeConsoleBytes(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	if decoded, err := simplifiedchinese.GBK.NewDecoder().Bytes(b); err == nil {
		return string(decoded)
	}
	return string(b)
}
