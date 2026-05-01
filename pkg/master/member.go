package master

import (
	"sync"
	"time"

	"github.com/cloudwego/eino-ptes/pkg/protocol"
)

const heartbeatTimeout = 30 * time.Second

type MemberManager struct {
	mu        sync.RWMutex
	workers   map[string]*protocol.WorkerInfo
	tasks     map[string]string
	toolIndex map[string][]string // tool name -> worker IDs
}

func NewMemberManager() *MemberManager {
	return &MemberManager{
		workers:   make(map[string]*protocol.WorkerInfo),
		tasks:     make(map[string]string),
		toolIndex: make(map[string][]string),
	}
}

func (m *MemberManager) Register(info *protocol.WorkerInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.workers[info.ID] = info

	// update tool index
	for toolName := range m.toolIndex {
		m.toolIndex[toolName] = filterSlice(m.toolIndex[toolName], info.ID)
	}
	for _, ti := range info.ToolInfos {
		m.toolIndex[ti.Name] = append(m.toolIndex[ti.Name], info.ID)
	}
}

func (m *MemberManager) Unregister(workerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.workers, workerID)
	for toolName := range m.toolIndex {
		m.toolIndex[toolName] = filterSlice(m.toolIndex[toolName], workerID)
	}
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

func (m *MemberManager) GetIdleWorker(toolOrCap string) *protocol.WorkerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// try tool name index first
	workerIDs, ok := m.toolIndex[toolOrCap]
	if ok {
		for _, id := range workerIDs {
			w, ok := m.workers[id]
			if !ok {
				continue
			}
			if w.Status == protocol.WorkerStatusIdle {
				return w
			}
		}
	}

	// fallback to capability matching for backward compatibility
	for _, w := range m.workers {
		if w.Status != protocol.WorkerStatusIdle {
			continue
		}
		for _, c := range w.Capabilities {
			if c == toolOrCap {
				return w
			}
		}
	}
	return nil
}

func (m *MemberManager) AcquireIdleWorker(toolOrCap string) *protocol.WorkerInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	// try tool name index first
	workerIDs, ok := m.toolIndex[toolOrCap]
	if ok {
		for _, id := range workerIDs {
			w, ok := m.workers[id]
			if !ok {
				continue
			}
			if w.Status == protocol.WorkerStatusIdle {
				w.Status = protocol.WorkerStatusBusy
				return w
			}
		}
	}

	// fallback to capability matching
	for _, w := range m.workers {
		if w.Status != protocol.WorkerStatusIdle {
			continue
		}
		for _, c := range w.Capabilities {
			if c == toolOrCap {
				w.Status = protocol.WorkerStatusBusy
				return w
			}
		}
	}
	return nil
}

func (m *MemberManager) ReleaseWorker(workerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if w, ok := m.workers[workerID]; ok {
		w.Status = protocol.WorkerStatusIdle
		w.CurrentTask = ""
	}
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
			for toolName := range m.toolIndex {
				m.toolIndex[toolName] = filterSlice(m.toolIndex[toolName], id)
			}
			removed = append(removed, id)
		}
	}
	return removed
}

func filterSlice(slice []string, exclude string) []string {
	var result []string
	for _, s := range slice {
		if s != exclude {
			result = append(result, s)
		}
	}
	return result
}
