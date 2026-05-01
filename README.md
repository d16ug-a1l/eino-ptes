# Eino-PTES

[![CI](https://github.com/d16ug-a1l/eino-ptes/actions/workflows/ci.yml/badge.svg)](https://github.com/d16ug-a1l/eino-ptes/actions/workflows/ci.yml)

基于 [CloudWeGo Eino](https://github.com/cloudwego/eino) LLM 框架开发的多 Agent 并行渗透测试执行系统（PTES）。主 Agent 负责任务编排与可视化展示，子 Agent 部署在 Kali Linux 虚拟机中执行实际渗透测试任务。

## 架构设计

```
┌─────────────────────────────────────────────────────────┐
│  Master (主 Agent)                                       │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐  │
│  │ HTTP API    │  │ TCP Worker  │  │ WebSocket       │  │
│  │ :9090       │  │ Registry    │  │ Dashboard       │  │
│  └─────────────┘  └─────────────┘  └─────────────────┘  │
│  ┌─────────────────────────────────────────────────────┐│
│  │  Eino Graph 编排                                     ││
│  │  START → reconnaissance → vulnerability_scan → END   ││
│  └─────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────┘
                           │
          ┌────────────────┼────────────────┐
          │ SSH Tunnel     │                │
          ▼                ▼                ▼
┌───────────────┐ ┌───────────────┐ ┌───────────────┐
│ kali-worker-1 │ │ kali-worker-2 │ │    ...        │
│ (nmap/nikto)  │ │ (nmap/nikto)  │ │               │
└───────────────┘ └───────────────┘ └───────────────┘
```

## 核心功能

- **Eino Graph 编排**：利用 `compose.Graph` 定义渗透测试标准流程，支持条件分支（如信息收集成功后进入漏洞扫描，否则结束）
- **多 Worker 并行执行**：支持多个 Kali Linux Worker 节点同时注册、接收任务、上报结果
- **工具链标准化**：nmap、nikto、dirb 等渗透测试工具统一实现 Eino `tool.InvokableTool` 接口
- **实时状态可视化**：通过 WebSocket 向 Dashboard 推送 Graph 节点执行状态
- **心跳与成员管理**：Worker 每 5 秒上报心跳，Master 自动清理超时节点
- **GitHub Actions CI**：自动化构建、测试与代码格式检查

## 技术栈

- **框架**：CloudWeGo Eino (`compose.Graph`, `tool.InvokableTool`, `callbacks.Handler`)
- **语言**：Go 1.21+
- **通信**：自定义 JSON over TCP + WebSocket (Dashboard)
- **部署**：OrbStack VM + SSH 隧道
- **靶场**：DVWA (Damn Vulnerable Web Application)

## 项目结构

```
eino-ptes/
├── cmd/
│   ├── master/          # 主 Agent 服务入口
│   └── worker/          # 子 Agent 服务入口
├── pkg/
│   ├── master/
│   │   ├── server.go        # HTTP + WebSocket + TCP API
│   │   ├── orchestrator.go  # Eino Graph 编排器
│   │   ├── scheduler.go     # 任务调度器
│   │   ├── member.go        # Worker 成员管理
│   │   └── graph_state.go   # Graph 执行状态收集
│   ├── worker/
│   │   ├── worker.go        # Worker 核心逻辑
│   │   ├── heartbeat.go     # 心跳发送器
│   │   ├── executor.go      # 任务执行器
│   │   └── tools/           # 渗透测试工具封装 (nmap, nikto, dirb)
│   └── protocol/
│       └── types.go         # 共享消息类型
├── .github/workflows/ci.yml # GitHub Actions CI
└── go.mod
```

## 快速开始

### 启动 Master

```bash
go run ./cmd/master -http=:9090 -tcp=:9091
```

### 启动 Worker

```bash
go run ./cmd/worker \
  -id=kali-worker-1 \
  -name="Kali-VM-1" \
  -master=<master-ip>:9091 \
  -caps=nmap,nikto,dirb
```

### 创建任务

```bash
curl -X POST http://localhost:9090/api/tasks \
  -H "Content-Type: application/json" \
  -d '{"type":"reconnaissance","target":"192.168.1.1","params":{"flags":"-sV"}}'
```

### 查看 Worker 列表

```bash
curl http://localhost:9090/api/workers
```

## CI 状态

本项目使用 GitHub Actions 进行持续集成，每次 push 到 `main` 分支或提交 PR 时自动执行：

- `go build ./...`
- `go test -race ./...`
- `gofmt` 格式检查

## 许可证

MIT
