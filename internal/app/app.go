package app

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"domain-vpn-router/internal/config"
	"domain-vpn-router/internal/proxy"
	"domain-vpn-router/internal/rules"
	"domain-vpn-router/internal/systemproxy"
	"domain-vpn-router/internal/vpn"
)

type Mode string

const (
	ModeAuto          Mode = "auto"
	ModeClash         Mode = "clash"
	ModeTyty          Mode = ModeClash
	ModeGlobalProtect Mode = "globalprotect"
	ModeDirect        Mode = "direct"
)

type Status struct {
	Mode          Mode
	ProxyListen   string
	DirectBindIP  string
	PhysicalIP    string
	ProxyRunning  bool
	SystemProxyOn bool
	ClashUp       bool
	TytyUp        bool
	GlobalUp      bool
	LastError     string
	RefreshedAt   time.Time
	Logs          []string
}

type Controller struct {
	cfg        config.Config
	configPath string
	statePath  string
	matcher    *rules.Matcher
	manager    *vpn.Manager
	proxy      *proxy.Server
	logs       *LogBuffer

	mu            sync.Mutex
	mode          Mode
	proxyRunning  bool
	systemProxyOn bool
	lastError     string
	physicalIP    string
	refreshedAt   time.Time
	started       bool
	shutdownOnce  sync.Once
}

func NewController(cfg config.Config, configPath, statePath string) *Controller {
	mode := NormalizeMode(cfg.App.StartMode)
	return &Controller{
		cfg:        cfg,
		configPath: configPath,
		statePath:  statePath,
		matcher:    rules.NewMatcher(cfg.Rules),
		manager:    vpn.NewManager(cfg.VPN),
		logs:       NewLogBuffer(100),
		mode:       mode,
	}
}

func NormalizeMode(mode string) Mode {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case string(ModeClash), "clash-verge", "clashverge", "tyty":
		return ModeClash
	case string(ModeGlobalProtect), "gp", "global":
		return ModeGlobalProtect
	case string(ModeDirect):
		return ModeDirect
	default:
		return ModeAuto
	}
}

func (c *Controller) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return nil
	}
	excludeAdapters := append(
		append([]string(nil), c.cfg.VPN.ClashVerge.AdapterKeywords...),
		c.cfg.VPN.GlobalProtect.AdapterKeywords...,
	)
	c.proxy = proxy.New(c.cfg.Proxy.Listen, c.cfg.Proxy.DirectBindIP, c.cfg.Proxy.ForeignProxy, c, excludeAdapters)
	c.physicalIP = c.proxy.CurrentPhysicalIPStr()
	c.refreshedAt = time.Now()
	c.started = true
	c.mu.Unlock()

	if c.Mode() != ModeDirect {
		if err := systemproxy.Enable(c.cfg.Proxy.Listen, c.statePath); err != nil {
			c.setError("设置系统代理失败: %v", err)
			return err
		}
		c.setSystemProxyOn(true)
	}

	go func() {
		c.addLog("域名分流代理已启动: %s", c.cfg.Proxy.Listen)
		if err := c.proxy.ListenAndServe(); err != nil {
			c.setProxyRunning(false)
			c.setError("代理服务失败: %v", err)
		}
	}()
	c.setProxyRunning(true)

	if c.Mode() == ModeClash || c.Mode() == ModeGlobalProtect {
		go func() {
			if err := c.ApplyMode(context.Background(), c.Mode()); err != nil {
				c.setError("启动初始模式失败: %v", err)
			}
		}()
	}
	return nil
}

func (c *Controller) Shutdown(ctx context.Context) error {
	var err error
	c.shutdownOnce.Do(func() {
		c.addLog("正在退出")
		c.mu.Lock()
		p := c.proxy
		c.mu.Unlock()
		if p != nil {
			if e := p.Shutdown(ctx); e != nil {
				err = e
			}
		}
		if e := c.RestoreProxy(); e != nil && err == nil {
			err = e
		}
	})
	return err
}

func (c *Controller) Route(ctx context.Context, target string) (rules.Match, error) {
	mode := c.Mode()
	match := c.matcher.Match(target)

	switch mode {
	case ModeClash:
		if !rules.IsLocalDirect(match) {
			match = rules.Match{Action: rules.ActionForeign, Rule: "manual-clash"}
		}
	case ModeGlobalProtect:
		if !rules.IsLocalDirect(match) {
			match = rules.Match{Action: rules.ActionCompany, Rule: "manual-globalprotect"}
		}
	case ModeDirect:
		match = rules.Match{Action: rules.ActionDirect, Rule: "manual-direct"}
	}

	switch match.Action {
	case rules.ActionCompany:
		return match, nil
	case rules.ActionForeign:
		if strings.TrimSpace(c.cfg.Proxy.ForeignProxy) != "" {
			return match, nil
		}
		err := fmt.Errorf("foreign traffic requires proxy.foreign_proxy for Clash Verge")
		c.setError("Clash Verge 上游代理未配置: %v", err)
		return match, err
	}
	return match, nil
}

func (c *Controller) ApplyMode(ctx context.Context, mode Mode) error {
	mode = NormalizeMode(string(mode))
	c.setMode(mode)
	c.addLog("切换模式: %s", mode)

	switch mode {
	case ModeDirect:
		return c.RestoreProxy()
	case ModeAuto:
		return c.EnableProxy()
	case ModeClash:
		if err := c.EnableProxy(); err != nil {
			return err
		}
		return c.manager.EnsureClashVerge(ctx)
	case ModeGlobalProtect:
		if err := c.EnableProxy(); err != nil {
			return err
		}
		return c.manager.EnsureGlobalProtect(ctx)
	default:
		return fmt.Errorf("未知模式: %s", mode)
	}
}

func (c *Controller) EnableProxy() error {
	if err := systemproxy.Enable(c.cfg.Proxy.Listen, c.statePath); err != nil {
		c.setError("设置系统代理失败: %v", err)
		return err
	}
	c.setSystemProxyOn(true)
	c.addLog("系统代理已启用: %s", c.cfg.Proxy.Listen)
	return nil
}

func (c *Controller) RestoreProxy() error {
	if err := systemproxy.Restore(c.statePath); err != nil {
		c.setError("恢复系统代理失败: %v", err)
		return err
	}
	c.setSystemProxyOn(false)
	c.addLog("系统代理已恢复")
	return nil
}

func (c *Controller) Mode() Mode {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.mode
}

func (c *Controller) Status(ctx context.Context) Status {
	status := c.StatusSnapshot()
	status.ClashUp = c.manager.ClashVergeUp(ctx)
	status.TytyUp = status.ClashUp
	status.GlobalUp = c.manager.GlobalProtectUp(ctx)
	return status
}

func (c *Controller) RefreshStatus(ctx context.Context) Status {
	clashUp := c.manager.ClashVergeUpPowerShell(ctx)
	globalUp := c.manager.GlobalProtectUpPowerShell(ctx)
	physicalIP := c.refreshPhysicalIP()
	c.mu.Lock()
	c.physicalIP = physicalIP
	c.refreshedAt = time.Now()
	c.mu.Unlock()
	status := c.StatusSnapshot()
	status.ClashUp = clashUp
	status.TytyUp = clashUp
	status.GlobalUp = globalUp
	return status
}

func (c *Controller) StatusSnapshot() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Status{
		Mode:          c.mode,
		ProxyListen:   c.cfg.Proxy.Listen,
		DirectBindIP:  c.cfg.Proxy.DirectBindIP,
		PhysicalIP:    c.physicalIP,
		ProxyRunning:  c.proxyRunning,
		SystemProxyOn: c.systemProxyOn,
		LastError:     c.lastError,
		RefreshedAt:   c.refreshedAt,
		Logs:          c.logs.Entries(),
	}
}

func (c *Controller) CompanyAdapterIP() net.IP {
	return c.manager.GlobalProtectAdapterIP()
}

func (c *Controller) PhysicalAdapterIP() string {
	ip := c.refreshPhysicalIP()
	c.mu.Lock()
	c.physicalIP = ip
	c.refreshedAt = time.Now()
	c.mu.Unlock()
	return ip
}

func (c *Controller) refreshPhysicalIP() string {
	c.mu.Lock()
	p := c.proxy
	c.mu.Unlock()
	if p != nil {
		return p.RefreshPhysicalIP()
	}
	excludeAdapters := append(
		append([]string(nil), c.cfg.VPN.ClashVerge.AdapterKeywords...),
		c.cfg.VPN.GlobalProtect.AdapterKeywords...,
	)
	return proxy.DynamicPhysicalIPStr(excludeAdapters)
}

func (c *Controller) DirectBindIP() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cfg.Proxy.DirectBindIP
}

func (c *Controller) UpdateDirectBindIP(value string) error {
	value = strings.TrimSpace(value)
	c.mu.Lock()
	c.cfg.Proxy.DirectBindIP = value
	p := c.proxy
	configPath := c.configPath
	c.mu.Unlock()
	if p != nil {
		p.SetDirectBindIP(value)
	}
	c.mu.Lock()
	if p != nil {
		c.physicalIP = p.CurrentPhysicalIPStr()
	} else {
		c.physicalIP = value
	}
	c.refreshedAt = time.Now()
	c.mu.Unlock()
	if configPath != "" {
		if err := config.UpdateProxyDirectBindIP(configPath, value); err != nil {
			c.setError("保存直连绑定 IP 失败: %v", err)
			return err
		}
	}
	c.addLog("直连绑定 IP 已更新: %s", value)
	return nil
}

func (c *Controller) StatusText(ctx context.Context) string {
	status := c.Status(ctx)
	lines := []string{
		"Domain VPN Router",
		"",
		fmt.Sprintf("当前模式: %s", status.Mode),
		fmt.Sprintf("代理监听: %s", status.ProxyListen),
		fmt.Sprintf("代理运行: %v", status.ProxyRunning),
		fmt.Sprintf("系统代理: %v", status.SystemProxyOn),
		fmt.Sprintf("物理网卡 IP: %s", emptyStatusText(status.PhysicalIP, "未检测")),
		fmt.Sprintf("网卡状态刷新: %s", formatStatusTime(status.RefreshedAt)),
		fmt.Sprintf("Clash Verge 网卡: %v", status.ClashUp),
		fmt.Sprintf("GlobalProtect 网卡: %v", status.GlobalUp),
	}
	if status.LastError != "" {
		lines = append(lines, fmt.Sprintf("最近错误: %s", status.LastError))
	}
	lines = append(lines, "", "最近日志")
	logs := status.Logs
	if len(logs) > 20 {
		logs = logs[len(logs)-20:]
	}
	lines = append(lines, logs...)
	return strings.Join(lines, "\r\n")
}

func (c *Controller) setMode(mode Mode) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mode = mode
	c.lastError = ""
}

func (c *Controller) setProxyRunning(running bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.proxyRunning = running
}

func (c *Controller) setSystemProxyOn(on bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.systemProxyOn = on
}

func (c *Controller) setError(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	c.mu.Lock()
	c.lastError = msg
	c.mu.Unlock()
	c.logs.Add("%s", msg)
	log.Print(msg)
}

func (c *Controller) addLog(format string, args ...any) {
	c.logs.Add(format, args...)
}

func emptyStatusText(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func formatStatusTime(t time.Time) string {
	if t.IsZero() {
		return "尚未刷新"
	}
	return t.Format("15:04:05")
}

func ShutdownContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}
