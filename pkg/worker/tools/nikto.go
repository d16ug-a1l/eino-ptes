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

// NiktoTool implements Eino tool.EnhancedInvokableTool for web vulnerability scanning.
type NiktoTool struct{}

func NewNiktoTool() *NiktoTool {
	return &NiktoTool{}
}

func (t *NiktoTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "nikto",
		Desc: "Web server vulnerability scanner. Tests for dangerous files, outdated versions, and configuration issues.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"target": {
				Type:     schema.String,
				Desc:     "Target URL to scan (e.g. http://192.168.1.1)",
				Required: true,
			},
			"flags": {
				Type: schema.String,
				Desc: "Optional nikto flags",
			},
		}),
	}, nil
}

func (t *NiktoTool) InvokableRun(ctx context.Context, toolArgument *schema.ToolArgument, opts ...tool.Option) (*schema.ToolResult, error) {
	var args struct {
		Target string `json:"target"`
		Flags  string `json:"flags,omitempty"`
	}
	if err := json.Unmarshal([]byte(toolArgument.Text), &args); err != nil {
		return nil, fmt.Errorf("parse nikto args: %w", err)
	}

	if args.Target == "" {
		return nil, fmt.Errorf("nikto: target is required")
	}

	var cmdArgs []string
	cmdArgs = append(cmdArgs, "-h", args.Target)
	if args.Flags != "" {
		cmdArgs = append(cmdArgs, strings.Fields(args.Flags)...)
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "nikto", cmdArgs...)
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
			return result, fmt.Errorf("nikto: timeout after 10 minutes")
		}
		return result, fmt.Errorf("nikto: %w", err)
	}
	return result, nil
}
