//go:build !linux

package chat

import (
	"os"
	"sync/atomic"
	"syscall"
)

// sandboxSysProcAttr has no process-group controls outside Linux; the
// timeout and output cap still apply.
func sandboxSysProcAttr() *syscall.SysProcAttr {
	return nil
}

// networkNamespaceFlags reports whether subprocesses can be launched without
// network access; user and network namespaces are Linux-only, so the sandbox
// fails closed unless the admin allows network access.
func networkNamespaceFlags() (string, bool) {
	return "", false
}

// watchProcessGroup kills the run when the context expires; without process
// groups (non-Linux) it can only signal the direct child.
func watchProcessGroup(pid *atomic.Int64, done <-chan struct{}) {
	go func() {
		<-done
		if value := pid.Load(); value != 0 {
			if process, err := os.FindProcess(int(value)); err == nil {
				_ = process.Kill()
			}
		}
	}()
}
