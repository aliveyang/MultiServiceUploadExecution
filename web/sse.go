package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"multi-service-deploy/logger"
)

// sseHistoryLimit 断线补发环形缓冲容量（事件条数，覆盖文本日志与结构化事件）
const sseHistoryLimit = 4096

// sseMessage 单条待推送的 SSE 消息：
// event 为空表示默认 message 事件（携带已格式化的纯文本日志行）；
// event 非空表示结构化命名事件（data 为单行 JSON）。
// seq 为全局单调递增事件号，用于 Last-Event-ID 断线补发。
type sseMessage struct {
	seq   uint64
	event string
	data  string
}

// SSEHub 管理 SSE 客户端连接、事件广播与断线补发缓冲
type SSEHub struct {
	mu      sync.Mutex
	clients map[chan sseMessage]bool
	seq     uint64
	history []sseMessage // 环形缓冲（容量 sseHistoryLimit），按 seq 升序
}

var hub = &SSEHub{
	clients: make(map[chan sseMessage]bool),
}

func (h *SSEHub) register(ch chan sseMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[ch] = true
}

func (h *SSEHub) unregister(ch chan sseMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, ch)
	close(ch)
}

// Broadcast 广播一条纯文本日志行（默认 message 事件，向后兼容）
func (h *SSEHub) Broadcast(msg string) {
	h.send(sseMessage{data: msg})
}

// BroadcastEvent 广播一条结构化命名事件（如 batch_started / service_finished），
// 负载序列化为单行 JSON；序列化失败时记录日志而不中断调用方
func (h *SSEHub) BroadcastEvent(name string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		logger.Error("SSE event %q marshal failed: %v", name, err)
		return
	}
	h.send(sseMessage{event: name, data: string(data)})
}

// send 分配单调事件号、写入补发缓冲并向所有客户端非阻塞投递；
// 客户端队列满时直接丢弃（慢客户端可通过重连 + Last-Event-ID 补发恢复）
func (h *SSEHub) send(m sseMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.seq++
	m.seq = h.seq
	h.history = append(h.history, m)
	if len(h.history) > sseHistoryLimit {
		h.history = h.history[len(h.history)-sseHistoryLimit:]
	}

	for ch := range h.clients {
		select {
		case ch <- m:
		default:
		}
	}
}

// replay 返回 seq 大于 since 的全部缓冲消息（调用方需持有 h.mu）
func (h *SSEHub) replayLocked(since uint64) []sseMessage {
	for i := range h.history {
		if h.history[i].seq > since {
			return h.history[i:]
		}
	}
	return nil
}

// handleSSE 处理实时日志与结构化事件推送；支持 Last-Event-ID 断线补发
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// EventSource 自动重连时会携带 Last-Event-ID 请求头，重放其后的缓冲事件
	var since uint64
	if raw := strings.TrimSpace(r.Header.Get("Last-Event-ID")); raw != "" {
		if v, err := strconv.ParseUint(raw, 10, 64); err == nil {
			since = v
		}
	}

	msgChan := make(chan sseMessage, 1024)
	hub.register(msgChan)
	defer hub.unregister(msgChan)

	// 发送初始连接通知（带事件号，供客户端跟踪断点）
	hub.mu.Lock()
	fmt.Fprintf(w, "id: %d\ndata: [CONNECTED] Real-time log stream ready\n\n", hub.seq)
	replay := hub.replayLocked(since)
	hub.mu.Unlock()
	flusher.Flush()

	for _, m := range replay {
		if !writeSSEMessage(w, m) {
			return
		}
		flusher.Flush()
	}

	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			return
		case msg, ok := <-msgChan:
			if !ok {
				return
			}
			if !writeSSEMessage(w, msg) {
				return
			}
			flusher.Flush()
		}
	}
}

// writeSSEMessage 按事件类型写出一条 SSE 消息
func writeSSEMessage(w http.ResponseWriter, msg sseMessage) bool {
	if msg.event != "" {
		// 命名结构化事件：data 恒为单行 JSON
		_, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", msg.seq, msg.event, msg.data)
		return err == nil
	}
	// 多行日志需兼容 SSE 格式
	lines := strings.Split(msg.data, "\n")
	for _, line := range lines {
		if line != "" {
			if _, err := fmt.Fprintf(w, "id: %d\ndata: %s\n", msg.seq, line); err != nil {
				return false
			}
		}
	}
	_, err := fmt.Fprintf(w, "\n")
	return err == nil
}
