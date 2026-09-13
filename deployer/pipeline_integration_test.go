package deployer

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"multi-service-deploy/config"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// generateTestRSAPrivateKey 生成测试用 RSA 私钥
func generateTestRSAPrivateKey() (ssh.Signer, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	return ssh.ParsePrivateKey(keyPEM)
}

// startMockSSHServer 启动本地轻量级测试 SSH 服务
func startMockSSHServer(t *testing.T, targetDir string) (int, func()) {
	signer, err := generateTestRSAPrivateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	sshConfig := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == "testuser" && string(pass) == "testpass" {
				return nil, nil
			}
			return nil, fmt.Errorf("password rejected")
		},
	}
	sshConfig.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port

	var wg sync.WaitGroup
	quit := make(chan struct{})

	go func() {
		for {
			tcpConn, err := listener.Accept()
			if err != nil {
				select {
				case <-quit:
					return
				default:
					continue
				}
			}

			wg.Add(1)
			go func(conn net.Conn) {
				defer wg.Done()
				defer conn.Close()

				sConn, chans, reqs, err := ssh.NewServerConn(conn, sshConfig)
				if err != nil {
					return
				}
				defer sConn.Close()
				go ssh.DiscardRequests(reqs)

				for newChannel := range chans {
					if newChannel.ChannelType() != "session" {
						newChannel.Reject(ssh.UnknownChannelType, "unsupported channel")
						continue
					}

					channel, requests, err := newChannel.Accept()
					if err != nil {
						continue
					}

					go handleSessionChannel(channel, requests, targetDir)
				}
			}(tcpConn)
		}
	}()

	cleanup := func() {
		close(quit)
		listener.Close()
		wg.Wait()
	}

	return port, cleanup
}

func handleSessionChannel(channel ssh.Channel, requests <-chan *ssh.Request, targetDir string) {
	defer channel.Close()

	for req := range requests {
		switch req.Type {
		case "subsystem":
			subsystem := string(req.Payload[4:])
			if subsystem == "sftp" {
				req.Reply(true, nil)
				server, err := sftp.NewServer(channel, sftp.WithServerWorkingDirectory(targetDir))
				if err == nil {
					_ = server.Serve()
					return
				}
			} else {
				req.Reply(false, nil)
			}
		case "exec":
			cmdLen := int(req.Payload[3])
			cmd := string(req.Payload[4 : 4+cmdLen])
			req.Reply(true, nil)

			fmt.Fprintf(channel, "mock remote output for: %s\n", cmd)

			var exitStatus = struct{ Status uint32 }{Status: 0}
			_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(&exitStatus))
			return
		default:
			req.Reply(false, nil)
		}
	}
}

func TestEndToEndPipeline(t *testing.T) {
	remoteBaseDir := t.TempDir()
	port, cleanup := startMockSSHServer(t, remoteBaseDir)
	defer cleanup()

	localDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(localDir, "app.txt"), []byte("deploy content 123"), 0644); err != nil {
		t.Fatalf("failed to write local file: %v", err)
	}

	svc := config.ServiceConfig{
		Name: "test-pipeline-svc",
		Server: config.ServerConfig{
			Host:           "127.0.0.1",
			Port:           port,
			Username:       "testuser",
			Password:       "testpass",
			ConnectTimeout: 5,
		},
		Upload: config.UploadConfig{
			LocalPath:  localDir,
			RemotePath: "remote-app",
		},
		Hooks: config.HooksConfig{
			PreUploadLocal:   config.CommandList{"echo pre-upload-local-run"},
			PreUploadRemote:  config.CommandList{"echo pre-upload-remote-run"},
			PostUploadRemote: config.CommandList{"echo post-upload-remote-run"},
			PostUploadLocal:  config.CommandList{"echo post-upload-local-run"},
		},
	}

	result := RunServicePipelineContext(context.Background(), svc, 0)
	if !result.Success {
		t.Fatalf("expected pipeline to succeed, got error: %v", result.Error)
	}

	if result.Stats == nil || result.Stats.TotalFiles == 0 {
		t.Fatalf("expected files to be uploaded, stats: %+v", result.Stats)
	}

	uploadedFile := filepath.Join(remoteBaseDir, "remote-app", "app.txt")
	content, err := os.ReadFile(uploadedFile)
	if err != nil {
		t.Fatalf("failed to read uploaded file on mock remote: %v", err)
	}

	if string(content) != "deploy content 123" {
		t.Fatalf("unexpected content in remote file: %q", string(content))
	}
}

func TestMultiServiceParallelDeployment(t *testing.T) {
	remoteDir1 := t.TempDir()
	port1, cleanup1 := startMockSSHServer(t, remoteDir1)
	defer cleanup1()

	remoteDir2 := t.TempDir()
	port2, cleanup2 := startMockSSHServer(t, remoteDir2)
	defer cleanup2()

	localDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(localDir, "file.txt"), []byte("hello cluster"), 0644)

	parallel := true
	cfg := &config.DeployConfig{
		Parallel: &parallel,
		Services: []config.ServiceConfig{
			{
				Name: "cluster-node-1",
				Server: config.ServerConfig{
					Host:     "127.0.0.1",
					Port:     port1,
					Username: "testuser",
					Password: "testpass",
				},
				Upload: config.UploadConfig{
					LocalPath:  localDir,
					RemotePath: "dest-node-1",
				},
				Hooks: config.HooksConfig{
					PreUploadLocal:   config.CommandList{"echo node-1-local-pre"},
					PostUploadRemote: config.CommandList{"echo node-1-remote-post"},
				},
			},
			{
				Name: "cluster-node-2",
				Server: config.ServerConfig{
					Host:     "127.0.0.1",
					Port:     port2,
					Username: "testuser",
					Password: "testpass",
				},
				Upload: config.UploadConfig{
					LocalPath:  localDir,
					RemotePath: "dest-node-2",
				},
				Hooks: config.HooksConfig{
					PreUploadRemote: config.CommandList{"echo node-2-remote-pre"},
					PostUploadLocal: config.CommandList{"echo node-2-local-post"},
				},
			},
		},
	}

	mgr := NewDeployManager(cfg, DeployOptions{})
	start := time.Now()
	allSuccess, err := mgr.RunWithContext(context.Background())
	if err != nil {
		t.Fatalf("manager Run failed: %v", err)
	}
	if !allSuccess {
		t.Fatalf("expected all services to deploy successfully")
	}

	t.Logf("Parallel deploy completed in %v", time.Since(start))

	// 验证两个节点都成功收到了文件
	if _, err := os.Stat(filepath.Join(remoteDir1, "dest-node-1", "file.txt")); err != nil {
		t.Errorf("node 1 destination file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(remoteDir2, "dest-node-2", "file.txt")); err != nil {
		t.Errorf("node 2 destination file missing: %v", err)
	}
}

// TestTagBatchHooksEndToEnd 验证标签批次钩子端到端语义：
// pre 钩子在批次开始前执行一次；post 钩子在该标签全部节点成功后执行一次；
// 多标签服务（backend+data）同时计入两个标签的完成计数。
func TestTagBatchHooksEndToEnd(t *testing.T) {
	remoteDir := t.TempDir()
	port, cleanup := startMockSSHServer(t, remoteDir)
	defer cleanup()

	preMarker := filepath.Join(t.TempDir(), "pre-marker.txt")
	postMarker := filepath.Join(t.TempDir(), "post-marker.txt")
	dataPostMarker := filepath.Join(t.TempDir(), "data-post-marker.txt")

	localDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(localDir, "app.txt"), []byte("tag hooks e2e"), 0644)

	cfg := &config.DeployConfig{
		TagHooks: []config.TagHookConfig{
			{
				Name: "backend",
				Hooks: config.BatchHooks{
					PreDeploy:  config.CommandList{"type nul > " + preMarker},
					PostDeploy: config.CommandList{"type nul > " + postMarker},
				},
			},
			{
				Name: "data",
				Hooks: config.BatchHooks{
					PostDeploy: config.CommandList{"type nul > " + dataPostMarker},
				},
			},
		},
		Services: []config.ServiceConfig{
			{
				Name: "tagged-node-1",
				Tags: []string{"backend", "data"}, // 多标签：同时计入 backend 与 data 的计数
				Server: config.ServerConfig{
					Host:     "127.0.0.1",
					Port:     port,
					Username: "testuser",
					Password: "testpass",
				},
				Upload: config.UploadConfig{
					LocalPath:  localDir,
					RemotePath: "dest-tagged-1",
				},
			},
			{
				Name: "tagged-node-2",
				Tags: []string{"backend"},
				Server: config.ServerConfig{
					Host:     "127.0.0.1",
					Port:     port,
					Username: "testuser",
					Password: "testpass",
				},
				Upload: config.UploadConfig{
					LocalPath:  localDir,
					RemotePath: "dest-tagged-2",
				},
			},
		},
	}

	mgr := NewDeployManager(cfg, DeployOptions{TargetTags: []string{"backend"}})
	allSuccess, err := mgr.RunWithContext(context.Background())
	if err != nil {
		t.Fatalf("manager Run failed: %v", err)
	}
	if !allSuccess {
		t.Fatalf("expected all tagged services to deploy successfully")
	}

	for _, marker := range []string{preMarker, postMarker, dataPostMarker} {
		if _, err := os.Stat(marker); err != nil {
			t.Errorf("expected tag hook marker file %s to exist: %v", marker, err)
		}
	}
}
