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

// NmapTool implements Eino tool.InvokableTool for network scanning.
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

func (t *NmapTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	var args struct {
		Target string `json:"target"`
		Flags  string `json:"flags,omitempty"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("parse nmap args: %w", err)
	}

	if args.Target == "" {
		return "", fmt.Errorf("nmap: target is required")
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
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return string(output), fmt.Errorf("nmap: timeout after 5 minutes")
		}
		return string(output), fmt.Errorf("nmap: %w", err)
	}

	result := map[string]interface{}{
		"tool":   "nmap",
		"target": args.Target,
		"output": string(output),
	}
	out, _ := json.Marshal(result)
	return string(out), nil
}
