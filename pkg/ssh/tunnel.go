package ssh

import (
	"fmt"
	"io"
	"net"
	"sync"

	"golang.org/x/crypto/ssh"
)

type TunnelConfig struct {
	Host       string
	Port       int
	User       string
	Password   string
	PrivateKey []byte
}

type Tunnel struct {
	config    *TunnelConfig
	client    *ssh.Client
	listeners []net.Listener
	mu        sync.Mutex
	closed    bool
}

func NewTunnel(cfg *TunnelConfig) *Tunnel {
	return &Tunnel{config: cfg}
}

func (t *Tunnel) Connect() error {
	var authMethods []ssh.AuthMethod

	if len(t.config.PrivateKey) > 0 {
		signer, err := ssh.ParsePrivateKey(t.config.PrivateKey)
		if err != nil {
			return fmt.Errorf("parse private key: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}

	if t.config.Password != "" {
		authMethods = append(authMethods, ssh.Password(t.config.Password))
	}

	sshConfig := &ssh.ClientConfig{
		User:            t.config.User,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	addr := fmt.Sprintf("%s:%d", t.config.Host, t.config.Port)
	client, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return fmt.Errorf("ssh dial %s: %w", addr, err)
	}

	t.client = client
	return nil
}

func (t *Tunnel) ForwardLocal(localAddr, remoteAddr string) (net.Listener, error) {
	if t.client == nil {
		return nil, fmt.Errorf("ssh client not connected")
	}

	listener, err := net.Listen("tcp", localAddr)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", localAddr, err)
	}

	t.mu.Lock()
	t.listeners = append(t.listeners, listener)
	t.mu.Unlock()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				if t.closed {
					return
				}
				continue
			}
			go t.handleForwardLocal(conn, remoteAddr)
		}
	}()

	return listener, nil
}

func (t *Tunnel) handleForwardLocal(localConn net.Conn, remoteAddr string) {
	defer localConn.Close()

	remoteConn, err := t.client.Dial("tcp", remoteAddr)
	if err != nil {
		return
	}
	defer remoteConn.Close()

	go func() {
		_, _ = io.Copy(remoteConn, localConn)
	}()
	_, _ = io.Copy(localConn, remoteConn)
}

func (t *Tunnel) ForwardRemote(remotePort int, localAddr string) (net.Listener, error) {
	if t.client == nil {
		return nil, fmt.Errorf("ssh client not connected")
	}

	listener, err := t.client.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", remotePort))
	if err != nil {
		return nil, fmt.Errorf("ssh listen remote %d: %w", remotePort, err)
	}

	t.mu.Lock()
	t.listeners = append(t.listeners, listener)
	t.mu.Unlock()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				if t.closed {
					return
				}
				continue
			}
			go t.handleForwardRemote(conn, localAddr)
		}
	}()

	return listener, nil
}

func (t *Tunnel) handleForwardRemote(remoteConn net.Conn, localAddr string) {
	defer remoteConn.Close()

	localConn, err := net.Dial("tcp", localAddr)
	if err != nil {
		return
	}
	defer localConn.Close()

	go func() {
		_, _ = io.Copy(localConn, remoteConn)
	}()
	_, _ = io.Copy(remoteConn, localConn)
}

func (t *Tunnel) Client() *ssh.Client {
	return t.client
}

func (t *Tunnel) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.closed = true
	for _, l := range t.listeners {
		_ = l.Close()
	}
	if t.client != nil {
		return t.client.Close()
	}
	return nil
}
