package deployer

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"multi-service-deploy/config"
)

func TestNewBatchIDFormat(t *testing.T) {
	id := NewBatchID()
	if !regexp.MustCompile(`^\d{8}-\d{6}$`).MatchString(id) {
		t.Errorf("expected batch id format YYYYMMDD-HHMMSS, got %q", id)
	}
}

func TestEnsureUniqueBatchID(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if got := EnsureUniqueBatchID(dir, "20260912-080000"); got != "20260912-080000" {
		t.Errorf("expected original id when dir empty, got %q", got)
	}

	if err := os.WriteFile(filepath.Join(dir, "batch-20260912-080000.json"), []byte("{}"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := EnsureUniqueBatchID(dir, "20260912-080000"); got != "20260912-080000-1" {
		t.Errorf("expected suffixed id, got %q", got)
	}
}

// TestManagerEmitsBatchEvents 验证批次生命周期结构化事件的完整发射序列
// （使用不可达端口让流水线快速失败，无需真实 SSH 服务端）
func TestManagerEmitsBatchEvents(t *testing.T) {
	cfg := &config.DeployConfig{
		Services: []config.ServiceConfig{
			{
				Name:  "mock-unreachable",
				Group: "backend",
				Type:  config.DeployTypeExecOnly,
				Stage: 1,
				Server: config.ServerConfig{
					Host:           "127.0.0.1",
					Port:           1, // 保留端口，连接必然被拒
					Username:       "tester",
					Password:       "pwd",
					ConnectTimeout: 1,
				},
			},
		},
	}

	var events []string
	var finishedStatus string
	var outcomeCount int
	opts := DeployOptions{
		BatchID:   "20260912-100000",
		Workspace: "default",
		OnEvent: func(event string, payload any) {
			events = append(events, event)
			switch event {
			case EventServiceFinished:
				if o, ok := payload.(ServiceOutcome); ok && o.Name == "mock-unreachable" && o.Status == ServiceStatusFailed {
					outcomeCount++
				}
			case EventBatchFinished:
				if rec, ok := payload.(BatchRecord); ok {
					finishedStatus = rec.Status
					if rec.ID != "20260912-100000" {
						t.Errorf("expected batch id to propagate, got %q", rec.ID)
					}
					if rec.Workspace != "default" {
						t.Errorf("expected workspace to propagate, got %q", rec.Workspace)
					}
				}
			}
		},
	}

	mgr := NewDeployManager(cfg, opts)
	allSuccess, err := mgr.RunWithContext(context.Background())
	if err != nil {
		t.Fatalf("RunWithContext returned error: %v", err)
	}
	if allSuccess {
		t.Errorf("expected deployment to fail against unreachable host")
	}

	expect := []string{EventBatchStarted, EventServiceStarted, EventServiceFinished, EventBatchFinished}
	if len(events) != len(expect) {
		t.Fatalf("expected %d events, got %d: %v", len(expect), len(events), events)
	}
	for i, e := range expect {
		if events[i] != e {
			t.Errorf("event[%d]: expected %s, got %s", i, e, events[i])
		}
	}
	if outcomeCount != 1 {
		t.Errorf("expected one failed service outcome, got %d", outcomeCount)
	}
	if finishedStatus != BatchStatusFailed {
		t.Errorf("expected batch status failed, got %q", finishedStatus)
	}
}

func TestOutcomeOfStatusMapping(t *testing.T) {
	ok := outcomeOf(ServiceResult{ServiceName: "a", Success: true, Duration: 1500 * time.Millisecond})
	if ok.Status != ServiceStatusOK || ok.DurationMs != 1500 {
		t.Errorf("unexpected ok outcome: %+v", ok)
	}

	fail := outcomeOf(ServiceResult{ServiceName: "b", Success: false, Error: assertNewError("SSH connection failed")})
	if fail.Status != ServiceStatusFailed {
		t.Errorf("unexpected failed outcome: %+v", fail)
	}

	skip := outcomeOf(ServiceResult{ServiceName: "c", Success: false, Error: assertNewError("skipped: preceding stage failed")})
	if skip.Status != ServiceStatusSkipped {
		t.Errorf("unexpected skipped outcome: %+v", skip)
	}
}

type staticError struct{ msg string }

func (e *staticError) Error() string { return e.msg }

func assertNewError(msg string) error { return &staticError{msg: msg} }
