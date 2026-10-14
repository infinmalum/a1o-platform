//go:build linux

package chat

import (
	"os/exec"
	"syscall"
)

// sandboxSysProcAttr puts the run in its own process group so timeouts can
// kill the whole tree, and reaps the child if the gateway dies. Resource
// limits themselves come from the ulimit preamble in the launch script
// (sandbox.go), which stays portable across platforms.
func sandboxSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}
}

// watchProcessGroup kills the whole process group when the context expires;
// CommandContext only signals the direct child.
func watchProcessGroup(cmd *exec.Cmd, done <-chan struct{}) {
	go func() {
		<-done
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}()
}
