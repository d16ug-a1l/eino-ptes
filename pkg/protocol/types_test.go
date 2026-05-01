package protocol

import (
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
)

func TestHeartbeatMessage(t *testing.T) {
	hb := &Heartbeat{
		WorkerID:  "worker-1",
		Status:    WorkerStatusIdle,
		Timestamp: time.Now(),
	}
	msg := HeartbeatMessage(hb)

	if msg.Role != schema.System {
		t.Errorf("expected role system, got %s", msg.Role)
	}

	extracted := ExtractHeartbeat(msg)
	if extracted == nil {
		t.Fatal("expected to extract heartbeat")
	}
	if extracted.WorkerID != "worker-1" {
		t.Errorf("expected worker-1, got %s", extracted.WorkerID)
	}
}

func TestWorkerRegisterMessage(t *testing.T) {
	info := &WorkerInfo{
		ID:   "worker-1",
		Name: "kali-1",
		ToolInfos: []*schema.ToolInfo{
			{Name: "nmap", Desc: "network scanner"},
		},
	}
	msg := WorkerRegisterMessage(info)

	if msg.Role != schema.User {
		t.Errorf("expected role user, got %s", msg.Role)
	}

	extracted := ExtractWorkerInfo(msg)
	if extracted == nil {
		t.Fatal("expected to extract worker info")
	}
	if extracted.ID != "worker-1" {
		t.Errorf("expected worker-1, got %s", extracted.ID)
	}
	if len(extracted.ToolInfos) != 1 || extracted.ToolInfos[0].Name != "nmap" {
		t.Errorf("expected 1 tool info with name nmap, got %v", extracted.ToolInfos)
	}
}

func TestTaskStatusTransitions(t *testing.T) {
	task := &Task{
		ID:     "task-1",
		Type:   TaskTypeReconnaissance,
		Target: "192.168.1.1",
		Status: TaskStatusPending,
	}

	if task.Status != TaskStatusPending {
		t.Errorf("expected pending, got %s", task.Status)
	}

	task.Status = TaskStatusRunning
	if task.Status != TaskStatusRunning {
		t.Errorf("expected running, got %s", task.Status)
	}
}
