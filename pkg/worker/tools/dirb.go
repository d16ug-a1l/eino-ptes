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

// DirbTool implements Eino tool.InvokableTool for web content scanning.
type DirbTool struct{}

func NewDirbTool() *DirbTool {
	return &DirbTool{}
}

func (t *DirbTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "dirb",
		Desc: "Web content scanner. Brute-forces directories and files on a web server.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"target": {
				Type:     schema.String,
				Desc:     "Target URL to scan (e.g. http://192.168.1.1)",
				Required: true,
			},
			"wordlist": {
				Type: schema.String,
				Desc: "Path to wordlist file (default: /usr/share/dirb/wordlists/common.txt)",
			},
			"flags": {
				Type: schema.String,
				Desc: "Optional dirb flags",
			},
		}),
	}, nil
}

func (t *DirbTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	var args struct {
		Target   string `json:"target"`
		Wordlist string `json:"wordlist,omitempty"`
		Flags    string `json:"flags,omitempty"`
	}
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("parse dirb args: %w", err)
	}

	if args.Target == "" {
		return "", fmt.Errorf("dirb: target is required")
	}

	wordlist := "/usr/share/dirb/wordlists/common.txt"
	if args.Wordlist != "" {
		wordlist = args.Wordlist
	}

	var cmdArgs []string
	cmdArgs = append(cmdArgs, args.Target, wordlist)
	if args.Flags != "" {
		cmdArgs = append(cmdArgs, strings.Fields(args.Flags)...)
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "dirb", cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return string(output), fmt.Errorf("dirb: timeout after 10 minutes")
		}
		return string(output), fmt.Errorf("dirb: %w", err)
	}

	result := map[string]interface{}{
		"tool":   "dirb",
		"target": args.Target,
		"output": string(output),
	}
	out, _ := json.Marshal(result)
	return string(out), nil
}
