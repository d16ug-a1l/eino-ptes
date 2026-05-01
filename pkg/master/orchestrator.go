package master

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/cloudwego/eino-ptes/pkg/protocol"
	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// PTESState is the state object passed through the Eino Graph nodes.
type PTESState struct {
	TaskID  string
	Target  string
	Params  map[string]interface{}
	Results map[protocol.TaskType]*schema.ToolResult
}

type Orchestrator struct {
	scheduler     *Scheduler
	memberMgr     *MemberManager
	graphState    GraphStateManager
	taskStore     *TaskStore
	mu            sync.RWMutex
	runningTasks  map[string]context.CancelFunc
	compiledGraph compose.Runnable[*PTESState, *PTESState]
}

type GraphStateManager interface {
	UpdateNode(taskID, nodeName string, state protocol.GraphNodeState, output interface{}, err string)
	BuildCallbackHandler(taskID string) callbacks.Handler
	GraphCompileCallback(taskID string) compose.GraphCompileCallback
}

type TaskStore struct {
	mu    sync.RWMutex
	tasks map[string]*protocol.Task
}

func NewTaskStore() *TaskStore {
	return &TaskStore{tasks: make(map[string]*protocol.Task)}
}

func (ts *TaskStore) Save(task *protocol.Task) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.tasks[task.ID] = task
}

func (ts *TaskStore) Get(id string) *protocol.Task {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.tasks[id]
}

func (ts *TaskStore) UpdateResult(id string, result *schema.ToolResult) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if t, ok := ts.tasks[id]; ok {
		t.Result = result
		t.Status = protocol.TaskStatusCompleted
	}
}

func (ts *TaskStore) List() []*protocol.Task {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	result := make([]*protocol.Task, 0, len(ts.tasks))
	for _, t := range ts.tasks {
		result = append(result, t)
	}
	return result
}

func NewOrchestrator(scheduler *Scheduler, memberMgr *MemberManager, graphState GraphStateManager) *Orchestrator {
	o := &Orchestrator{
		scheduler:    scheduler,
		memberMgr:    memberMgr,
		graphState:   graphState,
		taskStore:    NewTaskStore(),
		runningTasks: make(map[string]context.CancelFunc),
	}
	return o
}

func (o *Orchestrator) InitGraph(ctx context.Context) error {
	g, err := o.buildGraph()
	if err != nil {
		return fmt.Errorf("build graph: %w", err)
	}

	opts := []compose.GraphCompileOption{
		compose.WithGraphName("ptes-pipeline"),
	}
	if o.graphState != nil {
		opts = append(opts, compose.WithGraphCompileCallbacks(o.graphState.GraphCompileCallback("__template__")))
	}

	runnable, err := g.Compile(ctx, opts...)
	if err != nil {
		return fmt.Errorf("compile graph: %w", err)
	}

	o.compiledGraph = runnable
	return nil
}

func (o *Orchestrator) buildGraph() (*compose.Graph[*PTESState, *PTESState], error) {
	g := compose.NewGraph[*PTESState, *PTESState]()

	// Reconnaissance node
	if err := g.AddLambdaNode("reconnaissance", compose.InvokableLambda(o.reconNode)); err != nil {
		return nil, err
	}

	// Vulnerability scan node
	if err := g.AddLambdaNode("vulnerability_scan", compose.InvokableLambda(o.vulnScanNode)); err != nil {
		return nil, err
	}

	// Branch: if recon succeeded, go to vuln_scan; otherwise END
	branch := compose.NewGraphBranch[*PTESState](func(ctx context.Context, state *PTESState) (string, error) {
		if r := state.Results[protocol.TaskTypeReconnaissance]; r != nil {
			return "vulnerability_scan", nil
		}
		return compose.END, nil
	}, map[string]bool{"vulnerability_scan": true, compose.END: true})

	if err := g.AddBranch("reconnaissance", branch); err != nil {
		return nil, err
	}

	g.AddEdge(compose.START, "reconnaissance")
	g.AddEdge("vulnerability_scan", compose.END)

	return g, nil
}

func (o *Orchestrator) reconNode(ctx context.Context, state *PTESState) (*PTESState, error) {
	task := &protocol.Task{
		ID:     state.TaskID,
		Type:   protocol.TaskTypeReconnaissance,
		Target: state.Target,
		Params: state.Params,
	}
	result, err := o.dispatchAndWait(ctx, task, "nmap")
	state.Results[protocol.TaskTypeReconnaissance] = result
	return state, err
}

func (o *Orchestrator) vulnScanNode(ctx context.Context, state *PTESState) (*PTESState, error) {
	task := &protocol.Task{
		ID:     state.TaskID,
		Type:   protocol.TaskTypeVulnerabilityScan,
		Target: state.Target,
		Params: nil,
	}
	result, err := o.dispatchAndWait(ctx, task, "nikto")
	state.Results[protocol.TaskTypeVulnerabilityScan] = result
	return state, err
}

func (o *Orchestrator) dispatchAndWait(ctx context.Context, task *protocol.Task, toolName string) (*schema.ToolResult, error) {
	msg := protocol.DispatchTaskMessage(task)
	// override tool call name to match actual tool
	if len(msg.ToolCalls) > 0 {
		msg.ToolCalls[0].Function.Name = toolName
	}

	resultMsg, err := o.scheduler.DispatchToolCall(ctx, msg)
	if err != nil {
		return nil, err
	}

	result := protocol.ExtractToolResult(resultMsg)
	if result == nil && resultMsg != nil {
		result = &schema.ToolResult{
			Parts: []schema.ToolOutputPart{
				{Type: schema.ToolPartTypeText, Text: resultMsg.Content},
			},
		}
	}

	return result, nil
}

func (o *Orchestrator) CreateTask(ctx context.Context, taskType protocol.TaskType, target string, params map[string]interface{}) (*protocol.Task, error) {
	now := time.Now()
	task := &protocol.Task{
		ID:        generateTaskID(),
		Type:      taskType,
		Target:    target,
		Params:    params,
		Status:    protocol.TaskStatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	o.taskStore.Save(task)
	return task, nil
}

func (o *Orchestrator) ExecuteTask(ctx context.Context, task *protocol.Task) error {
	if o.compiledGraph == nil {
		if err := o.InitGraph(ctx); err != nil {
			return err
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	o.mu.Lock()
	o.runningTasks[task.ID] = cancel
	o.mu.Unlock()
	defer func() {
		o.mu.Lock()
		delete(o.runningTasks, task.ID)
		o.mu.Unlock()
	}()

	task.Status = protocol.TaskStatusRunning
	o.taskStore.Save(task)

	state := &PTESState{
		TaskID:  task.ID,
		Target:  task.Target,
		Params:  task.Params,
		Results: make(map[protocol.TaskType]*schema.ToolResult),
	}

	var handler callbacks.Handler
	if o.graphState != nil {
		handler = o.graphState.BuildCallbackHandler(task.ID)
	}

	var opts []compose.Option
	if handler != nil {
		opts = append(opts, compose.WithCallbacks(handler))
	}

	finalState, err := o.compiledGraph.Invoke(ctx, state, opts...)
	if err != nil {
		task.Status = protocol.TaskStatusFailed
		task.Result = &schema.ToolResult{
			Parts: []schema.ToolOutputPart{
				{Type: schema.ToolPartTypeText, Text: err.Error()},
			},
		}
		o.taskStore.Save(task)
		return err
	}

	// Aggregate results from all phases
	var outputs []map[string]interface{}
	var hasError bool
	for phase, r := range finalState.Results {
		if r == nil {
			continue
		}
		entry := map[string]interface{}{
			"phase": string(phase),
		}
		var textParts []string
		for _, p := range r.Parts {
			if p.Type == schema.ToolPartTypeText {
				textParts = append(textParts, p.Text)
			}
		}
		if len(textParts) > 0 {
			entry["output"] = textParts
		}
		outputs = append(outputs, entry)
	}

	aggJSON, _ := json.Marshal(map[string]interface{}{"phases": outputs})
	task.Result = &schema.ToolResult{
		Parts: []schema.ToolOutputPart{
			{Type: schema.ToolPartTypeText, Text: string(aggJSON)},
		},
	}
	if hasError {
		task.Status = protocol.TaskStatusFailed
	} else {
		task.Status = protocol.TaskStatusCompleted
	}
	o.taskStore.Save(task)

	return nil
}

func (o *Orchestrator) OnTaskResult(taskID string, result *schema.ToolResult) {
	o.taskStore.UpdateResult(taskID, result)

	var taskType protocol.TaskType
	for _, p := range result.Parts {
		if p.Type == schema.ToolPartTypeText && p.Text != "" {
			// try to infer task type from result content
			var data map[string]interface{}
			if err := json.Unmarshal([]byte(p.Text), &data); err == nil {
				if tool, ok := data["tool"].(string); ok {
					switch tool {
					case "nmap":
						taskType = protocol.TaskTypeReconnaissance
					case "nikto":
						taskType = protocol.TaskTypeVulnerabilityScan
					}
				}
			}
			break
		}
	}

	if o.graphState != nil {
		nodeName := string(taskType)
		if nodeName == "" {
			nodeName = "worker"
		}
		state := protocol.GraphNodeStateSuccess
		var output string
		for _, p := range result.Parts {
			if p.Type == schema.ToolPartTypeText {
				output = p.Text
				break
			}
		}
		o.graphState.UpdateNode(taskID, nodeName, state, output, "")
	}
}

func (o *Orchestrator) CancelTask(taskID string) error {
	o.mu.RLock()
	cancel, ok := o.runningTasks[taskID]
	o.mu.RUnlock()
	if !ok {
		return fmt.Errorf("task %s not running", taskID)
	}
	cancel()
	return nil
}

func generateTaskID() string {
	return fmt.Sprintf("task-%d", time.Now().UnixNano())
}
