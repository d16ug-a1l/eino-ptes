package master

import (
	"testing"
	"time"

	"github.com/cloudwego/eino-ptes/pkg/protocol"
)

func TestMemberManagerRegisterAndGet(t *testing.T) {
	mm := NewMemberManager()

	w := &protocol.WorkerInfo{
		ID:            "w-1",
		Name:          "kali-1",
		Capabilities:  []string{"nmap", "nikto"},
		Status:        protocol.WorkerStatusIdle,
		LastHeartbeat: time.Now(),
	}

	mm.Register(w)

	got := mm.GetWorker("w-1")
	if got == nil {
		t.Fatal("expected worker to be found")
	}
	if got.Name != "kali-1" {
		t.Errorf("expected name kali-1, got %s", got.Name)
	}
}

func TestMemberManagerGetIdleWorker(t *testing.T) {
	mm := NewMemberManager()

	mm.Register(&protocol.WorkerInfo{
		ID:            "w-1",
		Name:          "kali-1",
		Capabilities:  []string{"nmap"},
		Status:        protocol.WorkerStatusIdle,
		LastHeartbeat: time.Now(),
	})
	mm.Register(&protocol.WorkerInfo{
		ID:            "w-2",
		Name:          "kali-2",
		Capabilities:  []string{"nikto"},
		Status:        protocol.WorkerStatusIdle,
		LastHeartbeat: time.Now(),
	})

	idle := mm.GetIdleWorker("nmap")
	if idle == nil {
		t.Fatal("expected idle worker for nmap")
	}
	if idle.ID != "w-1" {
		t.Errorf("expected w-1, got %s", idle.ID)
	}

	idle = mm.GetIdleWorker("sqlmap")
	if idle != nil {
		t.Error("expected no idle worker for sqlmap")
	}
}

func TestMemberManagerCleanupStale(t *testing.T) {
	mm := NewMemberManager()

	mm.Register(&protocol.WorkerInfo{
		ID:            "w-1",
		Name:          "kali-1",
		Status:        protocol.WorkerStatusIdle,
		LastHeartbeat: time.Now().Add(-2 * time.Minute),
	})

	removed := mm.CleanupStale()
	if len(removed) != 1 || removed[0] != "w-1" {
		t.Errorf("expected w-1 to be removed, got %v", removed)
	}

	if mm.GetWorker("w-1") != nil {
		t.Error("expected w-1 to be gone")
	}
}
