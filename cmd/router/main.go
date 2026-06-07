package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os/signal"
	"syscall"

	"domain-vpn-router/internal/app"
	"domain-vpn-router/internal/config"
	"domain-vpn-router/internal/gui"
	"domain-vpn-router/internal/systemproxy"
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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	controller := app.NewController(cfg, *statePath)
	if err := gui.Run(ctx, controller, cfg.App.ShowWindowOnStart); err != nil {
		fmt.Printf("程序运行失败: %v\n", err)
	}
}
