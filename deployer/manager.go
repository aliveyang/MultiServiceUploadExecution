package deployer

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"multi-service-deploy/config"
	"multi-service-deploy/logger"
)

// DeployOptions 部署运行选项
type DeployOptions struct {
	Parallel       *bool
	TargetServices []string
	MaxWorkers     int // 最大并发 Worker 数量（<=0 时默认 10）
}

// DeployManager 多服务部署管理器
type DeployManager struct {
	cfg     *config.DeployConfig
	options DeployOptions
}

// NewDeployManager 创建部署管理器
func NewDeployManager(cfg *config.DeployConfig, opts DeployOptions) *DeployManager {
	return &DeployManager{
		cfg:     cfg,
		options: opts,
	}
}

// Run 启动多服务部署（向后兼容）
func (m *DeployManager) Run() (bool, error) {
	return m.RunWithContext(context.Background())
}

// RunWithContext 启动多服务部署，支持全局批次钩子、Context 取消与 Worker Pool 信号量流控
func (m *DeployManager) RunWithContext(ctx context.Context) (bool, error) {
	services := m.filterServices()
	if len(services) == 0 {
		return false, fmt.Errorf("no matching services found to deploy")
	}

	totalStart := time.Now()

	// 1. 执行全局批次前置钩子 (PreDeploy，仅本地执行一次)
	if len(m.cfg.Hooks.PreDeploy) > 0 {
		logger.System(">>> Running batch pre-deploy hooks (%d command(s))...", len(m.cfg.Hooks.PreDeploy))
		batchLogger := logger.NewServiceLogger("batch-pre", -1)
		for i, cmd := range m.cfg.Hooks.PreDeploy {
			if strings.TrimSpace(cmd) == "" {
				continue
			}
			if err := ctx.Err(); err != nil {
				return false, fmt.Errorf("pre-deploy hook canceled: %w", err)
			}
			if err := ExecuteLocalCommandContext(ctx, cmd, batchLogger); err != nil {
				logger.Error("Global pre-deploy hook command #%d failed: %v", i+1, err)
				return false, fmt.Errorf("global pre-deploy hook failed: %w", err)
			}
		}
		logger.Success("Global pre-deploy hooks completed successfully.")
	}

	// 确定是否并发执行
	parallel := m.cfg.IsParallel()
	if m.options.Parallel != nil {
		parallel = *m.options.Parallel
	}

	maxWorkers := m.options.MaxWorkers
	if maxWorkers <= 0 {
		maxWorkers = 10
	}

	logger.System("Selected %d service(s) to deploy (Parallel Mode: %t, Max Workers: %d)", len(services), parallel, maxWorkers)

	results := make([]ServiceResult, len(services))

	if parallel {
		var wg sync.WaitGroup
		sem := make(chan struct{}, maxWorkers)

		for i, svc := range services {
			wg.Add(1)
			go func(idx int, s config.ServiceConfig) {
				defer wg.Done()
				select {
				case <-ctx.Done():
					results[idx] = ServiceResult{
						ServiceName: s.Name,
						Host:        fmt.Sprintf("%s:%d", s.Server.Host, s.Server.Port),
						Success:     false,
						Error:       ctx.Err(),
					}
					return
				case sem <- struct{}{}:
				}
				defer func() { <-sem }()

				results[idx] = RunServicePipelineContext(ctx, s, idx)
			}(i, svc)
		}
		wg.Wait()
	} else {
		// 串行依次执行
		for i, svc := range services {
			if err := ctx.Err(); err != nil {
				results[i] = ServiceResult{
					ServiceName: svc.Name,
					Host:        fmt.Sprintf("%s:%d", svc.Server.Host, svc.Server.Port),
					Success:     false,
					Error:       err,
				}
				continue
			}
			results[i] = RunServicePipelineContext(ctx, svc, i)
		}
	}

	totalDuration := time.Since(totalStart)
	allSuccess := PrintSummary(results, totalDuration)

	// 2. 当且仅当所有节点全部成功时，执行全局批次后置钩子 (PostDeploy，仅本地执行一次)
	if allSuccess && len(m.cfg.Hooks.PostDeploy) > 0 {
		logger.System("\n>>> Running batch post-deploy hooks (%d command(s))...", len(m.cfg.Hooks.PostDeploy))
		batchLogger := logger.NewServiceLogger("batch-post", -1)
		for i, cmd := range m.cfg.Hooks.PostDeploy {
			if strings.TrimSpace(cmd) == "" {
				continue
			}
			if err := ctx.Err(); err != nil {
				logger.Error("Post-deploy hook canceled: %v", err)
				allSuccess = false
				break
			}
			if err := ExecuteLocalCommandContext(ctx, cmd, batchLogger); err != nil {
				logger.Error("Global post-deploy hook command #%d failed: %v", i+1, err)
				allSuccess = false
				break
			}
		}
		if allSuccess {
			logger.Success("Global post-deploy hooks completed successfully.")
		}
	}

	return allSuccess, nil
}

// filterServices 筛选启用的与目标指定的服务
func (m *DeployManager) filterServices() []config.ServiceConfig {
	targets := make(map[string]bool)
	for _, t := range m.options.TargetServices {
		t = strings.TrimSpace(t)
		if t != "" {
			targets[strings.ToLower(t)] = true
		}
	}

	filtered := make([]config.ServiceConfig, 0)
	for _, svc := range m.cfg.Services {
		if !svc.IsEnabled() {
			continue
		}
		if len(targets) > 0 && !targets[strings.ToLower(svc.Name)] {
			continue
		}
		filtered = append(filtered, svc)
	}

	return filtered
}

// PrintSummary 格式化输出部署结果报告
func PrintSummary(results []ServiceResult, totalDuration time.Duration) bool {
	logger.System("\n============================= DEPLOYMENT SUMMARY =============================")
	fmt.Printf("%-3s %-18s %-22s %-10s %-10s %s\n", "#", "SERVICE", "TARGET", "STATUS", "DURATION", "DETAILS")
	fmt.Println(strings.Repeat("-", 78))

	successCount := 0
	failedCount := 0

	for i, r := range results {
		statusStr := fmt.Sprintf("%sSUCCESS%s", logger.ColorGreen, logger.ColorReset)
		detailStr := "-"
		if r.Stats != nil && r.Stats.TotalFiles > 0 {
			detailStr = fmt.Sprintf("%d files (%s)", r.Stats.TotalFiles, formatBytes(r.Stats.TotalBytes))
		}

		if !r.Success {
			failedCount++
			statusStr = fmt.Sprintf("%sFAILED%s", logger.ColorRed, logger.ColorReset)
			if r.Error != nil {
				detailStr = r.Error.Error()
			}
		} else {
			successCount++
		}

		fmt.Printf("%-3d %-18s %-22s %-10s %-10s %s\n",
			i+1,
			r.ServiceName,
			r.Host,
			statusStr,
			r.Duration.Round(time.Millisecond).String(),
			detailStr,
		)
	}

	fmt.Println(strings.Repeat("=", 78))
	summaryLine := fmt.Sprintf("Total: %d | Successful: %d | Failed: %d | Total Time: %v",
		len(results), successCount, failedCount, totalDuration.Round(time.Millisecond))

	if failedCount == 0 {
		logger.Success("%s\n", summaryLine)
		return true
	}

	logger.Error("%s\n", summaryLine)
	return false
}
