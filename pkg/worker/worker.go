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

	<-w.stopCh
	w.wg.Wait()
	return nil
}

func (w *Worker) register() error {
	msg := protocol.Message{
		Type: protocol.MsgTypeRegister,
		Payload: protocol.WorkerInfo{
			ID:            w.config.ID,
			Name:          w.config.Name,
			Host:          w.config.ListenAddr,
			Capabilities:  w.config.Capabilities,
			Status:        protocol.WorkerStatusIdle,
			RegisteredAt:  time.Now(),
			LastHeartbeat: time.Now(),
		},
	}
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
			hb := protocol.Heartbeat{
				WorkerID:    w.config.ID,
				Status:      w.status,
				CurrentTask: w.current,
				Timestamp:   time.Now(),
			}
			w.mu.RUnlock()

			msg := protocol.Message{
				Type:    protocol.MsgTypeHeartbeat,
				Payload: hb,
			}
			if err := w.encoder.Encode(msg); err != nil {
				log.Printf("heartbeat error: %v", err)
				close(w.stopCh)
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

		var msg protocol.Message
		if err := w.decoder.Decode(&msg); err != nil {
			if err.Error() != "EOF" {
				log.Printf("decode error: %v", err)
			}
			close(w.stopCh)
			return
		}

		go w.processMessage(ctx, msg)
	}
}

func (w *Worker) processMessage(ctx context.Context, msg protocol.Message) {
	switch msg.Type {
	case protocol.MsgTypeDispatchTask:
		var task protocol.Task
		payloadBytes, _ := json.Marshal(msg.Payload)
		if err := json.Unmarshal(payloadBytes, &task); err != nil {
			log.Printf("unmarshal task error: %v", err)
			return
		}
		w.handleTask(ctx, task)
	case protocol.MsgTypeCancelTask:
		var taskID string
		payloadBytes, _ := json.Marshal(msg.Payload)
		_ = json.Unmarshal(payloadBytes, &taskID)
		w.executor.Cancel(taskID)
	}
}

func (w *Worker) handleTask(ctx context.Context, task protocol.Task) {
	w.mu.Lock()
	w.status = protocol.WorkerStatusBusy
	w.current = task.ID
	w.mu.Unlock()

	result, err := w.executor.Execute(ctx, task)

	w.mu.Lock()
	w.status = protocol.WorkerStatusIdle
	w.current = ""
	w.mu.Unlock()

	var taskResult protocol.TaskResult
	if err != nil {
		taskResult.Error = err.Error()
	} else {
		taskResult = *result
	}

	msg := protocol.Message{
		Type: protocol.MsgTypeReportResult,
		Payload: map[string]interface{}{
			"task_id": task.ID,
			"result":  taskResult,
		},
	}
	if err := w.encoder.Encode(msg); err != nil {
		log.Printf("report result error: %v", err)
	}
}

func (w *Worker) Stop() {
	close(w.stopCh)
	if w.conn != nil {
		_ = w.conn.Close()
	}
}
