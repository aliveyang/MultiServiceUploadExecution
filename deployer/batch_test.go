package deployer

import (
	"context"
	"regexp"
	"strings"
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

// TestBatchHookEnv 验证批次钩子注入环境变量的构造：变量恒全量定义且整数值无填充
func TestBatchHookEnv(t *testing.T) {
	env := batchHookEnv("prod", "backend,data", "20260912-150405", "deploy.json", 4, 3, 1, 12345)
	want := map[string]string{
		"SPACE":        "prod",
		"TAGS":         "backend,data",
		"BATCH_ID":     "20260912-150405",
		"CONFIG":       "deploy.json",
		"NODE_TOTAL":   "4",
		"NODE_SUCCESS": "3",
		"NODE_FAILED":  "1",
		"DURATION":     "12345",
	}
	got := map[string]string{}
	for _, kv := range env {
		parts := strings.SplitN(kv, "=", 2)
		got[parts[0]] = parts[1]
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("env %s: expected %q, got %q", k, v, got[k])
		}
	}
	if len(env) != len(want) {
		t.Errorf("expected %d env vars, got %d", len(want), len(env))
	}

	// 空字符串在 Windows 环境块中等同未定义，必须回退为非空默认值
	fallback := strings.Join(batchHookEnv("", "", "b", "c", 0, 0, 0, 0), "\n")
	if !strings.Contains(fallback, "SPACE=default") || !strings.Contains(fallback, "TAGS=none") {
		t.Errorf("expected empty workspace/tags to fall back, got %s", fallback)
	}
}

// TestManagerEmitsBatchEvents 验证批次生命周期结构化事件的完整发射序列
// （使用不可达端口让流水线快速失败，无需真实 SSH 服务端）
func TestManagerEmitsBatchEvents(t *testing.T) {
	cfg := &config.DeployConfig{
		Services: []config.ServiceConfig{
			{
				Name:  "mock-unreachable",
				Tags:  []string{"backend"},
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
	var startedTags []string
	var outcomeTags []string
	opts := DeployOptions{
		BatchID:    "20260912-100000",
		Workspace:  "default",
		TargetTags: []string{"Backend", "data", "Backend"}, // 验证批次记录中的标签规范化（小写去重）
		OnEvent: func(event string, payload any) {
			events = append(events, event)
			switch event {
			case EventBatchStarted:
				if p, ok := payload.(BatchStartedPayload); ok {
					startedTags = p.Tags
				}
			case EventServiceFinished:
				if o, ok := payload.(ServiceOutcome); ok && o.Name == "mock-unreachable" && o.Status == ServiceStatusFailed {
					outcomeCount++
					outcomeTags = o.Tags
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
					if len(rec.Tags) != 2 || rec.Tags[0] != "backend" || rec.Tags[1] != "data" {
						t.Errorf("expected normalized tags [backend data], got %v", rec.Tags)
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
	if len(startedTags) != 2 || startedTags[0] != "backend" {
		t.Errorf("expected batch_started tags [backend data], got %v", startedTags)
	}
	if len(outcomeTags) != 1 || outcomeTags[0] != "backend" {
		t.Errorf("expected outcome tags [backend], got %v", outcomeTags)
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
