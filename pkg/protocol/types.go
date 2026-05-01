package protocol

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
)

type TaskType string

const (
	TaskTypeReconnaissance    TaskType = "reconnaissance"
	TaskTypeVulnerabilityScan TaskType = "vulnerability_scan"
	TaskTypeExploitation      TaskType = "exploitation"
	TaskTypePostExploitation  TaskType = "post_exploitation"
	TaskTypeReportGeneration  TaskType = "report_generation"
)

type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusAssigned  TaskStatus = "assigned"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCancelled TaskStatus = "cancelled"
)

type Task struct {
	ID        string                 `json:"id"`
	Type      TaskType               `json:"type"`
	Target    string                 `json:"target"`
	Params    map[string]interface{} `json:"params,omitempty"`
	Status    TaskStatus             `json:"status"`
	WorkerID  string                 `json:"worker_id,omitempty"`
	Result    *schema.ToolResult     `json:"result,omitempty"`
	Context   map[string]interface{} `json:"context,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

type WorkerStatus string

const (
	WorkerStatusIdle    WorkerStatus = "idle"
	WorkerStatusBusy    WorkerStatus = "busy"
	WorkerStatusOffline WorkerStatus = "offline"
	WorkerStatusUnknown WorkerStatus = "unknown"
)

type WorkerInfo struct {
	ID            string             `json:"id"`
	Name          string             `json:"name"`
	Host          string             `json:"host"`
	Port          int                `json:"port"`
	SSHUser       string             `json:"ssh_user"`
	Capabilities  []string           `json:"capabilities"`
	ToolInfos     []*schema.ToolInfo `json:"tool_infos"`
	Status        WorkerStatus       `json:"status"`
	CurrentTask   string             `json:"current_task,omitempty"`
	LastHeartbeat time.Time          `json:"last_heartbeat"`
	RegisteredAt  time.Time          `json:"registered_at"`
}

type Heartbeat struct {
	WorkerID    string         `json:"worker_id"`
	Status      WorkerStatus   `json:"status"`
	CurrentTask string         `json:"current_task,omitempty"`
	Timestamp   time.Time      `json:"timestamp"`
	Metrics     map[string]any `json:"metrics,omitempty"`
}

type GraphNodeState string

const (
	GraphNodeStatePending GraphNodeState = "pending"
	GraphNodeStateRunning GraphNodeState = "running"
	GraphNodeStateSuccess GraphNodeState = "success"
	GraphNodeStateFailed  GraphNodeState = "failed"
	GraphNodeStateSkipped GraphNodeState = "skipped"
)

type GraphNodeUpdate struct {
	TaskID    string         `json:"task_id"`
	NodeName  string         `json:"node_name"`
	State     GraphNodeState `json:"state"`
	Output    any            `json:"output,omitempty"`
	Error     string         `json:"error,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

type DispatchResponse struct {
	Accepted bool   `json:"accepted"`
	Error    string `json:"error,omitempty"`
}

const (
	ExtraKeyWorkerInfo      = "worker_info"
	ExtraKeyHeartbeat       = "heartbeat"
	ExtraKeyTask            = "task"
	ExtraKeyDispatchResponse = "dispatch_response"
	ExtraKeyToolResult      = "tool_result"
)

func WorkerRegisterMessage(info *WorkerInfo) *schema.Message {
	return &schema.Message{
		Role:    schema.User,
		Content: "register",
		Extra: map[string]any{
			ExtraKeyWorkerInfo: info,
		},
	}
}

func HeartbeatMessage(hb *Heartbeat) *schema.Message {
	return &schema.Message{
		Role:    schema.System,
		Content: "heartbeat",
		Extra: map[string]any{
			ExtraKeyHeartbeat: hb,
		},
	}
}

func DispatchTaskMessage(task *Task) *schema.Message {
	callID := "call_" + string(task.Type) + "_" + task.ID
	toolCall := schema.ToolCall{
		ID:   callID,
		Type: "function",
		Function: schema.FunctionCall{
			Name:      string(task.Type),
			Arguments: taskToJSON(task),
		},
	}
	return schema.AssistantMessage("", []schema.ToolCall{toolCall})
}

func ReportResultMessage(callID string, result *schema.ToolResult) *schema.Message {
	return &schema.Message{
		Role:       schema.Tool,
		Content:    toolResultToString(result),
		ToolCallID: callID,
		ToolName:   "",
		Extra: map[string]any{
			ExtraKeyToolResult: result,
		},
	}
}

func ExtractWorkerInfo(msg *schema.Message) *WorkerInfo {
	if msg.Extra == nil {
		return nil
	}
	if v, ok := msg.Extra[ExtraKeyWorkerInfo].(*WorkerInfo); ok {
		return v
	}
	// JSON decode produces map[string]interface{}; re-marshal to concrete type
	return unmarshalViaJSON[WorkerInfo](msg.Extra[ExtraKeyWorkerInfo])
}

func ExtractHeartbeat(msg *schema.Message) *Heartbeat {
	if msg.Extra == nil {
		return nil
	}
	if v, ok := msg.Extra[ExtraKeyHeartbeat].(*Heartbeat); ok {
		return v
	}
	return unmarshalViaJSON[Heartbeat](msg.Extra[ExtraKeyHeartbeat])
}

func ExtractToolResult(msg *schema.Message) *schema.ToolResult {
	if msg.Extra == nil {
		return nil
	}
	if v, ok := msg.Extra[ExtraKeyToolResult].(*schema.ToolResult); ok {
		return v
	}
	return unmarshalViaJSON[schema.ToolResult](msg.Extra[ExtraKeyToolResult])
}

func unmarshalViaJSON[T any](v any) *T {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out T
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return &out
}

func ExtractTaskFromToolCall(tc *schema.ToolCall) (*Task, error) {
	var task Task
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func taskToJSON(task *Task) string {
	b, _ := json.Marshal(task)
	return string(b)
}

func toolResultToString(result *schema.ToolResult) string {
	if result == nil {
		return ""
	}
	var parts []string
	for _, p := range result.Parts {
		if p.Type == schema.ToolPartTypeText {
			parts = append(parts, p.Text)
		}
	}
	return strings.Join(parts, "\n")
}
