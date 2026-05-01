package master

import (
	"fmt"
	"io"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

type SSHManager struct {
	mu      sync.RWMutex
	clients map[string]*ssh.Client // workerID -> ssh client
}

func NewSSHManager() *SSHManager {
	return &SSHManager{
		clients: make(map[string]*ssh.Client),
	}
}

type SSHSession struct {
	Stdin  io.WriteCloser
	Stdout io.Reader
	Stderr io.Reader
	Close  func() error
}

func (m *SSHManager) Connect(workerID, host string, port int, user string, auth ssh.AuthMethod) error {
	if port == 0 {
		port = 22
	}
	config := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{auth},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	addr := fmt.Sprintf("%s:%d", host, port)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return fmt.Errorf("ssh dial %s: %w", addr, err)
	}

	m.mu.Lock()
	if old, ok := m.clients[workerID]; ok {
		old.Close()
	}
	m.clients[workerID] = client
	m.mu.Unlock()
	return nil
}

func (m *SSHManager) NewSession(workerID string) (*SSHSession, error) {
	m.mu.RLock()
	client, ok := m.clients[workerID]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("no ssh connection for worker %s", workerID)
	}

	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("ssh new session: %w", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		return nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, err
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		session.Close()
		return nil, err
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("xterm-256color", 80, 24, modes); err != nil {
		session.Close()
		return nil, fmt.Errorf("ssh request pty: %w", err)
	}
	if err := session.Shell(); err != nil {
		session.Close()
		return nil, fmt.Errorf("ssh shell: %w", err)
	}

	return &SSHSession{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		Close:  session.Close,
	}, nil
}

func (m *SSHManager) Disconnect(workerID string) {
	m.mu.Lock()
	if client, ok := m.clients[workerID]; ok {
		client.Close()
		delete(m.clients, workerID)
	}
	m.mu.Unlock()
}
