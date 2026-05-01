package protocol

import (
	"time"
)

type TaskType string

const (
	TaskTypeReconnaissance     TaskType = "reconnaissance"
	TaskTypeVulnerabilityScan  TaskType = "vulnerability_scan"
	TaskTypeExploitation       TaskType = "exploitation"
	TaskTypePostExploitation   TaskType = "post_exploitation"
	TaskTypeReportGeneration   TaskType = "report_generation"
)

type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusAssigned   TaskStatus = "assigned"
	TaskStatusRunning    TaskStatus = "running"
	TaskStatusCompleted  TaskStatus = "completed"
	TaskStatusFailed     TaskStatus = "failed"
	TaskStatusCancelled  TaskStatus = "cancelled"
)

type Task struct {
	ID        string                 `json:"id"`
	Type      TaskType               `json:"type"`
	Target    string                 `json:"target"`
	Params    map[string]interface{} `json:"params,omitempty"`
	Status    TaskStatus             `json:"status"`
	WorkerID  string                 `json:"worker_id,omitempty"`
	Result    *TaskResult            `json:"result,omitempty"`
	Context   map[string]interface{} `json:"context,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

type TaskResult struct {
	Output    string                 `json:"output,omitempty"`
	Artifacts map[string]interface{} `json:"artifacts,omitempty"`
	Error     string                 `json:"error,omitempty"`
}

type WorkerStatus string

const (
	WorkerStatusIdle      WorkerStatus = "idle"
	WorkerStatusBusy      WorkerStatus = "busy"
	WorkerStatusOffline   WorkerStatus = "offline"
	WorkerStatusUnknown   WorkerStatus = "unknown"
)

type WorkerInfo struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Host         string            `json:"host"`
	Port         int               `json:"port"`
	SSHUser      string            `json:"ssh_user"`
	Capabilities []string          `json:"capabilities"`
	Status       WorkerStatus      `json:"status"`
	CurrentTask  string            `json:"current_task,omitempty"`
	LastHeartbeat time.Time        `json:"last_heartbeat"`
	RegisteredAt  time.Time        `json:"registered_at"`
}

type Heartbeat struct {
	WorkerID    string            `json:"worker_id"`
	Status      WorkerStatus      `json:"status"`
	CurrentTask string            `json:"current_task,omitempty"`
	Timestamp   time.Time         `json:"timestamp"`
	Metrics     map[string]any    `json:"metrics,omitempty"`
}

type GraphNodeState string

const (
	GraphNodeStatePending   GraphNodeState = "pending"
	GraphNodeStateRunning   GraphNodeState = "running"
	GraphNodeStateSuccess   GraphNodeState = "success"
	GraphNodeStateFailed    GraphNodeState = "failed"
	GraphNodeStateSkipped   GraphNodeState = "skipped"
)

type GraphNodeUpdate struct {
	TaskID    string         `json:"task_id"`
	NodeName  string         `json:"node_name"`
	State     GraphNodeState `json:"state"`
	Output    any            `json:"output,omitempty"`
	Error     string         `json:"error,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

type MessageType string

const (
	MsgTypeRegister      MessageType = "register"
	MsgTypeHeartbeat     MessageType = "heartbeat"
	MsgTypeDispatchTask  MessageType = "dispatch_task"
	MsgTypeCancelTask    MessageType = "cancel_task"
	MsgTypeReportResult  MessageType = "report_result"
	MsgTypeReportProgress MessageType = "report_progress"
)

type Message struct {
	Type    MessageType `json:"type"`
	Payload interface{} `json:"payload"`
}

type DispatchRequest struct {
	Task     Task   `json:"task"`
	WorkerID string `json:"worker_id"`
}

type DispatchResponse struct {
	Accepted bool   `json:"accepted"`
	Error    string `json:"error,omitempty"`
}
