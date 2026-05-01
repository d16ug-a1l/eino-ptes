---
name: eino-compose
description: Eino 编排引擎使用指南，涵盖 Graph、Chain、Workflow、ToolsNode、Lambda、Branch、State、Checkpointing 等核心编排能力的详细用法和代码示例。
context: fork
---

# Eino Compose 编排引擎

## Graph

### 创建与编译
```go
g := compose.NewGraph[*PTESState, *PTESState]()

// 添加节点
g.AddLambdaNode("recon", compose.InvokableLambda(reconFunc))
g.AddChatModelNode("model", chatModel)
g.AddToolsNode("tools", toolsNode)
g.AddGraphNode("subgraph", subGraph)

// 添加边
g.AddEdge(compose.START, "recon")
g.AddEdge("recon", "tools")
g.AddEdge("tools", compose.END)

// 添加分支
branch := compose.NewGraphBranch[*PTESState](conditionFunc, endNodes)
g.AddBranch("recon", branch)

// 编译
runnable, err := g.Compile(ctx,
    compose.WithGraphName("ptes-pipeline"),
    compose.WithCheckPointStore(store),
    compose.WithGraphCompileCallbacks(callback),
)
```

### 状态共享
```go
g := compose.NewGraph[*PTESState, *PTESState](
    compose.WithGenLocalState(func(ctx context.Context) *PTESState {
        return &PTESState{Results: make(map[protocol.TaskType]*schema.ToolResult)}
    }),
)
```

## Chain

### 线性流水线
```go
chain := compose.NewChain[[]*schema.Message, *schema.Message]()
chain.AppendChatTemplate(template)
chain.AppendChatModel(model)
chain.AppendToolsNode(toolsNode)

runnable, err := chain.Compile(ctx)
result, err := runnable.Invoke(ctx, messages)
```

## Workflow

### 显式依赖 DAG
```go
wf := compose.NewWorkflow[string, string]()
node1 := wf.AddChatModelNode("model", chatModel)
node2 := wf.AddToolsNode("tools", toolsNode)
node3 := wf.AddLambdaNode("post", compose.InvokableLambda(postFunc))

node2.AddInput("model")
node3.AddInput("tools")

runnable, err := wf.Compile(ctx)
```

## ToolsNode

### 创建工具节点
```go
toolsNode, err := compose.NewToolNode(ctx, &compose.ToolsNodeConfig{
    Tools: []tool.BaseTool{nmapTool, niktoTool},
    ToolCallMiddlewares: []compose.EnhancedInvokableToolMiddleware{
        loggingMiddleware,
    },
})
```

### 未知工具处理
```go
cfg := &compose.ToolsNodeConfig{
    UnknownToolsHandler: func(ctx context.Context, name, input string) (string, error) {
        return "", fmt.Errorf("unknown tool: %s", name)
    },
}
```

## Lambda

```go
// 四种 Lambda 类型
compose.InvokableLambda(func(ctx context.Context, input I) (O, error))
compose.StreamableLambda(func(ctx context.Context, input I) (*schema.StreamReader[O], error))
compose.CollectableLambda(func(ctx context.Context, input *schema.StreamReader[I]) (O, error))
compose.TransformableLambda(func(ctx context.Context, input *schema.StreamReader[I]) (*schema.StreamReader[O], error))
```

## Branch

```go
// 单条件分支
branch := compose.NewGraphBranch[*PTESState](
    func(ctx context.Context, state *PTESState) (string, error) {
        if state.Results[protocol.TaskTypeReconnaissance] != nil {
            return "vulnerability_scan", nil
        }
        return compose.END, nil
    },
    map[string]bool{"vulnerability_scan": true, compose.END: true},
)
```

## Checkpointing

```go
// 配置 checkpoint 存储
runnable, err := g.Compile(ctx, compose.WithCheckPointStore(store))

// 执行时指定 checkpoint ID
result, err := runnable.Invoke(ctx, state, compose.WithCheckPointID("task-123"))
```

## Interrupt / Resume

```go
// 在节点前中断，等待人工输入
compose.Interrupt(ctx, &compose.InterruptInfo{
    Name: "approval",
    Desc: "等待管理员审批",
})

// 恢复执行
compose.Resume(ctx, interruptID)
compose.ResumeWithData(ctx, interruptID, data)
```

## 回调

```go
handler := callbacks.NewHandlerBuilder().
    OnStartFn(func(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
        fmt.Printf("Node %s started\n", info.Name)
        return ctx
    }).
    OnEndFn(func(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
        fmt.Printf("Node %s ended\n", info.Name)
        return ctx
    }).
    Build()

result, err := runnable.Invoke(ctx, state, compose.WithCallbacks(handler))
```
