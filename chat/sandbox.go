package chat

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/infinmalum/a1o-platform/common/config"
)

const (
	// sandboxOutputLimit caps combined stdout+stderr captured per run.
	sandboxOutputLimit = 64 << 10
	// sandboxMemoryLimit caps Python's address space in bytes; Node gets a
	// V8 heap cap (sandboxNodeHeapMB) instead because its VM reserves far
	// more virtual memory than it commits.
	sandboxMemoryLimit = int64(512 << 20)
	sandboxNodeHeapMB  = 512
	// sandboxFileSizeBytes caps files the run may create (8 MiB).
	sandboxFileSizeBytes = int64(8 << 20)
	// sandboxProcessLimit caps processes per run so fork bombs cannot
	// exhaust the host; sandboxOpenFileLimit caps open descriptors.
	sandboxProcessLimit  = 64
	sandboxOpenFileLimit = 256
)

// Sandbox executes model-generated code in a local subprocess. Isolation is
// process-level: rlimits (CPU, memory, file size, process count), a timeout,
// a fresh temp dir, and — unless the admin enables ChatSandboxNetworkEnabled —
// a network namespace with no connectivity. It still runs as the gateway's own
// UID, so deployments that enable the sandbox should run the gateway under a
// dedicated unprivileged user.
type Sandbox struct{}

// Languages returns the runtimes installed on this host.
func (s *Sandbox) Languages() []string {
	var languages []string
	if _, err := exec.LookPath("python3"); err == nil {
		languages = append(languages, "python")
	}
	if _, err := exec.LookPath("node"); err == nil {
		languages = append(languages, "javascript")
	}
	return languages
}

// Available reports whether any supported runtime exists.
func (s *Sandbox) Available() bool {
	return len(s.Languages()) > 0
}

// Execute runs the code and returns captured output; a non-nil error marks
// the run as failed for the model (timeout, non-zero exit, missing runtime).
func (s *Sandbox) Execute(ctx context.Context, language string, code string) (string, error) {
	bin, filename := "", ""
	switch language {
	case "python", "python3":
		bin, filename = "python3", "main.py"
	case "javascript", "js", "node":
		bin, filename = "node", "main.js"
	default:
		return "", fmt.Errorf("不支持的语言 %q", language)
	}
	if _, err := exec.LookPath(bin); err != nil {
		return "", fmt.Errorf("%s 运行时不可用", bin)
	}
	dir, err := os.MkdirTemp("", "chat-sandbox-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(code), 0600); err != nil {
		return "", err
	}

	timeout := config.ChatSandboxTimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	// RLIMIT_NPROC (-u) is accounted per user namespace on Linux 5.14+, so
	// the process cap only applies inside the unshare user namespace. On the
	// gateway's own namespace it would count every task of the gateway's
	// UID — including Node's startup threads — and break legitimate
	// runtimes on busy hosts; there, fork bombs stay bounded by the timeout
	// plus process-group kill instead.
	isolateNetwork := !config.ChatSandboxNetworkEnabled
	if isolateNetwork {
		if _, ok := networkNamespaceFlags(); !ok {
			return "", errors.New("沙箱网络隔离不可用（当前环境不支持 unshare）：请在管理后台开启沙箱网络访问或关闭沙箱")
		}
	}
	prlimitPath, lookupErr := exec.LookPath("prlimit")
	if lookupErr != nil {
		return "", errors.New("沙箱资源限制不可用（未安装 prlimit，属于 util-linux）：请安装 util-linux 后重试")
	}
	limits := []string{
		"--cpu=" + strconv.Itoa(timeout),
		"--fsize=" + strconv.Itoa(int(sandboxFileSizeBytes)),
		"--nofile=" + strconv.Itoa(sandboxOpenFileLimit),
	}
	if isolateNetwork {
		limits = append(limits, "--nproc="+strconv.Itoa(sandboxProcessLimit))
	}
	if bin == "python3" {
		limits = append(limits, "--as="+strconv.Itoa(int(sandboxMemoryLimit)))
	}
	// Resource limits ride on prlimit so no shell ever interprets a command
	// line; every value is an integer derived from compile-time constants.
	// prlimitPath is absolute (LookPath), so the run launches as a bare argv
	// list; watchProcessGroup owns the timeout kill.
	launchArgs := append([]string{prlimitPath}, limits...)
	launchArgs = append(launchArgs, "--", bin, filename)
	cmd := &exec.Cmd{Path: prlimitPath, Args: launchArgs}
	cmd.Dir = dir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + dir,
		"LANG=C.UTF-8",
		"PYTHONDONTWRITEBYTECODE=1",
	}
	if bin == "node" {
		// Node runs without an address-space rlimit (V8 reserves far more
		// memory than it commits), so cap its old-space heap instead.
		cmd.Env = append(cmd.Env, fmt.Sprintf("NODE_OPTIONS=--max-old-space-size=%d", sandboxNodeHeapMB))
	}
	if isolateNetwork {
		// Re-point the command at unshare so the whole run lands in a
		// network namespace with no connectivity. nsFlags is a fixed "-rn"
		// or "-n" from the one-time probe; nothing model-controlled ever
		// reaches the process list.
		nsFlags, _ := networkNamespaceFlags()
		unsharePath, lookupErr := exec.LookPath("unshare")
		if lookupErr != nil {
			return "", errors.New("沙箱网络隔离不可用（未安装 unshare）：请在管理后台开启沙箱网络访问或关闭沙箱")
		}
		cmd.Path = unsharePath
		cmd.Args = append([]string{"unshare", nsFlags}, cmd.Args...)
	}
	cmd.SysProcAttr = sandboxSysProcAttr()
	output := &limitedBuffer{limit: sandboxOutputLimit}
	cmd.Stdout = output
	cmd.Stderr = output
	// The watcher reads the pid through an atomic published after Start, so
	// it never races the Cmd's process field.
	var pid atomic.Int64
	watchProcessGroup(&pid, runCtx.Done())
	if err := cmd.Start(); err != nil {
		return output.String(), fmt.Errorf("进程启动失败: %v", err)
	}
	pid.Store(int64(cmd.Process.Pid))
	if err := cmd.Wait(); err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return output.String(), errors.New("代码执行超时")
		}
		return output.String(), fmt.Errorf("进程异常退出: %v", err)
	}
	if runCtx.Err() == context.Canceled || ctx.Err() == context.Canceled {
		return output.String(), errors.New("执行被取消")
	}
	return output.String(), nil
}

// sandboxContext bounds one tool call independently of the turn context.
func sandboxContext(ctx context.Context) (context.Context, context.CancelFunc) {
	seconds := config.ChatSandboxTimeoutSeconds
	if seconds <= 0 {
		seconds = 30
	}
	return context.WithTimeout(ctx, time.Duration(seconds+5)*time.Second)
}

// limitedBuffer captures at most limit bytes, discarding the rest.
type limitedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	room := l.limit - l.buf.Len()
	if room <= 0 {
		return len(p), nil
	}
	if len(p) > room {
		l.buf.Write(p[:room])
	} else {
		l.buf.Write(p)
	}
	return len(p), nil
}

func (l *limitedBuffer) String() string {
	out := l.buf.String()
	if l.buf.Len() >= l.limit {
		out += "\n[输出超过上限，已截断]"
	}
	return out
}
