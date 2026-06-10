package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := `app:
  start_mode: tyty
  minimize_to_tray: true
  show_window_on_start: true
proxy:
  listen: 127.0.0.1:18080
  direct_bind_ip: 192.168.1.186
  foreign_proxy: 127.0.0.1:9674
vpn:
  tyty:
    exe: C:\Program Files\Tyty\Tyty.exe
    process: Tyty
    stop_command: Write-Output stop-tyty
    adapter_keywords:
      - Mihomo
  globalprotect:
    exe: C:\Program Files\Palo Alto Networks\GlobalProtect\PanGPA.exe
    process: PanGPA
    stop_command: Write-Output stop-gp
    adapter_keywords:
      - PANGP
rules:
  company_domains:
    - "*.corp.example.com"
  foreign_domains:
    - github.com
  direct_domains:
    - localhost
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Proxy.Listen != "127.0.0.1:18080" {
		t.Fatalf("unexpected listen: %s", cfg.Proxy.Listen)
	}
	if cfg.Proxy.DirectBindIP != "192.168.1.186" {
		t.Fatalf("unexpected direct bind ip: %s", cfg.Proxy.DirectBindIP)
	}
	if cfg.Proxy.ForeignProxy != "127.0.0.1:9674" {
		t.Fatalf("unexpected foreign proxy: %s", cfg.Proxy.ForeignProxy)
	}
	if cfg.App.StartMode != "tyty" || !cfg.App.ShowWindowOnStart || !cfg.App.MinimizeToTray {
		t.Fatalf("unexpected app config: %#v", cfg.App)
	}
	if cfg.VPN.Tyty.StopCommand == "" || cfg.VPN.GlobalProtect.StopCommand == "" {
		t.Fatalf("stop command should be loaded")
	}
	if cfg.VPN.Tyty.Exe == "" || cfg.VPN.GlobalProtect.Exe == "" {
		t.Fatalf("vpn exe should be loaded")
	}
	if len(cfg.Rules.CompanyDomains) != 1 || cfg.Rules.CompanyDomains[0] != "*.corp.example.com" {
		t.Fatalf("unexpected company rules: %#v", cfg.Rules.CompanyDomains)
	}
}

func TestLoadConfigRequiresListen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := `vpn:
  tyty:
    exe: C:\Program Files\Tyty\Tyty.exe
  globalprotect:
    exe: C:\Program Files\Palo Alto Networks\GlobalProtect\PanGPA.exe
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatalf("expected error")
	}
}
