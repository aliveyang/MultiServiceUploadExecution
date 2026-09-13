package deployer

import (
	"strings"
	"time"
)

// 批次生命周期结构化事件名（经 DeployOptions.OnEvent 回调发射，Web 层消费）
const (
	EventBatchStarted    = "batch_started"
	EventServiceStarted  = "service_started"
	EventServiceFinished = "service_finished"
	EventBatchFinished   = "batch_finished"
)

// 批次终态（BatchRecord.Status）
const (
	BatchStatusSuccess  = "success"
	BatchStatusFailed   = "failed"
	BatchStatusCanceled = "canceled"
)

// BatchIDLayout 批次 ID 时间格式（本地时间，秒级精度，全局部署互斥下天然唯一）
const BatchIDLayout = "20060102-150405"

// NewBatchID 生成基于当前时间的批次 ID，如 "20260912-150405"
func NewBatchID() string {
	return time.Now().Format(BatchIDLayout)
}

// ServiceNodeInput 批次计划中的节点快照（batch_started 事件负载项）
type ServiceNodeInput struct {
	Name  string   `json:"name"`
	Tags  []string `json:"tags,omitempty"`
	Type  string   `json:"type"`
	Stage int      `json:"stage"`
	Host  string   `json:"host"`
}

// BatchStartedPayload 批次开始事件负载：宣告批次 ID 与全部计划节点
type BatchStartedPayload struct {
	ID         string             `json:"id"`
	Workspace  string             `json:"workspace,omitempty"`
	Tags       []string           `json:"tags,omitempty"` // 本次批次的目标标签筛选（空表示全量）
	Parallel   bool               `json:"parallel"`
	MaxWorkers int                `json:"maxWorkers"`
	Total      int                `json:"total"`
	Stages     []int              `json:"stages,omitempty"`
	Services   []ServiceNodeInput `json:"services"`
}

// ServiceOutcome 单个服务节点的执行结果快照（JSON 序列化友好，供 SSE 与历史记录共用）
type ServiceOutcome struct {
	Name       string   `json:"name"`
	Tags       []string `json:"tags,omitempty"`
	Type       string   `json:"type"`
	Stage      int      `json:"stage"`
	Host       string   `json:"host"`
	Status     string   `json:"status"` // ok | failed | skipped | canceled
	Error      string   `json:"error,omitempty"`
	DurationMs int64    `json:"durationMs"`
	Files      int      `json:"files,omitempty"`
	Bytes      int64    `json:"bytes,omitempty"`
}

// ServiceStatusOK / ServiceStatusFailed / ServiceStatusSkipped / ServiceStatusCanceled 节点状态常量
const (
	ServiceStatusOK       = "ok"
	ServiceStatusFailed   = "failed"
	ServiceStatusSkipped  = "skipped"
	ServiceStatusCanceled = "canceled"
)

// outcomeOf 将流水线原始结果转换为可序列化的节点结果快照
func outcomeOf(r ServiceResult) ServiceOutcome {
	o := ServiceOutcome{
		Name:       r.ServiceName,
		Tags:       r.Tags,
		Type:       r.Type,
		Stage:      r.Stage,
		Host:       r.Host,
		DurationMs: r.Duration.Milliseconds(),
	}
	if r.Error != nil {
		o.Error = r.Error.Error()
	}
	switch {
	case r.Success:
		o.Status = ServiceStatusOK
	case strings.Contains(o.Error, "canceled"):
		o.Status = ServiceStatusCanceled
	case strings.Contains(o.Error, "skipped"):
		o.Status = ServiceStatusSkipped
	default:
		o.Status = ServiceStatusFailed
	}
	if r.Stats != nil {
		o.Files = r.Stats.TotalFiles
		o.Bytes = r.Stats.TotalBytes
	}
	return o
}

// BatchRecord 一次部署批次的完整记录（batch_finished 事件负载与历史落盘文件共用同一结构）
type BatchRecord struct {
	ID         string           `json:"id"`
	Workspace  string           `json:"workspace,omitempty"`
	Tags       []string         `json:"tags,omitempty"` // 本次批次的目标标签筛选（空表示全量）
	Start      time.Time        `json:"start"`
	End        time.Time        `json:"end"`
	DurationMs int64            `json:"durationMs"`
	Total      int              `json:"total"`
	Success    int              `json:"success"`
	Failed     int              `json:"failed"`
	Status     string           `json:"status"` // success | failed | canceled
	Services   []ServiceOutcome `json:"services"`
}
