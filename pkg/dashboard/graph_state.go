package dashboard

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/cloudwego/eino-ptes/pkg/protocol"
	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/compose"
)

type GraphStateManager struct {
	mu          sync.RWMutex
	taskGraphs  map[string]*TaskGraphState
	broadcastFn func(update protocol.GraphNodeUpdate)
}

type TaskGraphState struct {
	TaskID    string                          `json:"task_id"`
	Nodes     map[string]*NodeState           `json:"nodes"`
	Edges     [][2]string                     `json:"edges"`
	StartTime time.Time                       `json:"start_time"`
	EndTime   *time.Time                      `json:"end_time,omitempty"`
}

type NodeState struct {
	Name      string                `json:"name"`
	State     protocol.GraphNodeState `json:"state"`
	Output    interface{}           `json:"output,omitempty"`
	Error     string                `json:"error,omitempty"`
	StartTime *time.Time            `json:"start_time,omitempty"`
	EndTime   *time.Time            `json:"end_time,omitempty"`
}

func NewGraphStateManager(broadcast func(update protocol.GraphNodeUpdate)) *GraphStateManager {
	return &GraphStateManager{
		taskGraphs:  make(map[string]*TaskGraphState),
		broadcastFn: broadcast,
	}
}

func (g *GraphStateManager) InitTaskGraph(taskID string, graphInfo *compose.GraphInfo) {
	g.mu.Lock()
	defer g.mu.Unlock()

	tgs := &TaskGraphState{
		TaskID:    taskID,
		Nodes:     make(map[string]*NodeState),
		Edges:     make([][2]string, 0),
		StartTime: time.Now(),
	}

	for name := range graphInfo.Nodes {
		tgs.Nodes[name] = &NodeState{
			Name:  name,
			State: protocol.GraphNodeStatePending,
		}
	}

	for from, toList := range graphInfo.Edges {
		for _, to := range toList {
			tgs.Edges = append(tgs.Edges, [2]string{from, to})
		}
	}

	g.taskGraphs[taskID] = tgs
}

func (g *GraphStateManager) UpdateNode(taskID, nodeName string, state protocol.GraphNodeState, output interface{}, err string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	tgs, ok := g.taskGraphs[taskID]
	if !ok {
		tgs = &TaskGraphState{
			TaskID:    taskID,
			Nodes:     make(map[string]*NodeState),
			StartTime: time.Now(),
		}
		g.taskGraphs[taskID] = tgs
	}

	node, ok := tgs.Nodes[nodeName]
	if !ok {
		node = &NodeState{Name: nodeName}
		tgs.Nodes[nodeName] = node
	}

	now := time.Now()
	if state == protocol.GraphNodeStateRunning && node.StartTime == nil {
		node.StartTime = &now
	}
	if (state == protocol.GraphNodeStateSuccess || state == protocol.GraphNodeStateFailed || state == protocol.GraphNodeStateSkipped) && node.EndTime == nil {
		node.EndTime = &now
	}

	node.State = state
	node.Output = output
	node.Error = err

	update := protocol.GraphNodeUpdate{
		TaskID:    taskID,
		NodeName:  nodeName,
		State:     state,
		Output:    output,
		Error:     err,
		Timestamp: now,
	}

	if g.broadcastFn != nil {
		go g.broadcastFn(update)
	}
}

func (g *GraphStateManager) GetTaskGraph(taskID string) *TaskGraphState {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.taskGraphs[taskID]
}

func (g *GraphStateManager) GetAllGraphs() map[string]*TaskGraphState {
	g.mu.RLock()
	defer g.mu.RUnlock()
	result := make(map[string]*TaskGraphState, len(g.taskGraphs))
	for k, v := range g.taskGraphs {
		result[k] = v
	}
	return result
}

func (g *GraphStateManager) BuildCallbackHandler(taskID string) callbacks.Handler {
	return callbacks.NewHandlerBuilder().
		OnStartFn(func(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
			g.UpdateNode(taskID, info.Name, protocol.GraphNodeStateRunning, nil, "")
			return ctx
		}).
		OnEndFn(func(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
			var out interface{}
			if output != nil {
				outBytes, _ := json.Marshal(output)
				_ = json.Unmarshal(outBytes, &out)
			}
			g.UpdateNode(taskID, info.Name, protocol.GraphNodeStateSuccess, out, "")
			return ctx
		}).
		OnErrorFn(func(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
			g.UpdateNode(taskID, info.Name, protocol.GraphNodeStateFailed, nil, err.Error())
			return ctx
		}).
		Build()
}

func (g *GraphStateManager) GraphCompileCallback(taskID string) compose.GraphCompileCallback {
	return &compileCallback{gm: g, taskID: taskID}
}

type compileCallback struct {
	gm     *GraphStateManager
	taskID string
}

func (c *compileCallback) OnFinish(ctx context.Context, info *compose.GraphInfo) {
	c.gm.InitTaskGraph(c.taskID, info)
}
