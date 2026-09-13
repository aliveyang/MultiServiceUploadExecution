package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"multi-service-deploy/config"
	"multi-service-deploy/deployer"
	"multi-service-deploy/logger"
)

// batchIDPattern 批次 ID 合法性（防路径穿透）：仅允许字母数字下划线短横线
var batchIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// historyDir 指定工作空间的批次历史归档目录（workspaces/<ws>/history/）
func historyDir(workspaceDir, ws string) string {
	return filepath.Join(workspaceDir, ws, "history")
}

// batchFilePath 批次记录 JSON / 归档日志的文件路径（ext 为 ".json" 或 ".log"）
func batchFilePath(workspaceDir, ws, batchID, ext string) string {
	return filepath.Join(historyDir(workspaceDir, ws), "batch-"+batchID+ext)
}

// historyStore 批次历史存储：持有当前部署的收集器，负责批次记录与日志落盘。
// 同一时刻至多一个收集器活跃（由部署全局互斥锁 isDeploying 保证）。
type historyStore struct {
	wsDir  func() string // 动态解析工作空间根目录
	mu     sync.Mutex
	active *batchCollector
}

// batchCollector 单个批次的记录收集器：消费 deployer 生命周期事件，
// 归档广播日志，并在批次结束时将 BatchRecord 落盘。
// 并发模型：rec 仅由部署 goroutine 经 observe/end 串行访问（免锁）；
// logMu 仅保护 logFile——teeLog 来自任意 logger 回调 goroutine。
// 注意：持锁期间严禁调用 logger（OnLog → teeLog → logMu 会自死锁）。
type batchCollector struct {
	ws      func() string // 动态解析工作空间根目录
	scope   string        // 归属工作空间 ID
	logMu   sync.Mutex
	rec     *deployer.BatchRecord
	logFile *os.File
	done    bool
}

func newHistoryStore(wsDir func() string) *historyStore {
	return &historyStore{wsDir: wsDir}
}

// begin 开启新的批次收集器；ws 非法时返回 no-op 收集器（不落盘）
func (h *historyStore) begin(ws, batchID string) *batchCollector {
	c := &batchCollector{ws: h.wsDir, scope: ws}
	if !config.IsValidWorkspaceID(ws) || !batchIDPattern.MatchString(batchID) {
		c.scope = ""
		return c
	}
	h.mu.Lock()
	h.active = c
	h.mu.Unlock()
	return c
}

// end 结束收集器：正常路径下记录已由 batch_finished 事件落盘；
// 此处兜底处理事件缺失的异常/被取消路径，保证"批次开始必有其记录"
func (h *historyStore) end(c *batchCollector, statusHint string) {
	h.mu.Lock()
	if h.active == c {
		h.active = nil
	}
	h.mu.Unlock()
	if c != nil {
		c.finalize(statusHint)
	}
}

// teeLog 将广播日志同步写入当前批次的归档日志文件（无活跃批次时为空操作）
func (h *historyStore) teeLog(line string) {
	h.mu.Lock()
	c := h.active
	h.mu.Unlock()
	if c != nil {
		c.appendLog(line)
	}
}

// observe 消费批次生命周期事件（仅由部署 goroutine 串行调用，免锁）
func (c *batchCollector) observe(name string, payload any) {
	switch name {
	case deployer.EventBatchStarted:
		started, ok := payload.(deployer.BatchStartedPayload)
		if !ok {
			return
		}
		now := time.Now()
		c.rec = &deployer.BatchRecord{
			ID:        started.ID,
			Workspace: started.Workspace,
			Tags:      started.Tags,
			Start:     now,
			Total:     started.Total,
			Status:    deployer.BatchStatusFailed,
			Services:  []deployer.ServiceOutcome{},
		}
		c.openLogFile()

	case deployer.EventServiceFinished:
		outcome, ok := payload.(deployer.ServiceOutcome)
		if !ok || c.rec == nil {
			return
		}
		c.rec.Services = append(c.rec.Services, outcome)

	case deployer.EventBatchFinished:
		record, ok := payload.(deployer.BatchRecord)
		if !ok || c.rec == nil {
			return
		}
		merged := record
		// 补齐收集器本地掌握的字段（批次开始时间与可能遗漏的节点结果）
		if merged.Start.IsZero() {
			merged.Start = c.rec.Start
		}
		if len(merged.Services) == 0 {
			merged.Services = c.rec.Services
		}
		c.rec = &merged
		c.persistLocked()
	}
}

// openLogFile 创建并打开批次归档日志文件
func (c *batchCollector) openLogFile() {
	if c.ws == nil || c.scope == "" || c.rec == nil {
		return
	}
	dir := historyDir(c.ws(), c.scope)
	if err := os.MkdirAll(dir, 0755); err != nil {
		logger.Error("Failed to create history dir: %v", err)
		return
	}
	c.logMu.Lock()
	defer c.logMu.Unlock()
	f, err := os.OpenFile(batchFilePath(c.ws(), c.scope, c.rec.ID, ".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		logger.Error("Failed to open batch log file: %v", err)
		return
	}
	c.logFile = f
}

// appendLog 追加一行广播日志到归档文件（错误静默降级：归档失败不影响部署主流程）
func (c *batchCollector) appendLog(line string) {
	c.logMu.Lock()
	defer c.logMu.Unlock()
	if c.logFile == nil {
		return
	}
	if _, err := c.logFile.WriteString(line + "\n"); err != nil {
		// 此处处于 logger 回调链上，禁止再调用 logger（会递归自死锁）
		_ = c.logFile.Close()
		c.logFile = nil
	}
}

// finalize 若批次记录尚未落盘则按状态提示落盘并关闭日志文件（幂等）
func (c *batchCollector) finalize(statusHint string) {
	if c.rec != nil && !c.done {
		if statusHint != "" {
			c.rec.Status = statusHint
		}
		c.persistLocked()
	}
	c.logMu.Lock()
	defer c.logMu.Unlock()
	if c.logFile != nil {
		_ = c.logFile.Close()
		c.logFile = nil
	}
}

// persistLocked 将批次记录写入 JSON 文件并标记完成。
// 免锁约定：rec 仅由部署 goroutine 访问；本函数内的 logger 调用安全
// （teeLog 只请求 logMu，与本函数无交集）。
func (c *batchCollector) persistLocked() {
	if c.rec == nil || c.ws == nil || c.scope == "" {
		return
	}
	dir := historyDir(c.ws(), c.scope)
	if err := os.MkdirAll(dir, 0755); err != nil {
		logger.Error("Failed to create history dir: %v", err)
		return
	}
	if c.rec.End.IsZero() {
		c.rec.End = time.Now()
	}
	if c.rec.DurationMs <= 0 {
		c.rec.DurationMs = c.rec.End.Sub(c.rec.Start).Milliseconds()
	}
	c.rec.Total = len(c.rec.Services)
	c.rec.Success = 0
	c.rec.Failed = 0
	for _, o := range c.rec.Services {
		if o.Status == deployer.ServiceStatusOK {
			c.rec.Success++
		} else {
			c.rec.Failed++
		}
	}

	data, err := json.MarshalIndent(c.rec, "", "  ")
	if err != nil {
		logger.Error("Failed to marshal batch record: %v", err)
		return
	}
	path := batchFilePath(c.ws(), c.scope, c.rec.ID, ".json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		logger.Error("Failed to write batch record: %v", err)
		return
	}
	c.done = true
	logger.System("Batch record archived: %s", path)
}

// batchSummary 历史列表条目（不含节点明细，降低列表负载）
type batchSummary struct {
	ID         string   `json:"id"`
	Workspace  string   `json:"workspace,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Start      string   `json:"start"`
	End        string   `json:"end"`
	DurationMs int64    `json:"durationMs"`
	Total      int      `json:"total"`
	Success    int      `json:"success"`
	Failed     int      `json:"failed"`
	Status     string   `json:"status"`
}

// handleDeployHistory GET 批次部署历史列表（?workspace=&limit=，默认最近 30 批）
func (s *Server) handleDeployHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_, wsID := s.resolveWorkspacePath(r)

	limit := 30
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}

	records, err := loadHistoryRecords(s.workspaceDir, wsID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to load history: %v", err), http.StatusInternalServerError)
		return
	}

	list := make([]batchSummary, 0, len(records))
	for _, rec := range records {
		list = append(list, batchSummary{
			ID:         rec.ID,
			Workspace:  rec.Workspace,
			Tags:       rec.Tags,
			Start:      rec.Start.Format(time.RFC3339),
			End:        rec.End.Format(time.RFC3339),
			DurationMs: rec.DurationMs,
			Total:      rec.Total,
			Success:    rec.Success,
			Failed:     rec.Failed,
			Status:     rec.Status,
		})
		if len(list) >= limit {
			break
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Workspace-ID", wsID)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"history": list})
}

// handleDeployHistoryDetail GET 单个批次详情（/api/deploy/history/{id}?workspace=）
func (s *Server) handleDeployHistoryDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_, wsID := s.resolveWorkspacePath(r)

	id := r.PathValue("id")
	if !batchIDPattern.MatchString(id) {
		http.Error(w, "invalid batch id", http.StatusBadRequest)
		return
	}

	data, err := os.ReadFile(batchFilePath(s.workspaceDir, wsID, id, ".json"))
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, fmt.Sprintf("batch %q not found", id), http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("Failed to read batch record: %v", err), http.StatusInternalServerError)
		return
	}

	var rec deployer.BatchRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse batch record: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Workspace-ID", wsID)
	_ = json.NewEncoder(w).Encode(rec)
}

// loadHistoryRecords 读取指定工作空间全部批次记录，按批次 ID 降序（新批次在前）
func loadHistoryRecords(workspaceDir, ws string) ([]deployer.BatchRecord, error) {
	dir := historyDir(workspaceDir, ws)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read history dir: %w", err)
	}

	var records []deployer.BatchRecord
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "batch-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var rec deployer.BatchRecord
		if err := json.Unmarshal(data, &rec); err != nil || rec.ID == "" {
			continue
		}
		records = append(records, rec)
	}

	sort.Slice(records, func(i, j int) bool { return records[i].ID > records[j].ID })
	return records, nil
}
