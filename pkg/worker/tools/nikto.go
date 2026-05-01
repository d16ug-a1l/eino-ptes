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

// NiktoTool implements Eino tool.InvokableTool for web vulnerability scanning.
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

func (t *NiktoTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	var args struct {
		Target string `json:"target"`
		Flags  string `json:"flags,omitempty"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("parse nikto args: %w", err)
	}

	if args.Target == "" {
		return "", fmt.Errorf("nikto: target is required")
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
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return string(output), fmt.Errorf("nikto: timeout after 10 minutes")
		}
		return string(output), fmt.Errorf("nikto: %w", err)
	}

	result := map[string]interface{}{
		"tool":   "nikto",
		"target": args.Target,
		"output": string(output),
	}
	out, _ := json.Marshal(result)
	return string(out), nil
}
