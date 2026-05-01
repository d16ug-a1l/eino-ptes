package protocol

import (
	"context"
)

type MasterService interface {
	RegisterWorker(ctx context.Context, info WorkerInfo) error
	Heartbeat(ctx context.Context, hb Heartbeat) error
	ReportResult(ctx context.Context, taskID string, result TaskResult) error
	ReportProgress(ctx context.Context, taskID string, progress map[string]any) error
}

type WorkerService interface {
	DispatchTask(ctx context.Context, task Task) (DispatchResponse, error)
	CancelTask(ctx context.Context, taskID string) error
}
