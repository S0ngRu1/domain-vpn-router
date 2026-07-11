package vpn

import (
	"context"
	"fmt"
	"log"
	"net"
	"runtime"
	"strings"
	"sync"
	"time"

	"domain-vpn-router/internal/config"
	"domain-vpn-router/internal/hiddenexec"
)

type Manager struct {
	cfg config.VPNConfig
	mu  sync.Mutex
}

func NewManager(cfg config.VPNConfig) *Manager {
	return &Manager{cfg: cfg}
}

func (m *Manager) EnsureTyty(ctx context.Context) error {
	return m.EnsureClashVerge(ctx)
}

func (m *Manager) EnsureClashVerge(ctx context.Context) error {
	return m.ensure(ctx, "Clash Verge", m.cfg.ClashVerge)
}

func (m *Manager) EnsureGlobalProtect(ctx context.Context) error {
	return m.ensure(ctx, "GlobalProtect", m.cfg.GlobalProtect)
}

func (m *Manager) StopTyty(ctx context.Context) error {
	return m.StopClashVerge(ctx)
}

func (m *Manager) StopClashVerge(ctx context.Context) error {
	return m.stop(ctx, "Clash Verge", m.cfg.ClashVerge)
}

func (m *Manager) StopGlobalProtect(ctx context.Context) error {
	return m.stop(ctx, "GlobalProtect", m.cfg.GlobalProtect)
}

func (m *Manager) TytyUp(ctx context.Context) bool {
	return m.ClashVergeUp(ctx)
}

func (m *Manager) ClashVergeUp(ctx context.Context) bool {
	return adapterUpFromInterfaces(m.cfg.ClashVerge.AdapterKeywords)
}

func (m *Manager) GlobalProtectUp(ctx context.Context) bool {
	return adapterUpFromInterfaces(m.cfg.GlobalProtect.AdapterKeywords)
}

func (m *Manager) ClashVergeUpPowerShell(ctx context.Context) bool {
	up, _ := adapterUpPowerShell(ctx, m.cfg.ClashVerge.AdapterKeywords)
	return up
}

func (m *Manager) GlobalProtectUpPowerShell(ctx context.Context) bool {
	up, _ := adapterUpPowerShell(ctx, m.cfg.GlobalProtect.AdapterKeywords)
	return up
}

func (m *Manager) GlobalProtectAdapterIP() net.IP {
	return adapterIPFromInterfaces(m.cfg.GlobalProtect.AdapterKeywords)
}

func (m *Manager) ensure(ctx context.Context, name string, endpoint config.VPNEndpoint) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if endpoint.Exe == "" {
		return fmt.Errorf("%s 未配置 exe", name)
	}
	if up, err := adapterUpPowerShell(ctx, endpoint.AdapterKeywords); err == nil && up {
		log.Printf("%s 网卡已可用", name)
		return nil
	}

	running, err := processRunning(ctx, endpoint.Process)
	if err != nil {
		log.Printf("检测 %s 进程失败，将尝试启动: %v", name, err)
	}
	if !running {
		log.Printf("正在启动 %s: %s", name, endpoint.Exe)
		if err := hiddenexec.CommandContext(ctx, endpoint.Exe).Start(); err != nil {
			return fmt.Errorf("启动 %s 失败: %w", name, err)
		}
	} else {
		log.Printf("%s 进程已存在，等待网卡连接", name)
	}

	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		up, err := adapterUpPowerShell(ctx, endpoint.AdapterKeywords)
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

func (m *Manager) stop(ctx context.Context, name string, endpoint config.VPNEndpoint) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if strings.TrimSpace(endpoint.StopCommand) != "" {
		log.Printf("正在运行 %s 关闭命令", name)
		return hiddenexec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", endpoint.StopCommand).Run()
	}
	if endpoint.Process == "" || runtime.GOOS != "windows" {
		return nil
	}
	log.Printf("正在尝试优雅关闭 %s", name)
	out, err := hiddenexec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", "$p=Get-Process -Name '"+escapePS(endpoint.Process)+"' -ErrorAction SilentlyContinue; if($p){ $p | ForEach-Object { $_.CloseMainWindow() | Out-Null } }").CombinedOutput()
	if err != nil {
		return fmt.Errorf("关闭 %s 失败: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func processRunning(ctx context.Context, process string) (bool, error) {
	if process == "" {
		return false, nil
	}
	if runtime.GOOS != "windows" {
		return false, nil
	}
	out, err := hiddenexec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", "Get-Process -Name '"+escapePS(process)+"' -ErrorAction SilentlyContinue | Select-Object -First 1 -ExpandProperty ProcessName").Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) != "", nil
}

func adapterUpPowerShell(ctx context.Context, keywords []string) (bool, error) {
	if runtime.GOOS != "windows" {
		return false, nil
	}
	if up := adapterUpFromInterfaces(keywords); up {
		return true, nil
	}
	out, err := hiddenexec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", "Get-NetAdapter | Select-Object Name,InterfaceDescription,Status | ConvertTo-Csv -NoTypeInformation").Output()
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

func adapterIPFromInterfaces(keywords []string) net.IP {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(iface.Name))
		for _, kw := range keywords {
			kw = strings.ToLower(strings.TrimSpace(kw))
			if kw == "" || !strings.Contains(name, kw) {
				continue
			}
			addrs, _ := iface.Addrs()
			for _, addr := range addrs {
				ip, _, err := net.ParseCIDR(addr.String())
				if err == nil && ip.To4() != nil && !ip.IsLoopback() {
					return ip
				}
			}
		}
	}
	return nil
}

func adapterUpFromInterfaces(keywords []string) bool {
	ifaces, err := net.Interfaces()
	if err != nil {
		return false
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(iface.Name))
		for _, kw := range keywords {
			kw = strings.ToLower(strings.TrimSpace(kw))
			if kw != "" && strings.Contains(name, kw) {
				return true
			}
		}
	}
	return false
}

func escapePS(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
