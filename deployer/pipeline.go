package deployer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"multi-service-deploy/config"
	"multi-service-deploy/logger"
)

// ServiceResult 单个服务单元的执行结果
type ServiceResult struct {
	ServiceName string
	Tags        []string
	Type        string
	Stage       int
	Host        string
	Success     bool
	Error       error
	Duration    time.Duration
	Stats       *UploadStats
}

// RunServicePipelineContext 执行单个服务的完整部署流水线，支持 Context 及时取消
func RunServicePipelineContext(ctx context.Context, svc config.ServiceConfig, index int) (result ServiceResult) {
	startTime := time.Now()
	log := logger.NewServiceLogger(svc.Name, index)

	result = ServiceResult{
		ServiceName: svc.Name,
		Tags:        svc.Tags,
		Type:        typeLabelOf(svc),
		Stage:       svc.Stage,
		Host:        fmt.Sprintf("%s:%d", svc.Server.Host, svc.Server.Port),
	}
	defer func() { result.Duration = time.Since(startTime) }()

	log.Info("Starting deployment pipeline...")

	// 检查 Context 取消
	if err := ctx.Err(); err != nil {
		return aborted(result, log, fmt.Errorf("deployment canceled: %w", err))
	}

	// Phase 1: 上传前本地执行命令 (PreUploadLocal)
	if !svc.Steps.IsStepEnabled(config.StepPreUploadLocal) {
		log.Info(">>> Phase 1/5: Step 'preUploadLocal' is disabled, skipping local pre-upload commands.")
	} else if err := runHooks(ctx, log, "Phase 1/5: pre-upload local commands", svc.Hooks.PreUploadLocal, func(c string) error {
		return ExecuteLocalCommandContext(ctx, c, log)
	}); err != nil {
		return aborted(result, log, err)
	}

	if err := ctx.Err(); err != nil {
		return aborted(result, log, fmt.Errorf("deployment canceled: %w", err))
	}

	// Phase 2: 建立 SSH 连接
	log.Info(">>> Connecting to remote server...")
	sshClient, err := NewSSHClientContext(ctx, svc.Server, log)
	if err != nil {
		return aborted(result, log, fmt.Errorf("SSH connection failed: %w", err))
	}
	defer sshClient.Close()

	if err := ctx.Err(); err != nil {
		return aborted(result, log, fmt.Errorf("deployment canceled: %w", err))
	}

	// Phase 3: 上传前远端执行命令 (PreUploadRemote)
	if !svc.Steps.IsStepEnabled(config.StepPreUploadRemote) {
		log.Info(">>> Phase 2/5: Step 'preUploadRemote' is disabled, skipping remote pre-upload commands.")
	} else if err := runHooks(ctx, log, "Phase 2/5: pre-upload remote commands", svc.Hooks.PreUploadRemote, func(c string) error {
		return sshClient.ExecuteRemoteCommandContext(ctx, c)
	}); err != nil {
		return aborted(result, log, err)
	}

	if err := ctx.Err(); err != nil {
		return aborted(result, log, fmt.Errorf("deployment canceled: %w", err))
	}

	// Phase 4: 文件传输 (SFTP Upload)
	if !svc.Steps.IsStepEnabled(config.StepUpload) {
		log.Info(">>> Phase 3/5: Step 'upload' is disabled, skipping file transfer.")
	} else if strings.TrimSpace(svc.Upload.LocalPath) != "" && strings.TrimSpace(svc.Upload.RemotePath) != "" {
		log.Info(">>> Phase 3/5: Transferring files via SFTP...")
		uploader, err := NewSFTPUploader(sshClient, log)
		if err != nil {
			return aborted(result, log, fmt.Errorf("SFTP init failed: %w", err))
		}
		defer uploader.Close()

		stats, err := uploader.Upload(svc.Upload)
		if err != nil {
			return aborted(result, log, fmt.Errorf("SFTP upload failed: %w", err))
		}
		result.Stats = stats
	} else {
		log.Info(">>> Phase 3/5: No upload paths configured, skipping file transfer.")
	}

	if err := ctx.Err(); err != nil {
		return aborted(result, log, fmt.Errorf("deployment canceled: %w", err))
	}

	// Phase 5: 上传后远端执行命令 (PostUploadRemote)
	if !svc.Steps.IsStepEnabled(config.StepPostUploadRemote) {
		log.Info(">>> Phase 4/5: Step 'postUploadRemote' is disabled, skipping remote post-upload commands.")
	} else if err := runHooks(ctx, log, "Phase 4/5: post-upload remote commands", svc.Hooks.PostUploadRemote, func(c string) error {
		return sshClient.ExecuteRemoteCommandContext(ctx, c)
	}); err != nil {
		return aborted(result, log, err)
	}

	if err := ctx.Err(); err != nil {
		return aborted(result, log, fmt.Errorf("deployment canceled: %w", err))
	}

	// Phase 6: 上传后本地执行命令 (PostUploadLocal)
	if !svc.Steps.IsStepEnabled(config.StepPostUploadLocal) {
		log.Info(">>> Phase 5/5: Step 'postUploadLocal' is disabled, skipping local post-upload commands.")
	} else if err := runHooks(ctx, log, "Phase 5/5: post-upload local commands", svc.Hooks.PostUploadLocal, func(c string) error {
		return ExecuteLocalCommandContext(ctx, c, log)
	}); err != nil {
		return aborted(result, log, err)
	}

	result.Success = true
	log.Success("Deployment successfully finished in %v", time.Since(startTime).Round(time.Millisecond))
	return result
}

// runHooks 按顺序执行一组命令：空列表跳过，空命令行跳过，任一失败即中止，支持 context 检测
func runHooks(ctx context.Context, log *logger.ServiceLogger, phase string, cmds []string, run func(string) error) error {
	if len(cmds) == 0 {
		return nil
	}
	log.Info(">>> %s (%d commands)", phase, len(cmds))
	for i, cmd := range cmds {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%s: canceled: %w", phase, err)
		}
		if strings.TrimSpace(cmd) == "" {
			continue
		}
		if err := run(cmd); err != nil {
			return fmt.Errorf("%s: command #%d failed: %w", phase, i+1, err)
		}
	}
	log.Success("%s completed.", phase)
	return nil
}

// aborted 记录中止原因并返回失败结果
func aborted(result ServiceResult, log *logger.ServiceLogger, err error) ServiceResult {
	result.Error = err
	log.Error("Deployment aborted: %v", err)
	return result
}

// typeLabelOf 服务的展示类型：优先旧版显式 type 字段（未迁移的内存配置），
// 否则根据步骤开关推导等价标签（standard / exec_only / sync_only / custom）
func typeLabelOf(svc config.ServiceConfig) string {
	if t := strings.ToLower(strings.TrimSpace(svc.Type)); t != "" {
		return t
	}
	return svc.Steps.TypeLabel()
}
