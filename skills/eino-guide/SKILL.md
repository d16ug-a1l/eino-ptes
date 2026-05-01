---
name: eino-guide
description: Eino LLM 应用开发框架通用指南，包含核心概念、架构分层、最佳实践和常见模式。适用于基于 Eino 构建渗透测试编排系统时的架构决策和代码编写。
context: fork
---

# Eino 框架通用指南

## 概述

Eino 是 CloudWeGo 开源的 Go 语言 LLM 应用开发框架，提供组件抽象、编排引擎、流式原语和回调系统。

## 架构四层

1. **schema** — 通用数据类型：`Message`、`StreamReader[T]`、`Document`、`ToolInfo` 等。所有组件间传递的原语。
2. **components** — 接口契约：`BaseChatModel`、`BaseTool`、`Retriever`、`Indexer`、`Embedding`、`ChatTemplate` 等。本仓库定义接口，实现在 `eino-ext`。
3. **compose** — 编排引擎：`Graph[I, O]`、`Chain[I, O]`、`Workflow` 连接组件为可执行流水线。
4. **adk / flow** — 高阶代理运行时和预置模式。`adk` 提供 `Agent`、`Runner` 和中间件；`flow` 提供 ReAct、多 Agent Host 等现成模式。

## 核心概念

### Message
```go
msg := schema.UserMessage("content")
sysMsg := schema.SystemMessage("instruction")
assistantMsg := schema.AssistantMessage("content", toolCalls)
toolMsg := schema.ToolMessage("result", toolCallID)
```

### Tool 定义
```go
type BaseTool interface {
    Info(ctx context.Context) (*schema.ToolInfo, error)
}

type EnhancedInvokableTool interface {
    BaseTool
    InvokableRun(ctx context.Context, toolArgument *schema.ToolArgument, opts ...tool.Option) (*schema.ToolResult, error)
}
```

### Streaming
```go
sr, sw := schema.Pipe[T](capacity)
sw.Send(chunk, err)
sw.Close()
chunk, err := sr.Recv()
```

## 最佳实践

- 组件只实现有意义的流式范式（`Invoke`、`Stream`、`Collect`、`Transform`），框架自动桥接
- 在系统边界验证输入，内部信任框架保证
- 使用 `callbacks.Handler` 注入可观测性，而非在业务代码中打日志
- 长任务使用 `compose.WithCheckPointStore` 实现断点续传
- 图编译时通过 `WithGraphCompileCallbacks` 获取执行状态

## 常见问题

**Q: 如何选择 Graph / Chain / Workflow？**
- Graph: 复杂有向图，支持分支和循环
- Chain: 线性流水线，Builder 模式
- Workflow: 显式声明依赖的 DAG

**Q: 组件接口变更如何处理？**
- `compose` 包通过 `component_to_graph_node.go` 桥接组件到图节点
- 修改组件接口通常需要同步修改 compose 包装层
