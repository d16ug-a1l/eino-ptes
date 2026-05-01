package master

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino-ptes/pkg/protocol"
	"github.com/cloudwego/eino/schema"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

type Server struct {
	addr         string
	tcpAddr      string
	memberMgr    *MemberManager
	scheduler    *Scheduler
	orchestrator *Orchestrator
	sshMgr       *SSHManager
	upgrader     websocket.Upgrader
	wsClients    map[*websocket.Conn]bool
	wsWriteMu    map[*websocket.Conn]*sync.Mutex
	wsMu         sync.RWMutex
	httpServer   *http.Server
	tcpListener  net.Listener
	wg           sync.WaitGroup
	stopCh       chan struct{}
}

func NewServer(addr, tcpAddr string, mm *MemberManager, sched *Scheduler, orch *Orchestrator) *Server {
	s := &Server{
		addr:         addr,
		tcpAddr:      tcpAddr,
		memberMgr:    mm,
		scheduler:    sched,
		orchestrator: orch,
		sshMgr:       NewSSHManager(),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		wsClients: make(map[*websocket.Conn]bool),
		wsWriteMu: make(map[*websocket.Conn]*sync.Mutex),
		stopCh:    make(chan struct{}),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/ws/ssh/", s.handleSSHWebSocket)
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
	s.wg.Add(3)
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
	defer s.wg.Done()
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

		var msg schema.Message
		if err := decoder.Decode(&msg); err != nil {
			if workerID != "" {
				s.scheduler.UnregisterConn(workerID)
				s.memberMgr.Unregister(workerID)
			}
			return
		}

		switch msg.Role {
		case schema.User:
			info := protocol.ExtractWorkerInfo(&msg)
			if info != nil {
				workerID = info.ID
				s.memberMgr.Register(info)
				s.scheduler.RegisterConn(workerID, encoder)
				s.orchestrator.RefreshToolSet()
				log.Printf("worker registered: %s (%s)", info.ID, info.Name)
			}

		case schema.System:
			hb := protocol.ExtractHeartbeat(&msg)
			if hb != nil {
				s.memberMgr.UpdateHeartbeat(hb)
			}

		case schema.Tool:
			if msg.ToolCallID != "" {
				result := protocol.ExtractToolResult(&msg)
				if result != nil {
					s.orchestrator.OnTaskResult(msg.ToolCallID, result)
					log.Printf("tool result received for %s", msg.ToolCallID)
				}
				s.scheduler.OnToolResult(msg.ToolCallID, &msg)
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
	s.wsWriteMu[conn] = &sync.Mutex{}
	s.wsMu.Unlock()

	defer func() {
		s.wsMu.Lock()
		delete(s.wsClients, conn)
		delete(s.wsWriteMu, conn)
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
		s.wsMu.RLock()
		mu, ok := s.wsWriteMu[c]
		s.wsMu.RUnlock()
		if !ok {
			continue
		}
		mu.Lock()
		_ = c.WriteMessage(websocket.TextMessage, data)
		mu.Unlock()
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
			Description string                 `json:"description,omitempty"`
			Type        string                 `json:"type,omitempty"`
			Target      string                 `json:"target,omitempty"`
			Params      map[string]interface{} `json:"params,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Natural language task planning path
		if req.Description != "" {
			desc := req.Description
			task, err := s.orchestrator.CreateTask(r.Context(), protocol.TaskType("planning"), "", nil)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			go func() {
				plan, err := s.orchestrator.PlanTask(context.Background(), desc)
				if err != nil {
					log.Printf("plan task %s error: %v", task.ID, err)
					task.Status = protocol.TaskStatusFailed
					task.Result = &schema.ToolResult{
						Parts: []schema.ToolOutputPart{
							{Type: schema.ToolPartTypeText, Text: "规划失败: " + err.Error()},
						},
					}
					s.orchestrator.taskStore.Save(task)
					return
				}
				task.Type = protocol.TaskType(plan.Phases[0].Phase)
				task.Target = plan.Target
				s.orchestrator.taskStore.Save(task)

				if err := s.orchestrator.ExecuteTaskWithPlan(context.Background(), task, plan); err != nil {
					log.Printf("execute task with plan %s error: %v", task.ID, err)
				}
			}()

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(task)
			return
		}

		// Traditional explicit task path
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
	path := r.URL.Path[len("/api/tasks/"):]
	parts := strings.Split(path, "/")
	id := parts[0]

	if len(parts) > 1 && parts[1] == "graph" {
		tg := s.orchestrator.GetTaskGraph(id)
		if tg == nil {
			http.Error(w, "graph not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tg)
		return
	}

	task := s.orchestrator.taskStore.Get(id)
	if task == nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(task)
}

func (s *Server) handleSSHWebSocket(w http.ResponseWriter, r *http.Request) {
	workerID := r.URL.Path[len("/ws/ssh/"):]
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ssh websocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	// 等待前端发送连接配置
	_, msg, err := conn.ReadMessage()
	if err != nil {
		log.Printf("ssh websocket read config error: %v", err)
		return
	}

	var cfg struct {
		User       string `json:"user"`
		Password   string `json:"password"`
		PrivateKey string `json:"private_key"`
		Port       int    `json:"port"`
	}
	if err := json.Unmarshal(msg, &cfg); err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("配置解析失败: "+err.Error()))
		return
	}

	worker := s.memberMgr.GetWorker(workerID)
	if worker == nil {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("worker 不存在"))
		return
	}

	if cfg.Port == 0 {
		cfg.Port = 22
	}
	if cfg.User == "" {
		cfg.User = worker.SSHUser
	}
	if cfg.User == "" {
		cfg.User = "root"
	}

	var auth ssh.AuthMethod
	if cfg.PrivateKey != "" {
		signer, err := ssh.ParsePrivateKey([]byte(cfg.PrivateKey))
		if err != nil {
			_ = conn.WriteMessage(websocket.TextMessage, []byte("私钥解析失败: "+err.Error()))
			return
		}
		auth = ssh.PublicKeys(signer)
	} else {
		auth = ssh.Password(cfg.Password)
	}

	if err := s.sshMgr.Connect(workerID, worker.Host, cfg.Port, cfg.User, auth); err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("SSH 连接失败: "+err.Error()))
		return
	}
	defer s.sshMgr.Disconnect(workerID)

	sess, err := s.sshMgr.NewSession(workerID)
	if err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("SSH 会话失败: "+err.Error()))
		return
	}
	defer sess.Close()

	_ = conn.WriteMessage(websocket.TextMessage, []byte("\r\n=== SSH 连接成功 ===\r\n"))

	// stdout -> WebSocket
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := sess.Stdout.Read(buf)
			if n > 0 {
				_ = conn.WriteMessage(websocket.BinaryMessage, buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	// stderr -> WebSocket
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := sess.Stderr.Read(buf)
			if n > 0 {
				_ = conn.WriteMessage(websocket.BinaryMessage, buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	// WebSocket -> stdin
	for {
		mt, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if mt == websocket.TextMessage || mt == websocket.BinaryMessage {
			_, _ = sess.Stdin.Write(msg)
		}
	}
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
