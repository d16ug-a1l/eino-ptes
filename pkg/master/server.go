package master

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/cloudwego/eino-ptes/pkg/protocol"
	"github.com/gorilla/websocket"
)

type Server struct {
	addr        string
	tcpAddr     string
	memberMgr   *MemberManager
	scheduler   *Scheduler
	orchestrator *Orchestrator
	upgrader    websocket.Upgrader
	wsClients   map[*websocket.Conn]bool
	wsMu        sync.RWMutex
	httpServer  *http.Server
	tcpListener net.Listener
	wg          sync.WaitGroup
	stopCh      chan struct{}
}

func NewServer(addr, tcpAddr string, mm *MemberManager, sched *Scheduler, orch *Orchestrator) *Server {
	s := &Server{
		addr:         addr,
		tcpAddr:      tcpAddr,
		memberMgr:    mm,
		scheduler:    sched,
		orchestrator: orch,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		wsClients: make(map[*websocket.Conn]bool),
		stopCh:    make(chan struct{}),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/api/workers", s.handleWorkers)
	mux.HandleFunc("/api/tasks", s.handleTasks)
	mux.HandleFunc("/api/tasks/", s.handleTaskDetail)
	mux.Handle("/", http.FileServer(http.Dir("./web/static")))

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	return s
}

func (s *Server) Run(ctx context.Context) error {
	s.wg.Add(2)
	go s.runTCPServer(ctx)
	go s.runHTTPServer(ctx)
	go s.heartbeatCleanup(ctx)

	<-s.stopCh
	s.wg.Wait()
	return nil
}

func (s *Server) runTCPServer(ctx context.Context) {
	defer s.wg.Done()

	ln, err := net.Listen("tcp", s.tcpAddr)
	if err != nil {
		log.Printf("tcp listen error: %v", err)
		return
	}
	s.tcpListener = ln
	log.Printf("tcp server listening on %s", s.tcpAddr)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		go s.handleWorkerConn(ctx, conn)
	}
}

func (s *Server) runHTTPServer(ctx context.Context) {
	defer s.wg.Done()
	log.Printf("http server listening on %s", s.addr)

	go func() {
		<-ctx.Done()
		_ = s.httpServer.Shutdown(context.Background())
	}()

	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("http server error: %v", err)
	}
}

func (s *Server) heartbeatCleanup(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			removed := s.memberMgr.CleanupStale()
			for _, id := range removed {
				s.scheduler.UnregisterConn(id)
				log.Printf("worker %s removed due to heartbeat timeout", id)
			}
		}
	}
}

func (s *Server) handleWorkerConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	var workerID string

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		var msg protocol.Message
		if err := decoder.Decode(&msg); err != nil {
			if workerID != "" {
				s.scheduler.UnregisterConn(workerID)
				s.memberMgr.Unregister(workerID)
			}
			return
		}

		switch msg.Type {
		case protocol.MsgTypeRegister:
			payloadBytes, _ := json.Marshal(msg.Payload)
			var info protocol.WorkerInfo
			_ = json.Unmarshal(payloadBytes, &info)
			workerID = info.ID
			s.memberMgr.Register(&info)
			s.scheduler.RegisterConn(workerID, encoder)
			log.Printf("worker registered: %s (%s)", info.ID, info.Name)

		case protocol.MsgTypeHeartbeat:
			payloadBytes, _ := json.Marshal(msg.Payload)
			var hb protocol.Heartbeat
			_ = json.Unmarshal(payloadBytes, &hb)
			s.memberMgr.UpdateHeartbeat(&hb)

		case protocol.MsgTypeReportResult:
			payloadBytes, _ := json.Marshal(msg.Payload)
			var payload map[string]interface{}
			_ = json.Unmarshal(payloadBytes, &payload)
			taskID, _ := payload["task_id"].(string)
			resultBytes, _ := json.Marshal(payload["result"])
			var result protocol.TaskResult
			_ = json.Unmarshal(resultBytes, &result)
			if taskID != "" {
				s.orchestrator.OnTaskResult(taskID, &result)
				log.Printf("task %s result received", taskID)
			}
		}
	}
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	s.wsMu.Lock()
	s.wsClients[conn] = true
	s.wsMu.Unlock()

	defer func() {
		s.wsMu.Lock()
		delete(s.wsClients, conn)
		s.wsMu.Unlock()
	}()

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			return
		}
	}
}

func (s *Server) BroadcastGraphUpdate(update protocol.GraphNodeUpdate) {
	s.wsMu.RLock()
	clients := make([]*websocket.Conn, 0, len(s.wsClients))
	for c := range s.wsClients {
		clients = append(clients, c)
	}
	s.wsMu.RUnlock()

	data, _ := json.Marshal(update)
	for _, c := range clients {
		_ = c.WriteMessage(websocket.TextMessage, data)
	}
}

func (s *Server) handleWorkers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	workers := s.memberMgr.GetWorkers()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(workers)
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		tasks := s.orchestrator.taskStore.List()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tasks)
	case http.MethodPost:
		var req struct {
			Type   string                 `json:"type"`
			Target string                 `json:"target"`
			Params map[string]interface{} `json:"params,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		task, err := s.orchestrator.CreateTask(r.Context(), protocol.TaskType(req.Type), req.Target, req.Params)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		go func() {
			if err := s.orchestrator.ExecuteTask(context.Background(), task); err != nil {
				log.Printf("execute task %s error: %v", task.ID, err)
			}
		}()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(task)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleTaskDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Path[len("/api/tasks/"):]
	task := s.orchestrator.taskStore.Get(id)
	if task == nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(task)
}

func (s *Server) Stop() {
	close(s.stopCh)
	if s.tcpListener != nil {
		_ = s.tcpListener.Close()
	}
	if s.httpServer != nil {
		_ = s.httpServer.Close()
	}
}
