//go:build windows

package network

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

func configurePlayitWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}
