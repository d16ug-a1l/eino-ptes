package ssh

import (
	"testing"
)

func TestNewTunnel(t *testing.T) {
	cfg := &TunnelConfig{
		Host:     "127.0.0.1",
		Port:     22,
		User:     "test",
		Password: "test",
	}

	tun := NewTunnel(cfg)
	if tun == nil {
		t.Fatal("expected non-nil tunnel")
	}
	if tun.config != cfg {
		t.Error("expected config to match")
	}
}

func TestTunnelConfigDefaults(t *testing.T) {
	cfg := &TunnelConfig{
		Host: "192.168.1.100",
		Port: 22,
		User: "kali",
	}

	if cfg.Host != "192.168.1.100" {
		t.Errorf("unexpected host: %s", cfg.Host)
	}
	if cfg.Port != 22 {
		t.Errorf("unexpected port: %d", cfg.Port)
	}
}
