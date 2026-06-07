package app

import (
	"context"
	"fmt"
	"log"
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
	ModeTyty          Mode = "tyty"
	ModeGlobalProtect Mode = "globalprotect"
	ModeDirect        Mode = "direct"
)

type Status struct {
	Mode          Mode
	ProxyListen   string
	ProxyRunning  bool
	SystemProxyOn bool
	TytyUp        bool
	GlobalUp      bool
	LastError     string
	Logs          []string
}

type Controller struct {
	cfg       config.Config
	statePath string
	matcher   *rules.Matcher
	manager   *vpn.Manager
	proxy     *proxy.Server
	logs      *LogBuffer

	mu            sync.Mutex
	mode          Mode
	proxyRunning  bool
	systemProxyOn bool
	lastError     string
	started       bool
	shutdownOnce  sync.Once
}

func NewController(cfg config.Config, statePath string) *Controller {
	mode := NormalizeMode(cfg.App.StartMode)
	return &Controller{
		cfg:       cfg,
		statePath: statePath,
		matcher:   rules.NewMatcher(cfg.Rules),
		manager:   vpn.NewManager(cfg.VPN),
		logs:      NewLogBuffer(100),
		mode:      mode,
	}
}

func NormalizeMode(mode string) Mode {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case string(ModeTyty):
		return ModeTyty
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
	c.proxy = proxy.New(c.cfg.Proxy.Listen, c.cfg.Proxy.DirectBindIP, c)
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
			c.setError("代理服务失败: %v", err)
		}
	}()
	c.setProxyRunning(true)

	if c.Mode() == ModeTyty || c.Mode() == ModeGlobalProtect {
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
	case ModeTyty:
		if match.Action != rules.ActionDirect {
			match = rules.Match{Action: rules.ActionForeign, Rule: "manual-tyty"}
		}
	case ModeGlobalProtect:
		if match.Action != rules.ActionDirect {
			match = rules.Match{Action: rules.ActionCompany, Rule: "manual-globalprotect"}
		}
	case ModeDirect:
		match = rules.Match{Action: rules.ActionDirect, Rule: "manual-direct"}
	}

	c.addLog("访问目标=%s 动作=%s 规则=%s 模式=%s", target, match.Action, match.Rule, mode)
	switch match.Action {
	case rules.ActionCompany:
		if err := c.manager.EnsureGlobalProtect(ctx); err != nil {
			c.setError("切换 GlobalProtect 失败: %v", err)
			return match, err
		}
	case rules.ActionForeign:
		if err := c.manager.EnsureTyty(ctx); err != nil {
			c.setError("切换 Tyty 失败: %v", err)
			return match, err
		}
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
	case ModeTyty:
		if err := c.EnableProxy(); err != nil {
			return err
		}
		_ = c.manager.StopGlobalProtect(ctx)
		return c.manager.EnsureTyty(ctx)
	case ModeGlobalProtect:
		if err := c.EnableProxy(); err != nil {
			return err
		}
		_ = c.manager.StopTyty(ctx)
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
	c.mu.Lock()
	status := Status{
		Mode:          c.mode,
		ProxyListen:   c.cfg.Proxy.Listen,
		ProxyRunning:  c.proxyRunning,
		SystemProxyOn: c.systemProxyOn,
		LastError:     c.lastError,
		Logs:          c.logs.Entries(),
	}
	c.mu.Unlock()
	status.TytyUp = c.manager.TytyUp(ctx)
	status.GlobalUp = c.manager.GlobalProtectUp(ctx)
	return status
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
		fmt.Sprintf("Tyty 网卡: %v", status.TytyUp),
		fmt.Sprintf("GlobalProtect 网卡: %v", status.GlobalUp),
	}
	if status.LastError != "" {
		lines = append(lines, fmt.Sprintf("最近错误: %s", status.LastError))
	}
	lines = append(lines, "", "最近日志:")
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

func ShutdownContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}
