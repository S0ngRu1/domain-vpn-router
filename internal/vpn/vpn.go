package vpn

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"domain-vpn-router/internal/config"
)

type Manager struct {
	cfg config.VPNConfig
	mu  sync.Mutex
}

func NewManager(cfg config.VPNConfig) *Manager {
	return &Manager{cfg: cfg}
}

func (m *Manager) EnsureTyty(ctx context.Context) error {
	return m.ensure(ctx, "Tyty", m.cfg.Tyty)
}

func (m *Manager) EnsureGlobalProtect(ctx context.Context) error {
	return m.ensure(ctx, "GlobalProtect", m.cfg.GlobalProtect)
}

func (m *Manager) ensure(ctx context.Context, name string, endpoint config.VPNEndpoint) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if endpoint.Exe == "" {
		return fmt.Errorf("%s 未配置 exe", name)
	}
	if up, err := adapterUp(ctx, endpoint.AdapterKeywords); err == nil && up {
		log.Printf("%s 网卡已可用", name)
		return nil
	}

	running, err := processRunning(ctx, endpoint.Process)
	if err != nil {
		log.Printf("检测 %s 进程失败，将尝试启动: %v", name, err)
	}
	if !running {
		log.Printf("正在启动 %s: %s", name, endpoint.Exe)
		if err := exec.CommandContext(ctx, endpoint.Exe).Start(); err != nil {
			return fmt.Errorf("启动 %s 失败: %w", name, err)
		}
	} else {
		log.Printf("%s 进程已存在，等待网卡连接", name)
	}

	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		up, err := adapterUp(ctx, endpoint.AdapterKeywords)
		if err == nil && up {
			log.Printf("%s 网卡已连接", name)
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("%s 已启动，但等待网卡连接超时；如需要登录，请在官方客户端完成后重试", name)
}

func processRunning(ctx context.Context, process string) (bool, error) {
	if process == "" {
		return false, nil
	}
	if runtime.GOOS != "windows" {
		return false, nil
	}
	out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", "Get-Process -Name '"+escapePS(process)+"' -ErrorAction SilentlyContinue | Select-Object -First 1 -ExpandProperty ProcessName").Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) != "", nil
}

func adapterUp(ctx context.Context, keywords []string) (bool, error) {
	if runtime.GOOS != "windows" {
		return false, nil
	}
	out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", "Get-NetAdapter | Select-Object Name,InterfaceDescription,Status | ConvertTo-Csv -NoTypeInformation").Output()
	if err != nil {
		return false, err
	}
	text := strings.ToLower(string(out))
	for _, kw := range keywords {
		kw = strings.ToLower(strings.TrimSpace(kw))
		if kw != "" && strings.Contains(text, kw) {
			for _, line := range strings.Split(text, "\n") {
				if strings.Contains(line, kw) && strings.Contains(line, "up") {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

func escapePS(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
