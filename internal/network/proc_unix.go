//go:build !windows

package network

import "os/exec"

func configurePlayitWindow(_ *exec.Cmd) {}
