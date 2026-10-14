//go:build !linux

package chat

import (
	"os/exec"
	"syscall"
)

// sandboxSysProcAttr has no process-group controls outside Linux; the
// timeout and output cap still apply.
func sandboxSysProcAttr() *syscall.SysProcAttr {
	return nil
}

// watchProcessGroup is a no-op without process groups.
func watchProcessGroup(cmd *exec.Cmd, done <-chan struct{}) {}
