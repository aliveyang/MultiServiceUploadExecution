package logger

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

func TestServiceLoggerThreadSafety(t *testing.T) {
	buf := &bytes.Buffer{}
	SetOutput(buf)

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
