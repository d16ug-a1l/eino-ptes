// Package remote provides master-side proxies that turn remote worker tools
// into standard eino tool.InvokableTool / tool.EnhancedInvokableTool components.
package remote

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/eino-ptes/pkg/master"
	"github.com/cloudwego/eino-ptes/pkg/protocol"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// RemoteTool implements tool.EnhancedInvokableTool by proxying calls to a
// remote worker via the Scheduler.
type RemoteTool struct {
	name      string
	info      *schema.ToolInfo
	scheduler *master.Scheduler
}

// NewRemoteTool creates a remote tool proxy.
func NewRemoteTool(name string, info *schema.ToolInfo, scheduler *master.Scheduler) *RemoteTool {
	return &RemoteTool{
		name:      name,
		info:      info,
		scheduler: scheduler,
	}
}

// Info returns the tool metadata.
func (t *RemoteTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

// InvokableRun dispatches the tool call to a remote worker and returns the
// structured schema.ToolResult.
func (t *RemoteTool) InvokableRun(ctx context.Context, toolArgument *schema.ToolArgument, opts ...tool.Option) (*schema.ToolResult, error) {
	callID := fmt.Sprintf("call_%d", time.Now().UnixNano())
	tc := schema.ToolCall{
		ID:   callID,
		Type: "function",
		Function: schema.FunctionCall{
			Name:      t.name,
			Arguments: toolArgument.Text,
		},
	}
	msg := schema.AssistantMessage("", []schema.ToolCall{tc})

	resultMsg, err := t.scheduler.DispatchToolCall(ctx, msg)
	if err != nil {
		return nil, fmt.Errorf("remote tool %s: %w", t.name, err)
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

// RemoteToolSet builds a set of RemoteTool instances from registered worker
// ToolInfos.  It deduplicates by tool name.
func RemoteToolSet(scheduler *master.Scheduler, memberMgr *master.MemberManager) []tool.BaseTool {
	seen := make(map[string]bool)
	var tools []tool.BaseTool
	for _, w := range memberMgr.GetWorkers() {
		for _, ti := range w.ToolInfos {
			if seen[ti.Name] {
				continue
			}
			seen[ti.Name] = true
			// copy ToolInfo to avoid mutation
			info := *ti
			tools = append(tools, NewRemoteTool(ti.Name, &info, scheduler))
		}
	}
	return tools
}
