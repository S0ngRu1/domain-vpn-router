//go:build !windows

package gui

import (
	"context"

	"domain-vpn-router/internal/app"
)

func Run(ctx context.Context, controller *app.Controller, showWindow bool) error {
	if err := controller.Start(ctx); err != nil {
		return err
	}
	<-ctx.Done()
	shutdownCtx, cancel := app.ShutdownContext()
	defer cancel()
	return controller.Shutdown(shutdownCtx)
}
