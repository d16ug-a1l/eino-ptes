package master

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/cloudwego/eino/schema"
)

type Scheduler struct {
	memberMgr   *MemberManager
	conns       map[string]*WorkerConn
	mu          sync.RWMutex
	toolWaits   map[string]chan *schema.Message // callID -> result channel
	callWorkers map[string]string               // callID -> workerID
}

type WorkerConn struct {
	WorkerID string
	Encoder  *json.Encoder
}

func NewScheduler(mm *MemberManager) *Scheduler {
	return &Scheduler{
		memberMgr: mm,
		conns:     make(map[string]*WorkerConn),
		toolWaits:   make(map[string]chan *schema.Message),
		callWorkers: make(map[string]string),
	}
}

func (s *Scheduler) RegisterConn(workerID string, encoder *json.Encoder) {
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

func (s *Scheduler) DispatchToolCall(ctx context.Context, msg *schema.Message) (*schema.Message, error) {
	if msg == nil || len(msg.ToolCalls) == 0 {
		return nil, fmt.Errorf("no tool calls in message")
	}

	tc := msg.ToolCalls[0]
	worker := s.memberMgr.AcquireIdleWorker(tc.Function.Name)
	if worker == nil {
		return nil, fmt.Errorf("no idle worker with tool %s", tc.Function.Name)
	}

	s.mu.RLock()
	conn, ok := s.conns[worker.ID]
	s.mu.RUnlock()
	if !ok {
		s.memberMgr.ReleaseWorker(worker.ID)
		return nil, fmt.Errorf("worker %s connection not found", worker.ID)
	}

	resultCh := make(chan *schema.Message, 1)
	s.mu.Lock()
	s.toolWaits[tc.ID] = resultCh
	s.callWorkers[tc.ID] = worker.ID
	s.mu.Unlock()

	if err := conn.Encoder.Encode(msg); err != nil {
		s.mu.Lock()
		delete(s.toolWaits, tc.ID)
		delete(s.callWorkers, tc.ID)
		s.mu.Unlock()
		s.memberMgr.ReleaseWorker(worker.ID)
		return nil, fmt.Errorf("dispatch tool call to worker %s: %w", worker.ID, err)
	}

	select {
	case result := <-resultCh:
		return result, nil
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.toolWaits, tc.ID)
		delete(s.callWorkers, tc.ID)
		s.mu.Unlock()
		s.memberMgr.ReleaseWorker(worker.ID)
		return nil, ctx.Err()
	}
}

func (s *Scheduler) OnToolResult(callID string, msg *schema.Message) {
	s.mu.Lock()
	ch, ok := s.toolWaits[callID]
	if ok {
		delete(s.toolWaits, callID)
	}
	workerID := s.callWorkers[callID]
	delete(s.callWorkers, callID)
	s.mu.Unlock()

	if workerID != "" {
		s.memberMgr.ReleaseWorker(workerID)
	}

	if ok {
		ch <- msg
	}
}
