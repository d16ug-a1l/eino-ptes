package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino-ptes/pkg/protocol"
	"github.com/cloudwego/eino-ptes/pkg/worker/tools"
)

type Executor struct {
	capabilities []string
	tools        map[string]tool.InvokableTool
	cancelMu     sync.RWMutex
	cancels      map[string]context.CancelFunc
}

func NewExecutor(capabilities []string) *Executor {
	e := &Executor{
		capabilities: capabilities,
		tools:        make(map[string]tool.InvokableTool),
		cancels:      make(map[string]context.CancelFunc),
	}

	for _, cap := range capabilities {
		switch cap {
		case "nmap":
			e.tools["nmap"] = tools.NewNmapTool()
		case "nikto":
			e.tools["nikto"] = tools.NewNiktoTool()
		case "dirb":
			e.tools["dirb"] = tools.NewDirbTool()
		}
	}

	return e
}

func (e *Executor) Execute(ctx context.Context, task protocol.Task) (*protocol.TaskResult, error) {
	taskCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	e.cancelMu.Lock()
	e.cancels[task.ID] = cancel
	e.cancelMu.Unlock()
	defer func() {
		e.cancelMu.Lock()
		delete(e.cancels, task.ID)
		e.cancelMu.Unlock()
	}()

	switch task.Type {
	case protocol.TaskTypeReconnaissance:
		return e.executeReconnaissance(taskCtx, task)
	case protocol.TaskTypeVulnerabilityScan:
		return e.executeVulnerabilityScan(taskCtx, task)
	default:
		return nil, fmt.Errorf("unsupported task type: %s", task.Type)
	}
}

func (e *Executor) executeReconnaissance(ctx context.Context, task protocol.Task) (*protocol.TaskResult, error) {
	t, ok := e.tools["nmap"]
	if !ok {
		return nil, fmt.Errorf("nmap tool not available")
	}

	args := map[string]interface{}{
		"target": task.Target,
	}
	if task.Params != nil {
		for k, v := range task.Params {
			args[k] = v
		}
	}

	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("marshal nmap args: %w", err)
	}

	output, err := t.InvokableRun(ctx, string(argsJSON))
	if err != nil {
		return nil, err
	}

	return &protocol.TaskResult{
		Output: output,
		Artifacts: map[string]interface{}{
			"tool":   "nmap",
			"target": task.Target,
		},
	}, nil
}

func (e *Executor) executeVulnerabilityScan(ctx context.Context, task protocol.Task) (*protocol.TaskResult, error) {
	t, ok := e.tools["nikto"]
	if !ok {
		return nil, fmt.Errorf("nikto tool not available")
	}

	args := map[string]interface{}{
		"target": task.Target,
	}
	if task.Params != nil {
		for k, v := range task.Params {
			args[k] = v
		}
	}

	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("marshal nikto args: %w", err)
	}

	output, err := t.InvokableRun(ctx, string(argsJSON))
	if err != nil {
		return nil, err
	}

	return &protocol.TaskResult{
		Output: output,
		Artifacts: map[string]interface{}{
			"tool":   "nikto",
			"target": task.Target,
		},
	}, nil
}

func (e *Executor) Cancel(taskID string) {
	e.cancelMu.RLock()
	cancel, ok := e.cancels[taskID]
	e.cancelMu.RUnlock()
	if ok {
		cancel()
	}
}
