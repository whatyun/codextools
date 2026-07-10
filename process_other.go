//go:build !windows

package main

import (
	"errors"
	"os/exec"
	"syscall"
)

func hideSubprocessWindow(cmd *exec.Cmd) {}

func processIDRunning(pid int) (bool, error) {
	err := syscall.Kill(pid, 0)
	switch {
	case err == nil, errors.Is(err, syscall.EPERM):
		return true, nil
	case errors.Is(err, syscall.ESRCH):
		return false, nil
	default:
		return false, err
	}
}
