package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// NmapTool implements Eino tool.EnhancedInvokableTool for network scanning.
type NmapTool struct{}

func NewNmapTool() *NmapTool {
	return &NmapTool{}
}

func (t *NmapTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "nmap",
		Desc: "Network discovery and security auditing tool. Scans ports and services on a target host.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"target": {
				Type:     schema.String,
				Desc:     "Target IP address or hostname to scan",
				Required: true,
			},
			"flags": {
				Type: schema.String,
				Desc: "Optional nmap flags (e.g. '-sV -sC', '-sP -n')",
			},
		}),
	}, nil
}

func (t *NmapTool) InvokableRun(ctx context.Context, toolArgument *schema.ToolArgument, opts ...tool.Option) (*schema.ToolResult, error) {
	var args struct {
		Target string `json:"target"`
		Flags  string `json:"flags,omitempty"`
	}
	if err := json.Unmarshal([]byte(toolArgument.Text), &args); err != nil {
		return nil, fmt.Errorf("parse nmap args: %w", err)
	}

	if args.Target == "" {
		return nil, fmt.Errorf("nmap: target is required")
	}

	flags := "-sV -sC"
	if args.Flags != "" {
		flags = args.Flags
	}

	flagParts := strings.Fields(flags)
	cmdArgs := append(flagParts, args.Target)

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "nmap", cmdArgs...)
	output, err := cmd.CombinedOutput()

	result := &schema.ToolResult{
		Parts: []schema.ToolOutputPart{
			{
				Type: schema.ToolPartTypeText,
				Text: string(output),
			},
		},
	}
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return result, fmt.Errorf("nmap: timeout after 5 minutes")
		}
		return result, fmt.Errorf("nmap: %w", err)
	}
	return result, nil
}
