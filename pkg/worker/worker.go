package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/cloudwego/eino-ptes/pkg/protocol"
	"github.com/cloudwego/eino/schema"
)

type Config struct {
	ID           string
	Name         string
	MasterAddr   string
	Capabilities []string
	ListenAddr   string
}

type Worker struct {
	config   *Config
	conn     net.Conn
	encoder  *json.Encoder
	decoder  *json.Decoder
	executor *Executor
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
	mu       sync.RWMutex
	status   protocol.WorkerStatus
	current  string
}

func New(cfg *Config) *Worker {
	return &Worker{
		config:   cfg,
		executor: NewExecutor(cfg.Capabilities),
		stopCh:   make(chan struct{}),
		status:   protocol.WorkerStatusIdle,
	}
}

func (w *Worker) Run(ctx context.Context) error {
	conn, err := net.Dial("tcp", w.config.MasterAddr)
	if err != nil {
		return fmt.Errorf("dial master: %w", err)
	}
	w.conn = conn
	w.encoder = json.NewEncoder(conn)
	w.decoder = json.NewDecoder(conn)

	if err := w.register(); err != nil {
		return fmt.Errorf("register: %w", err)
	}

	w.wg.Add(2)
	go w.heartbeatLoop(ctx)
	go w.handleMessages(ctx)

	log.Printf("worker %s connected to master at %s", w.config.ID, w.config.MasterAddr)

	select {
	case <-ctx.Done():
		w.Stop()
	case <-w.stopCh:
	}
	w.wg.Wait()
	return nil
}

func (w *Worker) register() error {
	info := &protocol.WorkerInfo{
		ID:            w.config.ID,
		Name:          w.config.Name,
		Host:          w.config.ListenAddr,
		Capabilities:  w.config.Capabilities,
		ToolInfos:     w.executor.GetToolInfos(context.Background()),
		Status:        protocol.WorkerStatusIdle,
		RegisteredAt:  time.Now(),
		LastHeartbeat: time.Now(),
	}
	msg := protocol.WorkerRegisterMessage(info)
	return w.encoder.Encode(msg)
}

func (w *Worker) heartbeatLoop(ctx context.Context) {
	defer w.wg.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.mu.RLock()
			hb := &protocol.Heartbeat{
				WorkerID:    w.config.ID,
				Status:      w.status,
				CurrentTask: w.current,
				Timestamp:   time.Now(),
			}
			w.mu.RUnlock()

			msg := protocol.HeartbeatMessage(hb)
			if err := w.encoder.Encode(msg); err != nil {
				log.Printf("heartbeat error: %v", err)
				w.Stop()
				return
			}
		}
	}
}

func (w *Worker) handleMessages(ctx context.Context) {
	defer w.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		default:
		}

		var msg schema.Message
		if err := w.decoder.Decode(&msg); err != nil {
			if err.Error() != "EOF" {
				log.Printf("decode error: %v", err)
			}
			w.Stop()
			return
		}

		go w.processMessage(ctx, &msg)
	}
}

func (w *Worker) processMessage(ctx context.Context, msg *schema.Message) {
	if msg.Role == schema.Assistant && len(msg.ToolCalls) > 0 {
		for _, tc := range msg.ToolCalls {
			w.handleToolCall(ctx, tc)
		}
	}
}

func (w *Worker) handleToolCall(ctx context.Context, tc schema.ToolCall) {
	w.mu.Lock()
	w.status = protocol.WorkerStatusBusy
	w.current = tc.ID
	w.mu.Unlock()

	var task protocol.Task
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &task); err != nil {
		w.reportError(tc.ID, tc.Function.Name, err)
		w.mu.Lock()
		w.status = protocol.WorkerStatusIdle
		w.current = ""
		w.mu.Unlock()
		return
	}

	result, err := w.executor.Execute(ctx, task)

	w.mu.Lock()
	w.status = protocol.WorkerStatusIdle
	w.current = ""
	w.mu.Unlock()

	if err != nil {
		w.reportError(tc.ID, tc.Function.Name, err)
		return
	}

	msg := protocol.ReportResultMessage(tc.ID, result)
	if err := w.encoder.Encode(msg); err != nil {
		log.Printf("report result error: %v", err)
	}
}

func (w *Worker) reportError(callID, toolName string, err error) {
	result := &schema.ToolResult{
		Parts: []schema.ToolOutputPart{
			{
				Type: schema.ToolPartTypeText,
				Text: err.Error(),
			},
		},
	}
	msg := &schema.Message{
		Role:       schema.Tool,
		Content:    err.Error(),
		ToolCallID: callID,
		ToolName:   toolName,
		Extra: map[string]any{
			protocol.ExtraKeyToolResult: result,
		},
	}
	if encErr := w.encoder.Encode(msg); encErr != nil {
		log.Printf("report error message error: %v", encErr)
	}
}

func (w *Worker) Stop() {
	w.stopOnce.Do(func() {
		close(w.stopCh)
		if w.conn != nil {
			_ = w.conn.Close()
		}
	})
}
