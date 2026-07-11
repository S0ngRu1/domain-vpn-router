package app

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"domain-vpn-router/internal/config"
)

func TestNormalizeMode(t *testing.T) {
	tests := map[string]Mode{
		"":                ModeAuto,
		"auto":            ModeAuto,
		"clash":           ModeClash,
		"clash-verge":     ModeClash,
		"tyty":            ModeClash,
		"globalprotect":   ModeGlobalProtect,
		"gp":              ModeGlobalProtect,
		"direct":          ModeDirect,
		"unexpected-mode": ModeAuto,
	}
	for input, want := range tests {
		if got := NormalizeMode(input); got != want {
			t.Fatalf("NormalizeMode(%q)=%s, want %s", input, got, want)
		}
	}
}

func TestStartMarksProxyNotRunningWhenListenFails(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on test port: %v", err)
	}
	defer listener.Close()

	cfg := config.Config{
		App: config.AppConfig{StartMode: string(ModeDirect)},
		Proxy: config.ProxyConfig{
			Listen: listener.Addr().String(),
		},
		VPN: config.VPNConfig{
			ClashVerge:    config.VPNEndpoint{Exe: "unused"},
			GlobalProtect: config.VPNEndpoint{Exe: "unused"},
		},
	}
	controller := NewController(cfg, "", "")

	if err := controller.Start(context.Background()); err != nil {
		t.Fatalf("start controller: %v", err)
	}
	defer controller.Shutdown(context.Background())

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status := controller.StatusSnapshot()
		if status.LastError != "" {
			if status.ProxyRunning {
				t.Fatalf("ProxyRunning=true after listen failure; LastError=%q", status.LastError)
			}
			if !strings.Contains(status.LastError, "listen tcp") {
				t.Fatalf("LastError=%q, want proxy listen failure", status.LastError)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for listen failure")
}
