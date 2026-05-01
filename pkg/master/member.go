package master

import (
	"sync"
	"time"

	"github.com/cloudwego/eino-ptes/pkg/protocol"
)

const heartbeatTimeout = 30 * time.Second

type MemberManager struct {
	mu      sync.RWMutex
	workers map[string]*protocol.WorkerInfo
	tasks   map[string]string
}

func NewMemberManager() *MemberManager {
	return &MemberManager{
		workers: make(map[string]*protocol.WorkerInfo),
		tasks:   make(map[string]string),
	}
}

func (m *MemberManager) Register(info *protocol.WorkerInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.workers[info.ID] = info
}

func (m *MemberManager) Unregister(workerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.workers, workerID)
}

func (m *MemberManager) UpdateHeartbeat(hb *protocol.Heartbeat) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if w, ok := m.workers[hb.WorkerID]; ok {
		w.Status = hb.Status
		w.CurrentTask = hb.CurrentTask
		w.LastHeartbeat = time.Now()
	}
}

func (m *MemberManager) GetIdleWorker(capability string) *protocol.WorkerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, w := range m.workers {
		if w.Status != protocol.WorkerStatusIdle {
			continue
		}
		if capability != "" {
			hasCap := false
			for _, c := range w.Capabilities {
				if c == capability {
					hasCap = true
					break
				}
			}
			if !hasCap {
				continue
			}
		}
		return w
	}
	return nil
}

func (m *MemberManager) GetWorkers() []*protocol.WorkerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*protocol.WorkerInfo, 0, len(m.workers))
	for _, w := range m.workers {
		result = append(result, w)
	}
	return result
}

func (m *MemberManager) GetWorker(workerID string) *protocol.WorkerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.workers[workerID]
}

func (m *MemberManager) CleanupStale() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var removed []string
	now := time.Now()
	for id, w := range m.workers {
		if now.Sub(w.LastHeartbeat) > heartbeatTimeout {
			delete(m.workers, id)
			removed = append(removed, id)
		}
	}
	return removed
}
