package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"domain-vpn-router/internal/config"
	"domain-vpn-router/internal/proxy"
	"domain-vpn-router/internal/rules"
	"domain-vpn-router/internal/systemproxy"
	"domain-vpn-router/internal/vpn"
)

func main() {
	configPath := flag.String("config", "config.yaml", "配置文件路径")
	statePath := flag.String("state", ".router-proxy-state.json", "系统代理备份文件路径")
	flag.Parse()

	if flag.NArg() > 0 && flag.Arg(0) == "restore-proxy" {
		if err := systemproxy.Restore(*statePath); err != nil {
			log.Fatalf("恢复系统代理失败: %v", err)
		}
		log.Printf("系统代理已恢复")
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("读取配置失败: %v", err)
	}
	matcher := rules.NewMatcher(cfg.Rules)
	manager := vpn.NewManager(cfg.VPN)

	if err := systemproxy.Enable(cfg.Proxy.Listen, *statePath); err != nil {
		log.Fatalf("设置系统代理失败: %v", err)
	}
	defer func() {
		if err := systemproxy.Restore(*statePath); err != nil {
			log.Printf("恢复系统代理失败，请手动运行 restore-proxy: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	p := proxy.New(cfg.Proxy.Listen, cfg.Proxy.DirectBindIP, matcher, manager)
	errc := make(chan error, 1)
	go func() {
		log.Printf("域名分流代理已启动: %s", cfg.Proxy.Listen)
		log.Printf("退出时会自动恢复系统代理；异常退出后可运行: %s -state %s restore-proxy", exeName(), *statePath)
		errc <- p.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		log.Printf("收到退出信号，正在关闭代理")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := p.Shutdown(shutdownCtx); err != nil {
			log.Printf("关闭代理失败: %v", err)
		}
	case err := <-errc:
		if err != nil {
			fmt.Fprintf(os.Stderr, "代理服务失败: %v\n", err)
			os.Exit(1)
		}
	}
}

func exeName() string {
	if len(os.Args) == 0 || os.Args[0] == "" {
		return "domain-vpn-router.exe"
	}
	return os.Args[0]
}
