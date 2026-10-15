//go:build linux

package chat

import (
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
)

// sandboxSysProcAttr puts the run in its own process group so timeouts can
// kill the whole tree, and reaps the child if the gateway dies. Resource
// limits themselves come from prlimit flags on the launch command
// (sandbox.go), which stays portable across platforms.
func sandboxSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}
}

var (
	networkNSOnce      sync.Once
	networkNSFlags     string
	networkNSAvailable bool
)

// networkNamespaceFlags reports whether this host can launch subprocesses
// with no network access (a fresh network namespace via unshare) and returns
// the unshare flag string to use. "-rn" wraps the network namespace in a
// user namespace so unprivileged gateways can use it too; plain "-n" needs
// CAP_SYS_ADMIN. The probe runs once and its result is cached.
func networkNamespaceFlags() (string, bool) {
	networkNSOnce.Do(func() {
		if err := exec.Command("unshare", "-rn", "true").Run(); err == nil {
			networkNSFlags, networkNSAvailable = "-rn", true
			return
		}
		if err := exec.Command("unshare", "-n", "true").Run(); err == nil {
			networkNSFlags, networkNSAvailable = "-n", true
		}
	})
	return networkNSFlags, networkNSAvailable
}

// watchProcessGroup kills the whole process group when the context expires;
// CommandContext only signals the direct child. The group-leading pid is
// published by the caller through the atomic after Start returns, so the
// watcher never races the Cmd's process field.
func watchProcessGroup(pid *atomic.Int64, done <-chan struct{}) {
	go func() {
		<-done
		if value := pid.Load(); value != 0 {
			_ = syscall.Kill(-int(value), syscall.SIGKILL)
		}
	}()
}
