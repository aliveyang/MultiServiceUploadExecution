package deployer

import (
	"context"
	"fmt"
	"sort"
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
	TargetGroups   []string // 过滤目标分组，如 "frontend", "backend"
	TargetTypes    []string // 过滤目标类型，如 "standard", "exec_only", "sync_only"
	Scenario       string   // 指定场景预设名称，如 "prod", "test"
	MaxWorkers     int      // 最大并发 Worker 数量（<=0 时默认 10）
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

// RunWithContext 启动多服务部署，支持多场景、多任务、多分组分阶段调度编排、Context 取消与 Worker Pool 限流
func (m *DeployManager) RunWithContext(ctx context.Context) (bool, error) {
	services, err := m.filterServices()
	if err != nil {
		return false, err
	}
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

	// 确定场景与并发策略
	var scenario *config.ScenarioConfig
	if strings.TrimSpace(m.options.Scenario) != "" {
		scenario = m.cfg.FindScenario(m.options.Scenario)
	}

	parallel := m.cfg.IsParallel()
	if scenario != nil && scenario.Parallel != nil {
		parallel = *scenario.Parallel
	}
	if m.options.Parallel != nil {
		parallel = *m.options.Parallel
	}

	maxWorkers := m.options.MaxWorkers
	if maxWorkers <= 0 && scenario != nil && scenario.MaxWorkers > 0 {
		maxWorkers = scenario.MaxWorkers
	}
	if maxWorkers <= 0 {
		maxWorkers = 10
	}

	// 按 Stage 将筛选出的服务分波次归类并升序排列
	stageMap := make(map[int][]config.ServiceConfig)
	var stageNums []int
	for _, svc := range services {
		stage := svc.Stage
		if stage <= 0 {
			stage = 1
		}
		if _, exists := stageMap[stage]; !exists {
			stageNums = append(stageNums, stage)
		}
		stageMap[stage] = append(stageMap[stage], svc)
	}
	sort.Ints(stageNums)

	scenarioDesc := "none"
	if scenario != nil {
		scenarioDesc = scenario.Name
	}
	logger.System("Deployment Plan: %d service(s) across %d stage(s) [Scenario: %s, Parallel: %t, Max Workers: %d]",
		len(services), len(stageNums), scenarioDesc, parallel, maxWorkers)

	// 分组生命周期状态跟踪
	groupTotalServices := make(map[string]int)
	for _, svc := range services {
		grp := strings.ToLower(svc.Group)
		if grp == "" {
			grp = config.DefaultGroup
		}
		groupTotalServices[grp]++
	}
	groupCompletedSuccess := make(map[string]int)
	groupPreDeployExecuted := make(map[string]bool)
	groupPostDeployExecuted := make(map[string]bool)

	var allResults []ServiceResult
	globalServiceIdx := 0
	abortedDueToFailure := false

	// 按 Stage 升序串行依次执行每个阶段
	for _, stageNum := range stageNums {
		stageServices := stageMap[stageNum]

		// 若前置阶段已失败，触发流水线熔断保护，阻断后续所有阶段
		if abortedDueToFailure {
			for _, svc := range stageServices {
				allResults = append(allResults, ServiceResult{
					ServiceName: svc.Name,
					Group:       svc.Group,
					Type:        svc.Type,
					Stage:       stageNum,
					Host:        fmt.Sprintf("%s:%d", svc.Server.Host, svc.Server.Port),
					Success:     false,
					Error:       fmt.Errorf("skipped: preceding stage failed (pipeline circuit-breaker triggered)"),
				})
			}
			continue
		}

		if err := ctx.Err(); err != nil {
			for _, svc := range stageServices {
				allResults = append(allResults, ServiceResult{
					ServiceName: svc.Name,
					Group:       svc.Group,
					Type:        svc.Type,
					Stage:       stageNum,
					Host:        fmt.Sprintf("%s:%d", svc.Server.Host, svc.Server.Port),
					Success:     false,
					Error:       err,
				})
			}
			continue
		}

		// 执行当前 Stage 中所涉分组的专属批次前置钩子 (Group PreDeploy)
		for _, svc := range stageServices {
			grp := strings.ToLower(svc.Group)
			if grp == "" {
				grp = config.DefaultGroup
			}
			if !groupPreDeployExecuted[grp] {
				groupPreDeployExecuted[grp] = true
				groupCfg := m.cfg.FindGroup(grp)
				if groupCfg != nil && len(groupCfg.Hooks.PreDeploy) > 0 {
					logger.System(">>> Running group %q pre-deploy hooks (%d command(s))...", grp, len(groupCfg.Hooks.PreDeploy))
					grpLogger := logger.NewServiceLogger(grp+"-pre", -1)
					for i, cmd := range groupCfg.Hooks.PreDeploy {
						if strings.TrimSpace(cmd) == "" {
							continue
						}
						if err := ctx.Err(); err != nil {
							return false, fmt.Errorf("group %q pre-deploy hook canceled: %w", grp, err)
						}
						if err := ExecuteLocalCommandContext(ctx, cmd, grpLogger); err != nil {
							logger.Error("Group %q pre-deploy hook command #%d failed: %v", grp, i+1, err)
							return false, fmt.Errorf("group %q pre-deploy hook failed: %w", grp, err)
						}
					}
					logger.Success("Group %q pre-deploy hooks completed successfully.", grp)
				}
			}
		}

		logger.System("\n>>> [Stage %d] Starting deployment of %d service(s)...", stageNum, len(stageServices))
		stageResults := make([]ServiceResult, len(stageServices))

		if parallel {
			var wg sync.WaitGroup
			sem := make(chan struct{}, maxWorkers)

			for i, svc := range stageServices {
				wg.Add(1)
				curIdx := globalServiceIdx + i
				go func(idx int, logIdx int, s config.ServiceConfig) {
					defer wg.Done()
					select {
					case <-ctx.Done():
						stageResults[idx] = ServiceResult{
							ServiceName: s.Name,
							Group:       s.Group,
							Type:        s.Type,
							Stage:       stageNum,
							Host:        fmt.Sprintf("%s:%d", s.Server.Host, s.Server.Port),
							Success:     false,
							Error:       ctx.Err(),
						}
						return
					case sem <- struct{}{}:
					}
					defer func() { <-sem }()

					stageResults[idx] = RunServicePipelineContext(ctx, s, logIdx)
				}(i, curIdx, svc)
			}
			wg.Wait()
		} else {
			// 串行依次执行
			for i, svc := range stageServices {
				curIdx := globalServiceIdx + i
				if err := ctx.Err(); err != nil {
					stageResults[i] = ServiceResult{
						ServiceName: svc.Name,
						Group:       svc.Group,
						Type:        svc.Type,
						Stage:       stageNum,
						Host:        fmt.Sprintf("%s:%d", svc.Server.Host, svc.Server.Port),
						Success:     false,
						Error:       err,
					}
					continue
				}
				stageResults[i] = RunServicePipelineContext(ctx, svc, curIdx)
			}
		}

		globalServiceIdx += len(stageServices)
		allResults = append(allResults, stageResults...)

		// 检查当前 Stage 是否有任何服务失败，并累加分组成功计数
		for _, r := range stageResults {
			grp := strings.ToLower(r.Group)
			if grp == "" {
				grp = config.DefaultGroup
			}
			if !r.Success {
				abortedDueToFailure = true
				logger.Error("Stage %d deployment failed on service %q. Aborting subsequent stages!", stageNum, r.ServiceName)
			} else {
				groupCompletedSuccess[grp]++
			}
		}

		// 检查当前 Stage 结束后是否有分组已全部成功完成，触发对应 Group PostDeploy
		for grp, total := range groupTotalServices {
			if !groupPostDeployExecuted[grp] && groupCompletedSuccess[grp] == total {
				groupPostDeployExecuted[grp] = true
				groupCfg := m.cfg.FindGroup(grp)
				if groupCfg != nil && len(groupCfg.Hooks.PostDeploy) > 0 {
					logger.System("\n>>> Running group %q post-deploy hooks (%d command(s))...", grp, len(groupCfg.Hooks.PostDeploy))
					grpLogger := logger.NewServiceLogger(grp+"-post", -1)
					for i, cmd := range groupCfg.Hooks.PostDeploy {
						if strings.TrimSpace(cmd) == "" {
							continue
						}
						if err := ctx.Err(); err != nil {
							logger.Error("Group %q post-deploy hook canceled: %v", grp, err)
							break
						}
						if err := ExecuteLocalCommandContext(ctx, cmd, grpLogger); err != nil {
							logger.Error("Group %q post-deploy hook command #%d failed: %v", grp, i+1, err)
							break
						}
					}
					logger.Success("Group %q post-deploy hooks completed successfully.", grp)
				}
			}
		}
	}

	totalDuration := time.Since(totalStart)
	allSuccess := PrintSummary(allResults, totalDuration)

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

// filterServices 筛选启用的与目标指定的服务（支持多场景、多分组、多类型与服务名多重过滤）
func (m *DeployManager) filterServices() ([]config.ServiceConfig, error) {
	var scenario *config.ScenarioConfig
	if strings.TrimSpace(m.options.Scenario) != "" {
		scenario = m.cfg.FindScenario(m.options.Scenario)
		if scenario == nil {
			return nil, fmt.Errorf("scenario %q not found in configuration", m.options.Scenario)
		}
	}

	// 命令行与调用方指定的过滤列表
	targetServices := toLowerSet(m.options.TargetServices)
	targetGroups := toLowerSet(m.options.TargetGroups)
	targetTypes := toLowerSet(m.options.TargetTypes)

	// 场景定义的白名单限制
	var scServices, scGroups, scTypes map[string]bool
	if scenario != nil {
		scServices = toLowerSet(scenario.Services)
		scGroups = toLowerSet(scenario.Groups)
		scTypes = toLowerSet(scenario.Types)
	}

	filtered := make([]config.ServiceConfig, 0)
	for _, svc := range m.cfg.Services {
		if !svc.IsEnabled() {
			continue
		}

		sName := strings.ToLower(strings.TrimSpace(svc.Name))
		sGroup := strings.ToLower(strings.TrimSpace(svc.Group))
		if sGroup == "" {
			sGroup = config.DefaultGroup
		}
		sType := strings.ToLower(strings.TrimSpace(svc.Type))
		if sType == "" {
			sType = config.DeployTypeStandard
		}

		// 1. 场景约束过滤
		if scenario != nil {
			if len(scServices) > 0 && !scServices[sName] {
				continue
			}
			if len(scGroups) > 0 && !scGroups[sGroup] {
				continue
			}
			if len(scTypes) > 0 && !scTypes[sType] {
				continue
			}
		}

		// 2. 调用方参数过滤
		if len(targetServices) > 0 && !targetServices[sName] {
			continue
		}
		if len(targetGroups) > 0 && !targetGroups[sGroup] {
			continue
		}
		if len(targetTypes) > 0 && !targetTypes[sType] {
			continue
		}

		filtered = append(filtered, svc)
	}

	return filtered, nil
}

func toLowerSet(slice []string) map[string]bool {
	m := make(map[string]bool)
	for _, item := range slice {
		for _, part := range strings.Split(item, ",") {
			p := strings.ToLower(strings.TrimSpace(part))
			if p != "" {
				m[p] = true
			}
		}
	}
	return m
}

// PrintSummary 格式化输出部署结果报告，呈现服务、分组、类型、阶段及执行耗时
func PrintSummary(results []ServiceResult, totalDuration time.Duration) bool {
	logger.System("\n============================= DEPLOYMENT SUMMARY =============================")
	fmt.Printf("%-3s %-16s %-10s %-10s %-6s %-20s %-10s %-10s %s\n",
		"#", "SERVICE", "GROUP", "TYPE", "STAGE", "TARGET", "STATUS", "DURATION", "DETAILS")
	fmt.Println(strings.Repeat("-", 98))

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

		grp := r.Group
		if grp == "" {
			grp = config.DefaultGroup
		}
		typ := r.Type
		if typ == "" {
			typ = config.DeployTypeStandard
		}
		stg := r.Stage
		if stg <= 0 {
			stg = 1
		}

		fmt.Printf("%-3d %-16s %-10s %-10s %-6d %-20s %-10s %-10s %s\n",
			i+1,
			r.ServiceName,
			grp,
			typ,
			stg,
			r.Host,
			statusStr,
			r.Duration.Round(time.Millisecond).String(),
			detailStr,
		)
	}

	fmt.Println(strings.Repeat("=", 98))
	summaryLine := fmt.Sprintf("Total: %d | Successful: %d | Failed: %d | Total Time: %v",
		len(results), successCount, failedCount, totalDuration.Round(time.Millisecond))

	if failedCount == 0 {
		logger.Success("%s\n", summaryLine)
		return true
	}

	logger.Error("%s\n", summaryLine)
	return false
}
