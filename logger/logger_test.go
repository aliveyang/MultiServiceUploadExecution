package logger

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

// captureLog 临时接管全局 OnLog 回调，将日志行收集到缓冲（write 持锁串行调用，无需额外加锁）
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := OnLog
	OnLog = func(line string) { buf.WriteString(line + "\n") }
	t.Cleanup(func() { OnLog = prev })
	return buf
}

func TestServiceLoggerThreadSafety(t *testing.T) {
	buf := captureLog(t)

	log1 := NewServiceLogger("service-1", 0)
	log2 := NewServiceLogger("service-2", 1)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(idx int) {
			defer wg.Done()
			log1.Info("msg from svc1: %d", idx)
		}(i)
		go func(idx int) {
			defer wg.Done()
			log2.Info("msg from svc2: %d", idx)
		}(i)
	}

	wg.Wait()
	out := buf.String()
	if !strings.Contains(out, "service-1") || !strings.Contains(out, "service-2") {
		t.Fatalf("expected output to contain service names")
	}
}
