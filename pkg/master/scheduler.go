package master

import (
	"context"
	"fmt"
	"sync"

	"github.com/cloudwego/eino-ptes/pkg/protocol"
)

type Scheduler struct {
	memberMgr   *MemberManager
	conns       map[string]*WorkerConn
	mu          sync.RWMutex
	taskUpdates chan protocol.Task
}

type WorkerConn struct {
	WorkerID string
	Encoder  interface{}
	Conn     interface{}
}

func NewScheduler(mm *MemberManager) *Scheduler {
	return &Scheduler{
		memberMgr:   mm,
		conns:       make(map[string]*WorkerConn),
		taskUpdates: make(chan protocol.Task, 100),
	}
}

func (s *Scheduler) RegisterConn(workerID string, encoder interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conns[workerID] = &WorkerConn{
		WorkerID: workerID,
		Encoder:  encoder,
	}
}

func (s *Scheduler) UnregisterConn(workerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, workerID)
}

func (s *Scheduler) Dispatch(ctx context.Context, task *protocol.Task, capability string) (*protocol.WorkerInfo, error) {
	worker := s.memberMgr.GetIdleWorker(capability)
	if worker == nil {
		return nil, fmt.Errorf("no idle worker with capability %s", capability)
	}

	s.mu.RLock()
	conn, ok := s.conns[worker.ID]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("worker %s connection not found", worker.ID)
	}

	task.WorkerID = worker.ID
	task.Status = protocol.TaskStatusAssigned

	msg := protocol.Message{
		Type:    protocol.MsgTypeDispatchTask,
		Payload: task,
	}

	if enc, ok := conn.Encoder.(interface{ Encode(v interface{}) error }); ok {
		if err := enc.Encode(msg); err != nil {
			return nil, fmt.Errorf("dispatch task to worker %s: %w", worker.ID, err)
		}
	}

	s.memberMgr.mu.Lock()
	if w, exists := s.memberMgr.workers[worker.ID]; exists {
		w.Status = protocol.WorkerStatusBusy
		w.CurrentTask = task.ID
	}
	s.memberMgr.mu.Unlock()

	return worker, nil
}
