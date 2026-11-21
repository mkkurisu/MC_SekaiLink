//go:build !windows

package engine

import "os/exec"

func configureCmdWindow(_ *exec.Cmd) {}
