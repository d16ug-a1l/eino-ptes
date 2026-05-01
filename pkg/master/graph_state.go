package master

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/cloudwego/eino-ptes/pkg/protocol"
	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/compose"
)

type GraphState struct {
	mu         sync.RWMutex
	taskGraphs map[string]*TaskGraph
	server     *Server
}

type TaskGraph struct {
	TaskID    string                `json:"task_id"`
	Nodes     map[string]*NodeState `json:"nodes"`
	Edges     [][2]string           `json:"edges"`
	StartTime time.Time             `json:"start_time"`
	EndTime   *time.Time            `json:"end_time,omitempty"`
}

type NodeState struct {
	Name      string                  `json:"name"`
	State     protocol.GraphNodeState `json:"state"`
	Output    interface{}             `json:"output,omitempty"`
	Error     string                  `json:"error,omitempty"`
	StartTime *time.Time              `json:"start_time,omitempty"`
	EndTime   *time.Time              `json:"end_time,omitempty"`
}

func NewGraphState() *GraphState {
	return &GraphState{taskGraphs: make(map[string]*TaskGraph)}
}

func (g *GraphState) SetServer(s *Server) {
	g.server = s
}

func (g *GraphState) InitTaskGraph(taskID string, graphInfo *compose.GraphInfo) {
	g.mu.Lock()
	defer g.mu.Unlock()

	tg := &TaskGraph{
		TaskID:    taskID,
		Nodes:     make(map[string]*NodeState),
		Edges:     make([][2]string, 0),
		StartTime: time.Now(),
	}

	for name := range graphInfo.Nodes {
		tg.Nodes[name] = &NodeState{
			Name:  name,
			State: protocol.GraphNodeStatePending,
		}
	}

	for from, toList := range graphInfo.Edges {
		for _, to := range toList {
			tg.Edges = append(tg.Edges, [2]string{from, to})
		}
	}

	g.taskGraphs[taskID] = tg
}

func (g *GraphState) UpdateNode(taskID, nodeName string, state protocol.GraphNodeState, output interface{}, err string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	tg, ok := g.taskGraphs[taskID]
	if !ok {
		tg = &TaskGraph{
			TaskID:    taskID,
			Nodes:     make(map[string]*NodeState),
			StartTime: time.Now(),
		}
		g.taskGraphs[taskID] = tg
	}

	node, ok := tg.Nodes[nodeName]
	if !ok {
		node = &NodeState{Name: nodeName}
		tg.Nodes[nodeName] = node
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

	if g.server != nil {
		go g.server.BroadcastGraphUpdate(update)
	}
}

func (g *GraphState) GetTaskGraph(taskID string) *TaskGraph {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.taskGraphs[taskID]
}

func (g *GraphState) GetAllGraphs() map[string]*TaskGraph {
	g.mu.RLock()
	defer g.mu.RUnlock()
	result := make(map[string]*TaskGraph, len(g.taskGraphs))
	for k, v := range g.taskGraphs {
		result[k] = v
	}
	return result
}

func (g *GraphState) BuildCallbackHandler(taskID string) callbacks.Handler {
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

func (g *GraphState) GraphCompileCallback(taskID string) compose.GraphCompileCallback {
	return &compileCallback{gs: g, taskID: taskID}
}

type compileCallback struct {
	gs     *GraphState
	taskID string
}

func (c *compileCallback) OnFinish(ctx context.Context, info *compose.GraphInfo) {
	c.gs.InitTaskGraph(c.taskID, info)
}
