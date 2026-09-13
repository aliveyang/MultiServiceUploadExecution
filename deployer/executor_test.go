package deployer

import (
	"bytes"
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"multi-service-deploy/config"
	"multi-service-deploy/logger"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestExecuteLocalCommand(t *testing.T) {
	buf := captureDeployerLog(t)

	log := logger.NewServiceLogger("test-svc", 0)
	err := ExecuteLocalCommandContext(context.Background(), "echo hello world", log)
	if err != nil {
		t.Fatalf("ExecuteLocalCommandContext failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "hello world") {
		t.Errorf("expected output to contain 'hello world', got %q", out)
	}
}

// TestExecuteLocalCommandEnvContext 验证批次钩子的环境变量注入：子进程可读到 extraEnv 变量
func TestExecuteLocalCommandEnvContext(t *testing.T) {
	buf := captureDeployerLog(t)

	log := logger.NewServiceLogger("test-svc", 0)
	extraEnv := []string{"DEPLOY_HOOK_PROBE=probe-value-ok"}
	var cmd string
	if runtime.GOOS == "windows" {
		cmd = "echo %DEPLOY_HOOK_PROBE%"
	} else {
		cmd = "echo $DEPLOY_HOOK_PROBE"
	}
	if err := ExecuteLocalCommandEnvContext(context.Background(), cmd, log, extraEnv); err != nil {
		t.Fatalf("ExecuteLocalCommandEnvContext failed: %v", err)
	}

	if !strings.Contains(buf.String(), "probe-value-ok") {
		t.Errorf("expected child process to see injected env, got %q", buf.String())
	}
}

// captureDeployerLog 临时接管全局日志回调收集输出（与 logger 包的 OnLog 契约一致）
func captureDeployerLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := logger.OnLog
	logger.OnLog = func(line string) { buf.WriteString(line + "\n") }
	t.Cleanup(func() { logger.OnLog = prev })
	return buf
}

// TestDecodeConsoleBytes 验证 GBK 输出（中文 Windows 控制台默认编码）能正确转为 UTF-8
func TestDecodeConsoleBytes(t *testing.T) {
	// 模拟中文 Windows cmd.exe 输出的 GBK 字节
	gbkBytes, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("部署完成，服务重启成功"))
	if err != nil {
		t.Fatalf("GBK encode failed: %v", err)
	}
	if got := decodeConsoleBytes(gbkBytes); got != "部署完成，服务重启成功" {
		t.Errorf("GBK decode failed, got %q", got)
	}

	// UTF-8 中文直接放行
	if got := decodeConsoleBytes([]byte("中文UTF-8输出")); got != "中文UTF-8输出" {
		t.Errorf("UTF-8 passthrough failed, got %q", got)
	}

	// 纯 ASCII 直接放行
	if got := decodeConsoleBytes([]byte("plain ascii 123")); got != "plain ascii 123" {
		t.Errorf("ASCII passthrough failed, got %q", got)
	}
}

// TestExecuteLocalCommandChineseOutput 端到端验证本地执行中文命令输出不乱码
func TestExecuteLocalCommandChineseOutput(t *testing.T) {
	buf := captureDeployerLog(t)

	log := logger.NewServiceLogger("test-svc", 0)
	if err := ExecuteLocalCommandContext(context.Background(), "echo 中文部署测试-部署完成", log); err != nil {
		t.Fatalf("ExecuteLocalCommandContext failed: %v", err)
	}

	if !strings.Contains(buf.String(), "中文部署测试-部署完成") {
		t.Errorf("expected proper Chinese output, got %q", buf.String())
	}
}

// TestExecuteLocalCommandContextCancellation 验证 Context 取消可中断执行
func TestExecuteLocalCommandContextCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	log := logger.NewServiceLogger("test-svc", 0)
	var err error
	if runtime.GOOS == "windows" {
		err = ExecuteLocalCommandContext(ctx, "powershell -Command Start-Sleep -Seconds 5", log)
	} else {
		err = ExecuteLocalCommandContext(ctx, "sleep 5", log)
	}

	if err == nil {
		t.Fatalf("expected command to be canceled, but succeeded")
	}
	if !strings.Contains(err.Error(), "canceled") {
		t.Errorf("expected cancellation error, got: %v", err)
	}
}

func TestTestSSHConnectivityFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// 测试连接到不可达的非法地址，应返回错误而不能 panic
	cfg := config.ServerConfig{
		Host:           "127.0.0.1",
		Port:           65432, // 不存在的端口
		Username:       "invalid",
		Password:       "invalid",
		ConnectTimeout: 1,
	}

	err := TestSSHConnectivity(ctx, cfg, nil)
	if err == nil {
		t.Fatalf("expected error connecting to non-existent SSH server, got nil")
	}
}
