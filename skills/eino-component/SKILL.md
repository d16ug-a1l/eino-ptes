---
name: eino-component
description: Eino 组件层接口规范与使用指南，涵盖 Model、Tool、Embedding、Retriever、Indexer、ChatTemplate 等组件的定义、实现和集成方式。
context: fork
---

# Eino 组件层

## Model 组件

### BaseChatModel
```go
type BaseChatModel interface {
    Generate(ctx context.Context, input []*schema.Message, opts ...Option) (*schema.Message, error)
}
```

### ToolCallingChatModel
```go
type ToolCallingChatModel interface {
    BaseChatModel
    WithTools(tools []*schema.ToolInfo) (BaseChatModel, error)
    BindTools(tools []*schema.ToolInfo) error
}
```

### 官方实现
- OpenAI: `github.com/cloudwego/eino-ext/components/model/openai`
- Claude: `github.com/cloudwego/eino-ext/components/model/claude`
- Ollama: `github.com/cloudwego/eino-ext/components/model/ollama`

### 配置示例
```go
import openaiModel "github.com/cloudwego/eino-ext/components/model/openai"

cfg := &openaiModel.ChatModelConfig{
    APIKey:  apiKey,
    BaseURL: baseURL,
    Model:   modelName,
    Timeout: 30,
}
cm, err := openaiModel.NewChatModel(ctx, cfg)
```

## Tool 组件

### 接口层次
```go
BaseTool → Info()
InvokableTool → InvokableRun(ctx, argumentsJSON, opts) (string, error)
StreamableTool → StreamableRun(ctx, argumentsJSON, opts) (*StreamReader[string], error)
EnhancedInvokableTool → InvokableRun(ctx, *ToolArgument, opts) (*ToolResult, error)
EnhancedStreamableTool → StreamableRun(ctx, *ToolArgument, opts) (*StreamReader[*ToolResult], error)
```

### 快速创建工具
```go
import "github.com/cloudwego/eino/components/tool/utils"

type ScanArgs struct {
    Target string `json:"target" desc:"目标地址"`
    Flags  string `json:"flags,omitempty" desc:"可选参数"`
}

t, err := utils.InferEnhancedTool[ScanArgs](func(ctx context.Context, args *ScanArgs) (*schema.ToolResult, error) {
    // 执行扫描
    return &schema.ToolResult{Parts: []schema.ToolOutputPart{{Type: schema.ToolPartTypeText, Text: output}}}, nil
})
```

### ToolInfo 定义
```go
info := &schema.ToolInfo{
    Name: "nmap",
    Desc: "网络扫描工具",
    ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
        "target": {Type: schema.String, Desc: "目标IP", Required: true},
        "flags":  {Type: schema.String, Desc: "扫描参数"},
    }),
}
```

## Embedding 组件
```go
type Embedding interface {
    EmbedStrings(ctx context.Context, texts []string, opts ...Option) ([][]float64, error)
}
```

## Retriever / Indexer
```go
type Retriever interface {
    Retrieve(ctx context.Context, query string, opts ...Option) ([]*schema.Document, error)
}

type Indexer interface {
    Store(ctx context.Context, docs []*schema.Document, opts ...Option) ([]string, error)
}
```

## ChatTemplate
```go
type ChatTemplate interface {
    Format(ctx context.Context, input map[string]any, opts ...Option) ([]*schema.Message, error)
}
```
