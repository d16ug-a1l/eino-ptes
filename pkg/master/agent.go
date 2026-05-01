package master

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/planexecute"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/cloudwego/eino-ptes/pkg/protocol"
)

// PTESPlan implements planexecute.Plan for penetration testing workflows.
type PTESPlan struct {
	Target string   `json:"target"`
	Steps  []string `json:"steps"`
}

func (p *PTESPlan) FirstStep() string {
	if len(p.Steps) == 0 {
		return ""
	}
	return p.Steps[0]
}

func (p *PTESPlan) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Target string   `json:"target"`
		Steps  []string `json:"steps"`
	}{Target: p.Target, Steps: p.Steps})
}

func (p *PTESPlan) UnmarshalJSON(data []byte) error {
	var raw struct {
		Target string   `json:"target"`
		Steps  []string `json:"steps"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	p.Target = raw.Target
	p.Steps = raw.Steps
	return nil
}

// NewPTESPlan creates a new empty PTESPlan.
func NewPTESPlan(_ context.Context) planexecute.Plan {
	return &PTESPlan{}
}

// toolNameToPhase maps tool names to PTES phase names.
func toolNameToPhase(name string) string {
	switch name {
	case "nmap":
		return "reconnaissance"
	case "nikto":
		return "vulnerability_scan"
	default:
		return name
	}
}

// ptesToolInjectorMiddleware injects the current tool set into the agent context
// and tracks tool execution in the graph state.
type ptesToolInjectorMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	orch *Orchestrator
}

func newPTESToolInjectorMiddleware(orch *Orchestrator) *ptesToolInjectorMiddleware {
	return &ptesToolInjectorMiddleware{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		orch:                         orch,
	}
}

func (m *ptesToolInjectorMiddleware) BeforeAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext) (context.Context, *adk.ChatModelAgentContext, error) {
	if m.orch.toolProvider != nil {
		tools := m.orch.toolProvider()
		runCtx.Tools = append(runCtx.Tools, tools...)
	}
	return ctx, runCtx, nil
}

// graphStateToolMiddleware wraps enhanced tool calls to update graph state and
// perform LLM analysis on results.
func newGraphStateToolMiddleware(orch *Orchestrator) compose.EnhancedInvokableToolMiddleware {
	return func(next compose.EnhancedInvokableToolEndpoint) compose.EnhancedInvokableToolEndpoint {
		return func(ctx context.Context, input *compose.ToolInput) (*compose.EnhancedInvokableToolOutput, error) {
			taskIDVal, ok := adk.GetSessionValue(ctx, "taskID")
			if !ok {
				return next(ctx, input)
			}
			taskID := taskIDVal.(string)
			phase := toolNameToPhase(input.Name)

			if orch.graphState != nil {
				orch.graphState.UpdateNode(taskID, phase, protocol.GraphNodeStateRunning, nil, "")
			}

			output, err := next(ctx, input)

			if orch.graphState != nil {
				if err != nil {
					orch.graphState.UpdateNode(taskID, phase, protocol.GraphNodeStateFailed, nil, err.Error())
				} else {
					var outStr string
					if output != nil && output.Result != nil {
						for _, p := range output.Result.Parts {
							if p.Type == schema.ToolPartTypeText {
								outStr = p.Text
								break
							}
						}
					}
					orch.graphState.UpdateNode(taskID, phase, protocol.GraphNodeStateSuccess, outStr, "")
				}
			}

			// LLM analysis with cross-phase context
			if err == nil && output != nil && output.Result != nil && orch.analyzer != nil {
				raw := extractTextFromToolResult(output.Result)
				if raw != "" {
					var analyses map[string]*ScanAnalysis
					if val, ok := adk.GetSessionValue(ctx, "analyses"); ok {
						analyses = val.(map[string]*ScanAnalysis)
					} else {
						analyses = make(map[string]*ScanAnalysis)
					}
					if analysis, aerr := orch.analyzer.Analyze(ctx, phase, raw, analyses); aerr == nil {
						analyses[phase] = analysis
						adk.AddSessionValue(ctx, "analyses", analyses)
					}
				}
			}

			return output, err
		}
	}
}

func extractTextFromToolResult(result *schema.ToolResult) string {
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

// formatMessages concatenates message contents into a single string.
func formatMessages(msgs []adk.Message) string {
	var sb strings.Builder
	for _, m := range msgs {
		if m != nil {
			sb.WriteString(m.Content)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// formatExecutedSteps formats executed steps for prompt inclusion.
func formatExecutedSteps(steps []planexecute.ExecutedStep) string {
	var sb strings.Builder
	for _, s := range steps {
		sb.WriteString(fmt.Sprintf("Step: %s\nResult: %s\n\n", s.Step, s.Result))
	}
	return sb.String()
}

// NewPTESExecutor creates a custom executor agent for PTES tasks with graph state tracking.
func NewPTESExecutor(ctx context.Context, m model.BaseChatModel, orch *Orchestrator) (adk.Agent, error) {
	genInput := func(ctx context.Context, instruction string, _ *adk.AgentInput) ([]adk.Message, error) {
		planVal, ok := adk.GetSessionValue(ctx, planexecute.PlanSessionKey)
		if !ok {
			return nil, fmt.Errorf("plan not found in session")
		}
		plan := planVal.(planexecute.Plan)

		userInputVal, ok := adk.GetSessionValue(ctx, planexecute.UserInputSessionKey)
		if !ok {
			return nil, fmt.Errorf("user input not found in session")
		}
		userInput := userInputVal.([]adk.Message)

		var executedSteps []planexecute.ExecutedStep
		if stepsVal, ok := adk.GetSessionValue(ctx, planexecute.ExecutedStepsSessionKey); ok {
			executedSteps = stepsVal.([]planexecute.ExecutedStep)
		}

		planJSON, _ := plan.MarshalJSON()

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("## OBJECTIVE\n%s\n\n", formatMessages(userInput)))
		sb.WriteString(fmt.Sprintf("## PLAN\n%s\n\n", string(planJSON)))

		if len(executedSteps) > 0 {
			sb.WriteString("## COMPLETED STEPS & RESULTS\n")
			sb.WriteString(formatExecutedSteps(executedSteps))
		}

		sb.WriteString(fmt.Sprintf("## YOUR TASK\nExecute the following step using the available security tools:\n%s\n\n", plan.FirstStep()))
		sb.WriteString("Call the appropriate tool with the correct parameters. Return a concise summary of the results.")

		return []*schema.Message{
			schema.SystemMessage("You are a penetration testing executor. Execute each step using available security tools (nmap, nikto, etc.). Be precise with tool parameters."),
			schema.UserMessage(sb.String()),
		}, nil
	}

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "ptes-executor",
		Description: "PTES task executor",
		Model:       m,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: []tool.BaseTool{}, // injected dynamically by middleware
				ToolCallMiddlewares: []compose.ToolMiddleware{
					{
						EnhancedInvokable: newGraphStateToolMiddleware(orch),
					},
				},
			},
		},
		GenModelInput: genInput,
		MaxIterations: 10,
		OutputKey:     planexecute.ExecutedStepSessionKey,
		Handlers:      []adk.ChatModelAgentMiddleware{newPTESToolInjectorMiddleware(orch)},
	})
	if err != nil {
		return nil, fmt.Errorf("create chat model agent: %w", err)
	}
	return agent, nil
}

// BuildPlanExecuteAgent creates a plan-execute-replan agent configured for PTES workflows.
func BuildPlanExecuteAgent(ctx context.Context, chatModel model.ToolCallingChatModel, orch *Orchestrator) (adk.ResumableAgent, error) {
	planner, err := planexecute.NewPlanner(ctx, &planexecute.PlannerConfig{
		ToolCallingChatModel: chatModel,
		NewPlan:              NewPTESPlan,
	})
	if err != nil {
		return nil, fmt.Errorf("create planner: %w", err)
	}

	executor, err := NewPTESExecutor(ctx, chatModel, orch)
	if err != nil {
		return nil, fmt.Errorf("create executor: %w", err)
	}

	replanner, err := planexecute.NewReplanner(ctx, &planexecute.ReplannerConfig{
		ChatModel: chatModel,
		NewPlan:   NewPTESPlan,
	})
	if err != nil {
		return nil, fmt.Errorf("create replanner: %w", err)
	}

	return planexecute.New(ctx, &planexecute.Config{
		Planner:       planner,
		Executor:      executor,
		Replanner:     replanner,
		MaxIterations: 10,
	})
}
