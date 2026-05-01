package protocol

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMessageSerialization(t *testing.T) {
	msg := Message{
		Type: MsgTypeHeartbeat,
		Payload: Heartbeat{
			WorkerID:  "worker-1",
			Status:    WorkerStatusIdle,
			Timestamp: time.Now(),
		},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}

	if decoded.Type != MsgTypeHeartbeat {
		t.Errorf("expected type %s, got %s", MsgTypeHeartbeat, decoded.Type)
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
