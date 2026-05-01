---
name: eino-agent
description: Eino ADK（Agent Development Kit）使用指南，涵盖 ChatModelAgent、Runner、Middleware、Skill 系统、预置 Agent 模式（Plan-Execute、Supervisor、Deep Agent）和 Agent-as-Tool 模式。
context: fork
---

# Eino ADK Agent 开发指南

## ChatModelAgent

### 基础配置
```go
import "github.com/cloudwego/eino/adk"

agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
    Name:        "ptes-agent",
    Description: "渗透测试任务规划与执行代理",
    Instruction: "你是一个渗透测试专家...",
    Model:       model, // 必须支持 tool calling
    ToolsConfig: &compose.ToolsNodeConfig{
        Tools: []tool.BaseTool{nmapTool, niktoTool},
    },
    MaxIterations: 20,
})
```

### 运行
```go
input := &adk.AgentInput{
    Messages: []adk.Message{schema.UserMessage("扫描 192.168.1.1")},
}
iter := agent.Run(ctx, input)
for {
    event, ok := iter.Next()
    if !ok { break }
    if event.Err != nil { log.Fatal(event.Err) }
    if event.Output != nil && event.Output.MessageOutput != nil {
        msg, _ := event.Output.MessageOutput.GetMessage()
        fmt.Println(msg.Content)
    }
}
```

## Runner

### 生命周期管理
```go
runner, err := adk.NewRunner(ctx, &adk.RunnerConfig{
    Agent:            agent,
    EnableStreaming:  true,
    CheckPointStore:  store,
})

// 运行
result, err := runner.Run(ctx, input)

// 查询状态
result, err := runner.Query(ctx, runID)

// 恢复
result, err := runner.Resume(ctx, runID)
```

## Middleware

### ChatModelAgentMiddleware（推荐）
```go
type ChatModelAgentMiddleware interface {
    BeforeAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext) (context.Context, *adk.ChatModelAgentContext, error)
    BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState) (context.Context, *adk.ChatModelAgentState, error)
    AfterModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState) (context.Context, *adk.ChatModelAgentState, error)
    WrapInvokableToolCall(ctx context.Context, tool tool.InvokableTool, tc *adk.ToolContext) (tool.InvokableTool, error)
    WrapModel(ctx context.Context, m model.BaseChatModel, mc *adk.ModelContext) (model.BaseChatModel, error)
}
```

### Skill Middleware
```go
import "github.com/cloudwego/eino/adk/middlewares/skill"

backend := skill.NewBackendFromFilesystem(&skill.FilesystemConfig{BaseDir: "./skills"})

skillHandler, err := skill.NewMiddleware(ctx, &skill.Config{
    Backend:  backend,
    AgentHub: myAgentHub,
    ModelHub: myModelHub,
})

agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
    // ...
    Handlers: []adk.ChatModelAgentMiddleware{skillHandler},
})
```

## 预置 Agent 模式

### Plan-Execute
```go
import "github.com/cloudwego/eino/adk/prebuilt/planexecute"

agent, err := planexecute.New(ctx, &planexecute.Config{
    Planner: &planexecute.PlannerConfig{
        ChatModel: model,
        Prompt:    "将用户目标分解为渗透测试阶段",
    },
    Executor: &planexecute.ExecutorConfig{
        Model:         model,
        ToolsConfig:   &compose.ToolsNodeConfig{Tools: tools},
        MaxIterations: 10,
    },
    Replanner: &planexecute.ReplannerConfig{
        ChatModel: model,
    },
    MaxIterations: 5,
})
```

### Supervisor
```go
import "github.com/cloudwego/eino/adk/prebuilt/supervisor"

agent, err := supervisor.New(ctx, &supervisor.Config{
    Supervisor: supervisorAgent,
    SubAgents:  []adk.Agent{reconAgent, vulnAgent, exploitAgent},
})
```

### Deep Agent
```go
import "github.com/cloudwego/eino/adk/prebuilt/deep"

agent, err := deep.New(ctx, &deep.Config{
    Name:          "deep-ptes",
    ChatModel:     model,
    Instruction:   "深度渗透测试代理",
    SubAgents:     []adk.Agent{...},
    MaxIteration:  50,
})
```

## Agent as Tool

```go
// 将 Agent 包装为 Tool
agentTool, err := adk.NewAgentTool(ctx, agent)

// 在主 Agent 中使用
mainAgent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
    ToolsConfig: &compose.ToolsNodeConfig{
        Tools: []tool.BaseTool{agentTool, nmapTool},
    },
})
```

## 子 Agent 与上下文

```go
// 设置子 Agent
adk.SetSubAgents(parentAgent, subAgent1, subAgent2)

// 上下文模式
// fork: 子 Agent 无父历史
// fork_with_context: 子 Agent 携带父历史
```
