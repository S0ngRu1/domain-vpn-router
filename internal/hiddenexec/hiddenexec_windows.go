//go:build windows

package hiddenexec

import (
	"context"
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

func Command(name string, arg ...string) *exec.Cmd {
	cmd := exec.Command(name, arg...)
	hideWindow(cmd)
	return cmd
}

func CommandContext(ctx context.Context, name string, arg ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, arg...)
	hideWindow(cmd)
	return cmd
}

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}
