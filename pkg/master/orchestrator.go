package master

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/cloudwego/eino-ptes/pkg/protocol"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// PTESState is the state object passed through the Eino Graph nodes.
type PTESState struct {
	TaskID    string
	Target    string
	Params    map[string]interface{}
	Results   map[protocol.TaskType]*schema.ToolResult
	Analysis  map[string]*ScanAnalysis
}

type Orchestrator struct {
	scheduler    *Scheduler
	memberMgr    *MemberManager
	graphState   GraphStateManager
	taskStore    *TaskStore
	analyzer     ScanAnalyzer
	planner      Planner
	chatModel    model.ToolCallingChatModel
	agent        adk.Agent
	toolProvider func() []tool.BaseTool
	toolSet      map[string]tool.EnhancedInvokableTool
	mu           sync.RWMutex
	runningTasks map[string]context.CancelFunc
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

func NewOrchestrator(scheduler *Scheduler, memberMgr *MemberManager, graphState GraphStateManager, analyzer ScanAnalyzer, planner Planner, chatModel model.ToolCallingChatModel) *Orchestrator {
	if analyzer == nil {
		analyzer = NoopScanAnalyzer{}
	}
	o := &Orchestrator{
		scheduler:    scheduler,
		memberMgr:    memberMgr,
		graphState:   graphState,
		taskStore:    NewTaskStore(),
		analyzer:     analyzer,
		planner:      planner,
		chatModel:    chatModel,
		toolSet:      make(map[string]tool.EnhancedInvokableTool),
		runningTasks: make(map[string]context.CancelFunc),
	}
	return o
}

func (o *Orchestrator) SetToolProvider(fn func() []tool.BaseTool) {
	o.toolProvider = fn
}

func (o *Orchestrator) RefreshToolSet() {
	if o.toolProvider == nil {
		return
	}
	tools := o.toolProvider()
	o.mu.Lock()
	defer o.mu.Unlock()
	o.toolSet = make(map[string]tool.EnhancedInvokableTool, len(tools))
	for _, t := range tools {
		info, err := t.Info(context.Background())
		if err != nil {
			continue
		}
		if et, ok := t.(tool.EnhancedInvokableTool); ok {
			o.toolSet[info.Name] = et
		}
	}
}

func (o *Orchestrator) InitAgent(ctx context.Context) error {
	if o.chatModel == nil {
		return fmt.Errorf("chat model not configured")
	}
	agent, err := BuildPlanExecuteAgent(ctx, o.chatModel, o)
	if err != nil {
		return fmt.Errorf("build plan execute agent: %w", err)
	}
	o.agent = agent
	return nil
}

func (o *Orchestrator) invokeTool(ctx context.Context, task *protocol.Task, toolName string) (*schema.ToolResult, error) {
	o.mu.RLock()
	t, ok := o.toolSet[toolName]
	o.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("tool %s not available in tool set", toolName)
	}

	args := map[string]interface{}{
		"target": task.Target,
	}
	if task.Params != nil {
		for k, v := range task.Params {
			args[k] = v
		}
	}
	argsJSON, _ := json.Marshal(args)

	return t.InvokableRun(ctx, &schema.ToolArgument{Text: string(argsJSON)})
}

func (o *Orchestrator) PlanTask(ctx context.Context, description string) (*TaskPlan, error) {
	if o.planner == nil {
		return nil, fmt.Errorf("planner not configured")
	}
	return o.planner.Plan(ctx, description)
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

// RunTask executes a task using the ADK plan-execute-replan agent.
func (o *Orchestrator) RunTask(ctx context.Context, task *protocol.Task, input string) error {
	if o.agent == nil {
		if err := o.InitAgent(ctx); err != nil {
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

	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent: o.agent,
	})

	var handler callbacks.Handler
	if o.graphState != nil {
		handler = o.graphState.BuildCallbackHandler(task.ID)
	}

	var opts []adk.AgentRunOption
	if handler != nil {
		opts = append(opts, adk.WithCallbacks(handler))
	}
	opts = append(opts, adk.WithSessionValues(map[string]any{
		"taskID": task.ID,
	}))

	iter := runner.Query(ctx, input, opts...)

	var finalResponse string
	var hasError bool
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			hasError = true
			finalResponse = event.Err.Error()
			break
		}
		if event.Output != nil && event.Output.MessageOutput != nil {
			msg, _ := event.Output.MessageOutput.GetMessage()
			if msg != nil && msg.Content != "" {
				finalResponse = msg.Content
			}
		}
	}

	// Gather analyses from session (set by tool middleware)
	var analyses map[string]*ScanAnalysis
	if val, ok := adk.GetSessionValue(ctx, "analyses"); ok {
		analyses = val.(map[string]*ScanAnalysis)
	}

	agg := map[string]interface{}{
		"response": finalResponse,
	}
	if len(analyses) > 0 {
		agg["analysis"] = analyses
	}
	aggJSON, _ := json.Marshal(agg)
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

func (o *Orchestrator) GetTaskGraph(taskID string) *TaskGraph {
	if gs, ok := o.graphState.(*GraphState); ok {
		return gs.GetTaskGraph(taskID)
	}
	return nil
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

func extractTextFromResult(result *schema.ToolResult) string {
	if result == nil {
		return ""
	}
	for _, p := range result.Parts {
		if p.Type == schema.ToolPartTypeText {
			return p.Text
		}
	}
	return ""
}
