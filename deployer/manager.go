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
	TargetServices []string // 过滤目标服务名，如 "api-server-01"
	TargetTags     []string // 过滤目标标签（并集语义：服务携带任一选中标签即命中），如 "backend", "data"
	TargetTypes    []string // 过滤目标类型，如 "standard", "exec_only", "sync_only"
	MaxWorkers     int      // 最大并发 Worker 数量（<=0 时默认 10）
	ConfigPath     string   // 配置文件路径（注入批次钩子 $CONFIG 变量；可空）

	// 批次事件注入点（仅 Web 层使用；OnEvent 为 nil 时零开销，CLI 行为不变）
	Workspace string                          // 信息性字段：随批次事件透传，用于历史归属
	BatchID   string                          // 批次 ID；为空时由管理器自动生成
	OnEvent   func(event string, payload any) // 批次生命周期结构化事件回调
}

// DeployManager 多服务部署管理器
type DeployManager struct {
	cfg        *config.DeployConfig
	options    DeployOptions
	batchID    string
	batchStart time.Time
}

// emit 向已注册的回调发射批次生命周期事件（未注册时零开销）
func (m *DeployManager) emit(name string, payload any) {
	if m.options.OnEvent != nil {
		m.options.OnEvent(name, payload)
	}
}

// runOne 执行单服务流水线并发射服务级开始/结束事件
func (m *DeployManager) runOne(ctx context.Context, svc config.ServiceConfig, idx int) ServiceResult {
	m.emit(EventServiceStarted, ServiceNodeInput{
		Name:  svc.Name,
		Tags:  svc.Tags,
		Type:  typeLabelOf(svc),
		Stage: svc.Stage,
		Host:  fmt.Sprintf("%s:%d", svc.Server.Host, svc.Server.Port),
	})
	res := RunServicePipelineContext(ctx, svc, idx)
	m.emit(EventServiceFinished, outcomeOf(res))
	return res
}

// NewDeployManager 创建部署管理器
func NewDeployManager(cfg *config.DeployConfig, opts DeployOptions) *DeployManager {
	return &DeployManager{
		cfg:     cfg,
		options: opts,
	}
}

// batchHookEnv 构造批次钩子的注入环境变量：SPACE/TAGS/BATCH_ID/CONFIG/NODE_TOTAL/
// NODE_SUCCESS/NODE_FAILED/DURATION（POSIX shell 以 $SPACE 引用，Windows cmd 以 %SPACE% 引用）。
// 变量恒全量定义：结果类变量批次开始前为 0；字符串变量不允许空值——Windows 环境变量为空等同未定义，
// cmd 会将 %SPACE% 原样保留，故空 workspace 回退 "default"、空 tags 回退 "none"。
func batchHookEnv(workspace, tags, batchID, configPath string, nodeTotal, nodeSuccess, nodeFailed, durationMs int64) []string {
	if strings.TrimSpace(workspace) == "" {
		workspace = config.DefaultWorkspaceID
	}
	if strings.TrimSpace(tags) == "" {
		tags = "none"
	}
	return []string{
		"SPACE=" + workspace,
		"TAGS=" + tags,
		"BATCH_ID=" + batchID,
		"CONFIG=" + configPath,
		fmt.Sprintf("NODE_TOTAL=%d", nodeTotal),
		fmt.Sprintf("NODE_SUCCESS=%d", nodeSuccess),
		fmt.Sprintf("NODE_FAILED=%d", nodeFailed),
		fmt.Sprintf("DURATION=%d", durationMs),
	}
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
	m.batchStart = totalStart
	m.batchID = m.options.BatchID
	if m.batchID == "" {
		m.batchID = NewBatchID()
	}

	// 宣告批次开始：携带全部计划节点，供前端渲染节点状态与进度分母
	plan := make([]ServiceNodeInput, 0, len(services))
	stageSet := make(map[int]bool)
	for _, svc := range services {
		stage := svc.Stage
		if stage <= 0 {
			stage = 1
		}
		stageSet[stage] = true
		plan = append(plan, ServiceNodeInput{
			Name:  svc.Name,
			Tags:  svc.Tags,
			Type:  typeLabelOf(svc),
			Stage: stage,
			Host:  fmt.Sprintf("%s:%d", svc.Server.Host, svc.Server.Port),
		})
	}
	stages := make([]int, 0, len(stageSet))
	for st := range stageSet {
		stages = append(stages, st)
	}
	sort.Ints(stages)
	m.emit(EventBatchStarted, BatchStartedPayload{
		ID:         m.batchID,
		Workspace:  m.options.Workspace,
		Tags:       normalizeTagList(m.options.TargetTags),
		Parallel:   m.cfg.IsParallel(),
		MaxWorkers: m.options.MaxWorkers,
		Total:      len(services),
		Stages:     stages,
		Services:   plan,
	})

	var allResults []ServiceResult

	// 目标标签描述（供钩子环境变量与部署计划日志使用；空筛选回退 "none"）
	tagsDesc := strings.Join(normalizeTagList(m.options.TargetTags), ",")
	if tagsDesc == "" {
		tagsDesc = "none"
	}
	hookEnvBase := func(nodeSuccess, nodeFailed, durationMs int64) []string {
		return batchHookEnv(m.options.Workspace, tagsDesc, m.batchID, m.options.ConfigPath,
			int64(len(services)), nodeSuccess, nodeFailed, durationMs)
	}
	preEnv := hookEnvBase(0, 0, 0)

	// 1. 执行全局批次前置钩子 (PreDeploy，仅本地执行一次)
	if len(m.cfg.Hooks.PreDeploy) > 0 {
		logger.System(">>> Running batch pre-deploy hooks (%d command(s))...", len(m.cfg.Hooks.PreDeploy))
		batchLogger := logger.NewServiceLogger("batch-pre", -1)
		for i, cmd := range m.cfg.Hooks.PreDeploy {
			if strings.TrimSpace(cmd) == "" {
				continue
			}
			if err := ctx.Err(); err != nil {
				m.emitBatchFinished(allResults, false, ctx)
				return false, fmt.Errorf("pre-deploy hook canceled: %w", err)
			}
			if err := ExecuteLocalCommandEnvContext(ctx, cmd, batchLogger, preEnv); err != nil {
				logger.Error("Global pre-deploy hook command #%d failed: %v", i+1, err)
				m.emitBatchFinished(allResults, false, ctx)
				return false, fmt.Errorf("global pre-deploy hook failed: %w", err)
			}
		}
		logger.Success("Global pre-deploy hooks completed successfully.")
	}

	// 确定并发策略（调用方显式指定优先）
	parallel := m.cfg.IsParallel()
	if m.options.Parallel != nil {
		parallel = *m.options.Parallel
	}

	maxWorkers := m.options.MaxWorkers
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

	logger.System("Deployment Plan: %d service(s) across %d stage(s) [Tags: %s, Parallel: %t, Max Workers: %d]",
		len(services), len(stageNums), tagsDesc, parallel, maxWorkers)

	// 标签生命周期状态跟踪（多标签服务计入其所含的每个标签）
	tagTotalServices := make(map[string]int)
	for _, svc := range services {
		for _, tag := range serviceTags(svc) {
			tagTotalServices[tag]++
		}
	}
	tagCompletedSuccess := make(map[string]int)
	tagPreDeployExecuted := make(map[string]bool)
	tagPostDeployExecuted := make(map[string]bool)

	globalServiceIdx := 0
	abortedDueToFailure := false

	// 按 Stage 升序串行依次执行每个阶段
	for _, stageNum := range stageNums {
		stageServices := stageMap[stageNum]

		// 若前置阶段已失败，触发流水线熔断保护，阻断后续所有阶段
		if abortedDueToFailure {
			for _, svc := range stageServices {
				res := ServiceResult{
					ServiceName: svc.Name,
					Tags:        svc.Tags,
					Type:        svc.Type,
					Stage:       stageNum,
					Host:        fmt.Sprintf("%s:%d", svc.Server.Host, svc.Server.Port),
					Success:     false,
					Error:       fmt.Errorf("skipped: preceding stage failed (pipeline circuit-breaker triggered)"),
				}
				allResults = append(allResults, res)
				m.emit(EventServiceFinished, outcomeOf(res))
			}
			continue
		}

		if err := ctx.Err(); err != nil {
			for _, svc := range stageServices {
				res := ServiceResult{
					ServiceName: svc.Name,
					Tags:        svc.Tags,
					Type:        svc.Type,
					Stage:       stageNum,
					Host:        fmt.Sprintf("%s:%d", svc.Server.Host, svc.Server.Port),
					Success:     false,
					Error:       err,
				}
				allResults = append(allResults, res)
				m.emit(EventServiceFinished, outcomeOf(res))
			}
			continue
		}

		// 执行当前 Stage 中所涉标签的专属批次前置钩子 (Tag PreDeploy)
		for _, svc := range stageServices {
			for _, tag := range serviceTags(svc) {
				if tagPreDeployExecuted[tag] {
					continue
				}
				tagPreDeployExecuted[tag] = true
				tagCfg := m.cfg.FindTagHook(tag)
				if tagCfg != nil && len(tagCfg.Hooks.PreDeploy) > 0 {
					logger.System(">>> Running tag %q pre-deploy hooks (%d command(s))...", tag, len(tagCfg.Hooks.PreDeploy))
					tagLogger := logger.NewServiceLogger(tag+"-pre", -1)
					for i, cmd := range tagCfg.Hooks.PreDeploy {
						if strings.TrimSpace(cmd) == "" {
							continue
						}
						if err := ctx.Err(); err != nil {
							m.emitBatchFinished(allResults, false, ctx)
							return false, fmt.Errorf("tag %q pre-deploy hook canceled: %w", tag, err)
						}
						if err := ExecuteLocalCommandEnvContext(ctx, cmd, tagLogger, preEnv); err != nil {
							logger.Error("Tag %q pre-deploy hook command #%d failed: %v", tag, i+1, err)
							m.emitBatchFinished(allResults, false, ctx)
							return false, fmt.Errorf("tag %q pre-deploy hook failed: %w", tag, err)
						}
					}
					logger.Success("Tag %q pre-deploy hooks completed successfully.", tag)
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
							Tags:        s.Tags,
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

					stageResults[idx] = m.runOne(ctx, s, logIdx)
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
						Tags:        svc.Tags,
						Type:        svc.Type,
						Stage:       stageNum,
						Host:        fmt.Sprintf("%s:%d", svc.Server.Host, svc.Server.Port),
						Success:     false,
						Error:       err,
					}
					continue
				}
				stageResults[i] = m.runOne(ctx, svc, curIdx)
			}
		}

		globalServiceIdx += len(stageServices)
		allResults = append(allResults, stageResults...)

		// 检查当前 Stage 是否有任何服务失败，并累加标签成功计数（多标签服务计入其所含每个标签）
		for _, r := range stageResults {
			if !r.Success {
				abortedDueToFailure = true
				logger.Error("Stage %d deployment failed on service %q. Aborting subsequent stages!", stageNum, r.ServiceName)
				continue
			}
			for _, tag := range resultTags(r) {
				tagCompletedSuccess[tag]++
			}
		}

		// 检查当前 Stage 结束后是否有标签已全部成功完成，触发对应 Tag PostDeploy
		for tag, total := range tagTotalServices {
			if !tagPostDeployExecuted[tag] && tagCompletedSuccess[tag] == total {
				tagPostDeployExecuted[tag] = true
				tagCfg := m.cfg.FindTagHook(tag)
				if tagCfg != nil && len(tagCfg.Hooks.PostDeploy) > 0 {
					logger.System("\n>>> Running tag %q post-deploy hooks (%d command(s))...", tag, len(tagCfg.Hooks.PostDeploy))
					tagLogger := logger.NewServiceLogger(tag+"-post", -1)
					successSoFar, failedSoFar := countOutcomes(allResults)
					postEnv := hookEnvBase(successSoFar, failedSoFar, time.Since(totalStart).Milliseconds())
					for i, cmd := range tagCfg.Hooks.PostDeploy {
						if strings.TrimSpace(cmd) == "" {
							continue
						}
						if err := ctx.Err(); err != nil {
							logger.Error("Tag %q post-deploy hook canceled: %v", tag, err)
							break
						}
						if err := ExecuteLocalCommandEnvContext(ctx, cmd, tagLogger, postEnv); err != nil {
							logger.Error("Tag %q post-deploy hook command #%d failed: %v", tag, i+1, err)
							break
						}
					}
					logger.Success("Tag %q post-deploy hooks completed successfully.", tag)
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
		successCount, failedCount := countOutcomes(allResults)
		postEnv := hookEnvBase(successCount, failedCount, time.Since(totalStart).Milliseconds())
		for i, cmd := range m.cfg.Hooks.PostDeploy {
			if strings.TrimSpace(cmd) == "" {
				continue
			}
			if err := ctx.Err(); err != nil {
				logger.Error("Post-deploy hook canceled: %v", err)
				allSuccess = false
				break
			}
			if err := ExecuteLocalCommandEnvContext(ctx, cmd, batchLogger, postEnv); err != nil {
				logger.Error("Global post-deploy hook command #%d failed: %v", i+1, err)
				allSuccess = false
				break
			}
		}
		if allSuccess {
			logger.Success("Global post-deploy hooks completed successfully.")
		}
	}

	m.emitBatchFinished(allResults, allSuccess, ctx)
	return allSuccess, nil
}

// emitBatchFinished 汇总批次结果并发射结束事件（终态 + 全部节点结果，与历史落盘共用同一结构）
func (m *DeployManager) emitBatchFinished(results []ServiceResult, allSuccess bool, ctx context.Context) {
	status := BatchStatusFailed
	if allSuccess {
		status = BatchStatusSuccess
	} else if ctx.Err() != nil {
		status = BatchStatusCanceled
	}

	outcomes := make([]ServiceOutcome, 0, len(results))
	successCount := 0
	for _, r := range results {
		o := outcomeOf(r)
		if o.Status == ServiceStatusOK {
			successCount++
		}
		outcomes = append(outcomes, o)
	}

	m.emit(EventBatchFinished, BatchRecord{
		ID:         m.batchID,
		Workspace:  m.options.Workspace,
		Tags:       normalizeTagList(m.options.TargetTags),
		Start:      m.batchStart,
		End:        time.Now(),
		DurationMs: time.Since(m.batchStart).Milliseconds(),
		Total:      len(outcomes),
		Success:    successCount,
		Failed:     len(outcomes) - successCount,
		Status:     status,
		Services:   outcomes,
	})
}

// serviceTags 返回服务的规范化标签列表（历史遗留：空标签兜底默认标签 "default"）
func serviceTags(svc config.ServiceConfig) []string {
	if len(svc.Tags) == 0 {
		return []string{config.DefaultTag}
	}
	return svc.Tags
}

// resultTags 返回服务结果的标签列表（空值兜底默认标签 "default"）
func resultTags(r ServiceResult) []string {
	if len(r.Tags) == 0 {
		return []string{config.DefaultTag}
	}
	return r.Tags
}

// normalizeTagList 规范化标签列表：trim、小写、去空、去重（保持首次出现顺序）
func normalizeTagList(tags []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// filterServices 筛选启用的与目标指定的服务（支持标签、类型与服务名多重过滤；标签间为并集语义）
func (m *DeployManager) filterServices() ([]config.ServiceConfig, error) {
	// 命令行与调用方指定的过滤列表
	targetServices := toLowerSet(m.options.TargetServices)
	targetTags := toLowerSet(m.options.TargetTags)
	targetTypes := toLowerSet(m.options.TargetTypes)

	filtered := make([]config.ServiceConfig, 0)
	for _, svc := range m.cfg.Services {
		if !svc.IsEnabled() {
			continue
		}

		sName := strings.ToLower(strings.TrimSpace(svc.Name))

		// 1. 服务名过滤
		if len(targetServices) > 0 && !targetServices[sName] {
			continue
		}
		// 2. 标签过滤：并集语义——服务携带任一选中标签即命中
		if len(targetTags) > 0 && !svcHasAnyTag(svc, targetTags) {
			continue
		}
		// 3. 类型过滤：旧版 type 语义迁移为步骤开关后按开关等价匹配
		if len(targetTypes) > 0 && !svcMatchesTypeFilter(svc, targetTypes) {
			continue
		}

		filtered = append(filtered, svc)
	}

	return filtered, nil
}

// svcMatchesTypeFilter 类型过滤：优先匹配旧版显式 type 字段（兼容未迁移的内存配置），
// 迁移后（type 为空）按步骤开关推导等价类型语义
func svcMatchesTypeFilter(svc config.ServiceConfig, targetTypes map[string]bool) bool {
	for t := range targetTypes {
		if t == strings.ToLower(strings.TrimSpace(svc.Type)) {
			return true
		}
		steps := svc.Steps
		switch t {
		case config.DeployTypeStandard:
			if steps.TypeLabel() == config.DeployTypeStandard {
				return true
			}
		case config.DeployTypeExecOnly:
			if steps.TypeLabel() == config.DeployTypeExecOnly {
				return true
			}
		case config.DeployTypeSyncOnly:
			if steps.TypeLabel() == config.DeployTypeSyncOnly {
				return true
			}
		}
	}
	return false
}

// svcHasAnyTag 检查服务是否携带目标标签集合中的任一标签（并集匹配）
func svcHasAnyTag(svc config.ServiceConfig, targetTags map[string]bool) bool {
	for _, tag := range serviceTags(svc) {
		if targetTags[tag] {
			return true
		}
	}
	return false
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

// countOutcomes 统计已完成结果中的成功与失败节点数（未成功一律计入失败，与 PrintSummary 口径一致）
func countOutcomes(results []ServiceResult) (success, failed int64) {
	for _, r := range results {
		if r.Success {
			success++
		} else {
			failed++
		}
	}
	return
}

// PrintSummary 格式化输出部署结果报告，呈现服务、标签、类型、阶段及执行耗时
func PrintSummary(results []ServiceResult, totalDuration time.Duration) bool {
	logger.System("\n============================= DEPLOYMENT SUMMARY =============================")
	fmt.Printf("%-3s %-16s %-14s %-10s %-6s %-20s %-10s %-10s %s\n",
		"#", "SERVICE", "TAGS", "TYPE", "STAGE", "TARGET", "STATUS", "DURATION", "DETAILS")
	fmt.Println(strings.Repeat("-", 102))

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

		tags := strings.Join(resultTags(r), ",")
		typ := r.Type
		if typ == "" {
			typ = config.DeployTypeStandard
		}
		stg := r.Stage
		if stg <= 0 {
			stg = 1
		}

		fmt.Printf("%-3d %-16s %-14s %-10s %-6d %-20s %-10s %-10s %s\n",
			i+1,
			r.ServiceName,
			tags,
			typ,
			stg,
			r.Host,
			statusStr,
			r.Duration.Round(time.Millisecond).String(),
			detailStr,
		)
	}

	fmt.Println(strings.Repeat("=", 102))
	summaryLine := fmt.Sprintf("Total: %d | Successful: %d | Failed: %d | Total Time: %v",
		len(results), successCount, failedCount, totalDuration.Round(time.Millisecond))

	if failedCount == 0 {
		logger.Success("%s\n", summaryLine)
		return true
	}

	logger.Error("%s\n", summaryLine)
	return false
}
