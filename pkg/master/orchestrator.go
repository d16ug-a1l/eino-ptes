package master

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/cloudwego/eino-ptes/pkg/protocol"
	"github.com/cloudwego/eino/callbacks"
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
	scheduler     *Scheduler
	memberMgr     *MemberManager
	graphState    GraphStateManager
	taskStore     *TaskStore
	analyzer      ScanAnalyzer
	planner       Planner
	toolProvider  func() []tool.BaseTool
	toolSet       map[string]tool.EnhancedInvokableTool
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

func NewOrchestrator(scheduler *Scheduler, memberMgr *MemberManager, graphState GraphStateManager, analyzer ScanAnalyzer, planner Planner) *Orchestrator {
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

// buildGraphFromPlan dynamically constructs a Graph based on the task plan.
func (o *Orchestrator) buildGraphFromPlan(plan *TaskPlan) (*compose.Graph[*PTESState, *PTESState], error) {
	g := compose.NewGraph[*PTESState, *PTESState]()

	if len(plan.Phases) == 0 {
		return nil, fmt.Errorf("plan has no phases")
	}

	prevNode := compose.START
	for _, phase := range plan.Phases {
		nodeName := phase.Phase
		taskType := phaseToTaskType(nodeName)
		toolName := phase.Tool
		if toolName == "" {
			toolName = defaultToolForPhase(nodeName)
		}

		// Capture loop variables for closure
		phaseName := nodeName
		phaseTaskType := taskType
		phaseTool := toolName

		if err := g.AddLambdaNode(nodeName, compose.InvokableLambda(func(ctx context.Context, state *PTESState) (*PTESState, error) {
			return o.runPhaseNode(ctx, state, phaseName, phaseTaskType, phaseTool)
		})); err != nil {
			return nil, err
		}

		g.AddEdge(prevNode, nodeName)
		prevNode = nodeName
	}

	g.AddEdge(prevNode, compose.END)
	return g, nil
}

func phaseToTaskType(phase string) protocol.TaskType {
	switch phase {
	case "reconnaissance":
		return protocol.TaskTypeReconnaissance
	case "vulnerability_scan":
		return protocol.TaskTypeVulnerabilityScan
	case "exploitation":
		return protocol.TaskTypeExploitation
	case "post_exploitation":
		return protocol.TaskTypePostExploitation
	case "report_generation":
		return protocol.TaskTypeReportGeneration
	default:
		return protocol.TaskType(phase)
	}
}

func defaultToolForPhase(phase string) string {
	switch phase {
	case "reconnaissance":
		return "nmap"
	case "vulnerability_scan":
		return "nikto"
	default:
		return phase
	}
}

func (o *Orchestrator) reconNode(ctx context.Context, state *PTESState) (*PTESState, error) {
	return o.runPhaseNode(ctx, state, "reconnaissance", protocol.TaskTypeReconnaissance, "nmap")
}

func (o *Orchestrator) vulnScanNode(ctx context.Context, state *PTESState) (*PTESState, error) {
	return o.runPhaseNode(ctx, state, "vulnerability_scan", protocol.TaskTypeVulnerabilityScan, "nikto")
}

// runPhaseNode is a generic phase executor used by both static and dynamic graphs.
func (o *Orchestrator) runPhaseNode(ctx context.Context, state *PTESState, nodeName string, taskType protocol.TaskType, toolName string) (*PTESState, error) {
	if o.graphState != nil {
		o.graphState.UpdateNode(state.TaskID, nodeName, protocol.GraphNodeStateRunning, nil, "")
	}

	task := &protocol.Task{
		ID:     state.TaskID,
		Type:   taskType,
		Target: state.Target,
		Params: state.Params,
	}
	result, err := o.invokeTool(ctx, task, toolName)

	if o.graphState != nil {
		if err != nil {
			o.graphState.UpdateNode(state.TaskID, nodeName, protocol.GraphNodeStateFailed, nil, err.Error())
		} else {
			var output string
			if result != nil {
				for _, p := range result.Parts {
					if p.Type == schema.ToolPartTypeText {
						output = p.Text
						break
					}
				}
			}
			o.graphState.UpdateNode(state.TaskID, nodeName, protocol.GraphNodeStateSuccess, output, "")
		}
	}

	state.Results[taskType] = result

	// LLM analysis with context from previous phases
	if raw := extractTextFromResult(result); raw != "" && o.analyzer != nil {
		ctxAnalyses := make(map[string]*ScanAnalysis)
		for k, v := range state.Analysis {
			ctxAnalyses[k] = v
		}
		if analysis, aerr := o.analyzer.Analyze(ctx, nodeName, raw, ctxAnalyses); aerr == nil {
			if state.Analysis == nil {
				state.Analysis = make(map[string]*ScanAnalysis)
			}
			state.Analysis[nodeName] = analysis
		}
	}

	return state, err
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
		TaskID:   task.ID,
		Target:   task.Target,
		Params:   task.Params,
		Results:  make(map[protocol.TaskType]*schema.ToolResult),
		Analysis: make(map[string]*ScanAnalysis),
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

	agg := map[string]interface{}{
		"phases": outputs,
	}
	if len(finalState.Analysis) > 0 {
		agg["analysis"] = finalState.Analysis
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

// ExecuteTaskWithPlan dynamically builds a graph from the plan and executes it.
func (o *Orchestrator) ExecuteTaskWithPlan(ctx context.Context, task *protocol.Task, plan *TaskPlan) error {
	g, err := o.buildGraphFromPlan(plan)
	if err != nil {
		return fmt.Errorf("build graph from plan: %w", err)
	}

	opts := []compose.GraphCompileOption{
		compose.WithGraphName("ptes-plan-" + task.ID),
	}
	if o.graphState != nil {
		opts = append(opts, compose.WithGraphCompileCallbacks(o.graphState.GraphCompileCallback(task.ID)))
	}

	runnable, err := g.Compile(ctx, opts...)
	if err != nil {
		return fmt.Errorf("compile dynamic graph: %w", err)
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
		TaskID:   task.ID,
		Target:   plan.Target,
		Params:   nil,
		Results:  make(map[protocol.TaskType]*schema.ToolResult),
		Analysis: make(map[string]*ScanAnalysis),
	}

	var handler callbacks.Handler
	if o.graphState != nil {
		handler = o.graphState.BuildCallbackHandler(task.ID)
	}

	var runOpts []compose.Option
	if handler != nil {
		runOpts = append(runOpts, compose.WithCallbacks(handler))
	}

	finalState, err := runnable.Invoke(ctx, state, runOpts...)
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

	agg := map[string]interface{}{
		"phases": outputs,
	}
	if len(finalState.Analysis) > 0 {
		agg["analysis"] = finalState.Analysis
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
