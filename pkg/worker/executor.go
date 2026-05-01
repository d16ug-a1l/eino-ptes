package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/cloudwego/eino-ptes/pkg/protocol"
	"github.com/cloudwego/eino-ptes/pkg/worker/tools"
)

type Executor struct {
	capabilities []string
	tools        map[string]tool.EnhancedInvokableTool
	cancelMu     sync.RWMutex
	cancels      map[string]context.CancelFunc
}

func NewExecutor(capabilities []string) *Executor {
	e := &Executor{
		capabilities: capabilities,
		tools:        make(map[string]tool.EnhancedInvokableTool),
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

func (e *Executor) Execute(ctx context.Context, task protocol.Task) (*schema.ToolResult, error) {
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

	var toolName string
	switch task.Type {
	case protocol.TaskTypeReconnaissance:
		toolName = "nmap"
	case protocol.TaskTypeVulnerabilityScan:
		toolName = "nikto"
	default:
		return nil, fmt.Errorf("unsupported task type: %s", task.Type)
	}

	t, ok := e.tools[toolName]
	if !ok {
		return nil, fmt.Errorf("tool %s not available", toolName)
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
		return nil, fmt.Errorf("marshal args: %w", err)
	}

	result, err := t.InvokableRun(taskCtx, &schema.ToolArgument{Text: string(argsJSON)})
	if err != nil {
		return result, err
	}

	return result, nil
}

func (e *Executor) Cancel(taskID string) {
	e.cancelMu.RLock()
	cancel, ok := e.cancels[taskID]
	e.cancelMu.RUnlock()
	if ok {
		cancel()
	}
}

func (e *Executor) GetToolInfos(ctx context.Context) []*schema.ToolInfo {
	var infos []*schema.ToolInfo
	for _, t := range e.tools {
		info, err := t.Info(ctx)
		if err != nil {
			continue
		}
		infos = append(infos, info)
	}
	return infos
}
