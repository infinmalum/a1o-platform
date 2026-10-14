package chat

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/infinmalum/one-gateway/common/config"
)

const (
	// sandboxOutputLimit caps combined stdout+stderr captured per run.
	sandboxOutputLimit = 64 << 10
	// sandboxMemoryLimit caps Python's address space in bytes; Node's V8
	// reserves far more virtual memory than it commits, so it gets no AS
	// cap.
	sandboxMemoryLimit = int64(512 << 20)
	// sandboxFileSizeBytes caps files the run may create (8 MiB).
	sandboxFileSizeBytes = int64(8 << 20)
)

// Sandbox executes model-generated code in a local subprocess. Isolation is
// process-level (rlimits, timeout, fresh temp dir); it does not block
// network access, so the administrator option and per-session toggle gate it.
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
	// The ulimit preamble applies CPU, file-size, and (for Python, whose
	// runtimes tolerate address-space caps; Node's V8 does not) memory
	// limits, then execs the runtime so signals reach it directly.
	limits := fmt.Sprintf("ulimit -t %d; ulimit -f %d;", timeout, sandboxFileSizeBytes>>10)
	if bin == "python3" {
		limits += fmt.Sprintf(" ulimit -v %d;", sandboxMemoryLimit>>10)
	}
	cmd := exec.CommandContext(runCtx, "sh", "-c", limits+" exec "+bin+" "+filename)
	cmd.Dir = dir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + dir,
		"LANG=C.UTF-8",
		"PYTHONDONTWRITEBYTECODE=1",
	}
	cmd.SysProcAttr = sandboxSysProcAttr()
	output := &limitedBuffer{limit: sandboxOutputLimit}
	cmd.Stdout = output
	cmd.Stderr = output
	watchProcessGroup(cmd, runCtx.Done())
	if err := cmd.Run(); err != nil {
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
