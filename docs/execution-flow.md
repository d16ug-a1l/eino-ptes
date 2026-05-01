# Eino-PTES 执行流程说明

本文档描述以 DVWA（Damn Vulnerable Web Application）为目标进行扫描时，Eino-PTES 系统从任务提交到结果返回的完整运行过程。

---

## 1. 整体架构概览

```
+-------------------+         HTTP/TCP          +-------------------+
|      用户层        |  <------------------->  |     Master 层      |
|  (curl / Web UI)  |                         | (HTTP + TCP Server)|
+-------------------+                         +-------------------+
                                                        |
                                                        | TCP 长连接
                                                        |
                                               +--------+--------+
                                               |   Worker 节点    |
                                               | (kali-worker-1)  |
                                               | (kali-worker-2)  |
                                               +------------------+
```

---

## 2. 执行流程图

```
+--------------------------+
|          用户层           |
|  POST /api/tasks         |
|  {target, type}          |
+------------+-------------+
             |
             v
+--------------------------+
|      HTTP Server         |
|  CreateTask()            |
|  ExecuteTask() (后台)     |
+------------+-------------+
             |
             v
+--------------------------+
|      Orchestrator        |
|  compiledGraph.Invoke()  |
|  PTESState{TaskID,...}   |
+------------+-------------+
             |
             v
+--------------------------+
|         START            |
+------------+-------------+
             |
             v
+--------------------------+     +---------------------------+
|   reconnaissance 节点     |     |        Scheduler          |
|  dispatchAndWait(task,    +---->+  AcquireIdleWorker("nmap") |
|             "nmap")       |     |  -> kali-worker-1 (busy)  |
+------------+--------------+     +-------------+-------------+
             |                                    |
             |                                    | TCP Encode
             |                                    v
             |                      +---------------------------+
             |                      |     kali-worker-1         |
             |                      |  decode -> processMessage |
             |                      |  -> handleToolCall        |
             |                      |  -> Executor.nmap()       |
             |                      |  nmap -sV -sC <target>    |
             |                      +-------------+-------------+
             |                                    |
             |                                    | ReportResultMessage
             |                                    | TCP 返回
             |                                    v
             |                      +---------------------------+
             |                      |   Scheduler.OnToolResult  |
             |                      |   ReleaseWorker(worker-1) |
             |                      |   ch <- msg               |
             |                      +-------------+-------------+
             |                                    |
             | resultMsg                          |
             v                                    |
+--------------------------+                     |
|  state.Results[recon]    |                     |
|  = result                |                     |
+------------+-------------+                     |
             |                                    |
             | GraphBranch: result != nil ?       |
             | YES                                |
             v                                    |
+--------------------------+                     |
|  vulnerability_scan 节点  |                     |
|  dispatchAndWait(task,    +-------------------->+
|           "nikto")       |     AcquireIdleWorker("nikto")
+------------+-------------+     -> kali-worker-2 (busy)
             |                     TCP Encode
             |                                    v
             |                      +---------------------------+
             |                      |     kali-worker-2         |
             |                      |  handleToolCall("nikto")  |
             |                      |  -> Executor.nikto()      |
             |                      |  nikto -h http://<target> |
             |                      +-------------+-------------+
             |                                    |
             |                                    | TCP 返回
             |                                    v
             |                      +---------------------------+
             |                      |   Scheduler.OnToolResult  |
             |                      |   ReleaseWorker(worker-2) |
             |                      +-------------+-------------+
             |                                    |
             | resultMsg                          |
             v                                    |
+--------------------------+                     |
|  state.Results[vuln]     |                     |
|  = result                |                     |
+------------+-------------+                     |
             |                                    |
             v                                    |
+--------------------------+                     |
|          END             |                     |
+------------+-------------+                     |
             |                                    |
             v                                    |
+--------------------------+                     |
|    ExecuteTask 聚合       |                     |
|  task.Result = 聚合JSON   |                     |
|  task.Status = completed  |                     |
+------------+-------------+                     |
             |                                    |
             v                                    |
+--------------------------+                     |
|  GET /api/tasks/{id}     | <------------------+
|      返回报告             |
+--------------------------+
```

---

## 3. 关键组件交互

| 阶段 | 组件 | 动作 |
|------|------|------|
| 任务接收 | HTTP Server | 接收 POST 请求，调用 `CreateTask` 生成任务，后台启动 `ExecuteTask` |
| 图编排 | Orchestrator / `compose.Graph` | 按 DAG 顺序执行节点：`reconnaissance` -> `vulnerability_scan` |
| 节点执行 | Lambda Node | `dispatchAndWait` 构造 `schema.Message`，内含 `ToolCall` |
| 调度分配 | Scheduler / MemberManager | `AcquireIdleWorker` 原子获取 worker 并将其状态置为 `busy` |
| 网络传输 | TCP 长连接 | JSON 序列化的 `schema.Message` 在 master 与 worker 之间双向传输 |
| 工具执行 | Worker / Executor | 解析 `ToolCall`，根据 `TaskType` 调用 `nmap` 或 `nikto` 子进程 |
| 结果回传 | Worker | 通过 `ReportResultMessage` 返回 `schema.ToolResult` |
| 状态释放 | Scheduler | `OnToolResult` 向等待 channel 写入结果，`ReleaseWorker` 恢复 `idle` |
| 任务聚合 | Orchestrator | 遍历 `state.Results` 生成 `{phases: [...]}` 聚合报告 |

---

## 4. 分阶段详细说明

### 4.1 任务创建

用户提交请求：

```bash
curl -X POST http://localhost:9090/api/tasks \
  -H "Content-Type: application/json" \
  -d '{"target":"192.168.1.100","type":"reconnaissance"}'
```

`Server.handleTasks` 的处理流程：

1. 调用 `orchestrator.CreateTask` 生成任务，分配 `task-id`，状态为 `pending`
2. 在后台 goroutine 中调用 `orchestrator.ExecuteTask`
3. 立即向用户返回 `201 Created` 和任务信息（含 `created_at` / `updated_at`）

### 4.2 Graph 编排

`ExecuteTask` 初始化 `PTESState`，调用编译好的 Graph：

```
START -> reconnaissance -> [分支判断] -> vulnerability_scan -> END
```

- 若 `reconnaissance` 返回结果（`state.Results[recon]` 不为空），进入 `vulnerability_scan`
- 若结果为空，直接结束（`compose.END`）

### 4.3 调度分发（以 nmap 为例）

`reconNode` 内部流程：

1. 构造 `protocol.Task{Type: reconnaissance, Target: "192.168.1.100"}`
2. 调用 `protocol.DispatchTaskMessage(task)` 生成 `schema.Message`：
   - `Role: Assistant`
   - `ToolCalls[0].Function.Name = "nmap"`
   - `Arguments` 内含序列化后的 Task JSON
3. 调用 `scheduler.DispatchToolCall(ctx, msg)`
4. `Scheduler` 调用 `memberMgr.AcquireIdleWorker("nmap")`：
   - 通过 `toolIndex["nmap"]` 快速定位具备该工具的 worker
   - 原子操作：将选中的 worker 状态从 `idle` 置为 `busy`
   - 假设选中 `kali-worker-1`
5. 通过 TCP 长连接向 worker-1 发送消息
6. `DispatchToolCall` 阻塞在 `resultCh` channel 上等待结果

### 4.4 Worker 执行

`kali-worker-1` 的处理流程：

1. `handleMessages` 解码到 `Role = Assistant` 的消息
2. `processMessage` 提取 `ToolCalls`，为每个调用启动 goroutine
3. `handleToolCall` 解析 `Function.Arguments`，还原出 `protocol.Task`
4. `executor.Execute` 根据 `TaskType` 选择 `nmap` 工具
5. 执行子进程：`nmap -sV -sC 192.168.1.100`
6. 扫描完成后封装为 `schema.ToolResult`，文本输出放入 `Parts[0].Text`
7. 构造返回消息（`Role: Tool`，`ToolCallID: call_reconnaissance_task-xxx`）
8. 通过同一 TCP 连接发送回 master

### 4.5 结果接收与释放

Master 的 `handleWorkerConn` 收到 `Role = Tool` 的消息：

1. `protocol.ExtractToolResult(msg)` 从 `Extra` 中反序列化结果
2. 调用 `scheduler.OnToolResult(callID, msg)`：
   - 从 `toolWaits` 取出等待的 channel，写入结果
   - 从 `callWorkers` 取出 worker ID
   - 调用 `memberMgr.ReleaseWorker(workerID)` 将状态恢复为 `idle`
3. `DispatchToolCall` 的阻塞解除，返回结果给 `reconNode`

### 4.6 下一阶段（nikto）

`vulnerability_scan` 节点的流程与上述相同，只是工具名变为 `"nikto"`：

1. `AcquireIdleWorker("nikto")` 从两个 worker 中选择当前 `idle` 的节点
2. 分发 `nikto -h http://192.168.1.100:80` 任务
3. 执行完成后返回结果，worker 状态恢复

> 两个 worker 节点构成简单的负载均衡：当前实现按 `toolIndex` 顺序选取第一个 `idle` worker。

### 4.7 任务聚合与完成

Graph 到达 `END` 后，`ExecuteTask` 执行收尾：

1. 遍历 `finalState.Results`，将各阶段输出聚合为 JSON：
   ```json
   {
     "phases": [
       {"phase": "reconnaissance", "output": ["Nmap scan report..."]},
       {"phase": "vulnerability_scan", "output": ["nikto scan results..."]}
     ]
   }
   ```
2. 将聚合结果写入 `task.Result`
3. 将 `task.Status` 更新为 `completed`
4. 用户通过 `GET /api/tasks/{taskID}` 获取完整报告

---

## 5. 通信协议映射

| 阶段 | Master -> Worker | Worker -> Master |
|------|------------------|------------------|
| 注册 | `Role: User`, `Extra[worker_info]` | — |
| 心跳 | — | `Role: System`, `Extra[heartbeat]` |
| 任务分发 | `Role: Assistant`, `ToolCalls[0]` | — |
| 结果返回 | — | `Role: Tool`, `ToolCallID`, `Extra[tool_result]` |
