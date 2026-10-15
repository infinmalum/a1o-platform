package chat

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/infinmalum/one-gateway/common/config"
)

func skipWithoutPython(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("本机没有 python3，跳过")
	}
}

func TestSandboxAppliesFdLimitWhenNetworkAllowed(t *testing.T) {
	skipWithoutPython(t)
	previousNetwork := config.ChatSandboxNetworkEnabled
	config.ChatSandboxNetworkEnabled = true
	t.Cleanup(func() { config.ChatSandboxNetworkEnabled = previousNetwork })

	sandbox := &Sandbox{}
	output, err := sandbox.Execute(context.Background(), "python",
		"import resource\nprint(resource.getrlimit(resource.RLIMIT_NOFILE)[0])")
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !strings.Contains(output, "256") {
		t.Fatalf("fd limit not applied, output = %q", output)
	}
}

func TestSandboxAppliesProcessLimitWhenIsolated(t *testing.T) {
	skipWithoutPython(t)
	if _, ok := networkNamespaceFlags(); !ok {
		t.Skip("当前环境不支持 unshare 网络命名空间，跳过")
	}
	if config.ChatSandboxNetworkEnabled {
		t.Skip("默认网络隔离被全局配置关闭，跳过")
	}

	// RLIMIT_NPROC is accounted per user namespace, so inside unshare the
	// cap must hold regardless of how many tasks the gateway UID runs.
	sandbox := &Sandbox{}
	output, err := sandbox.Execute(context.Background(), "python",
		"import resource\nprint(resource.getrlimit(resource.RLIMIT_NPROC)[0], resource.getrlimit(resource.RLIMIT_NOFILE)[0])")
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !strings.Contains(output, "64 256") {
		t.Fatalf("rlimits not applied, output = %q", output)
	}
}

func TestSandboxNodeHeapCap(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("本机没有 node，跳过")
	}
	previousNetwork := config.ChatSandboxNetworkEnabled
	config.ChatSandboxNetworkEnabled = true
	t.Cleanup(func() { config.ChatSandboxNetworkEnabled = previousNetwork })

	sandbox := &Sandbox{}
	output, err := sandbox.Execute(context.Background(), "javascript", "console.log(process.env.NODE_OPTIONS)")
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if !strings.Contains(output, "--max-old-space-size=512") {
		t.Fatalf("node heap cap missing, output = %q", output)
	}
}

func TestSandboxBlocksNetworkByDefault(t *testing.T) {
	skipWithoutPython(t)
	if _, ok := networkNamespaceFlags(); !ok {
		t.Skip("当前环境不支持 unshare 网络命名空间，跳过")
	}
	if config.ChatSandboxNetworkEnabled {
		t.Skip("默认网络隔离被全局配置关闭，跳过")
	}

	sandbox := &Sandbox{}
	_, err := sandbox.Execute(context.Background(), "python",
		"import socket\nsocket.create_connection(('127.0.0.1', 9), timeout=2)\nprint('connected')")
	if err == nil {
		t.Fatal("沙箱默认应无网络连接，但 socket 连接成功了")
	}
	if strings.Contains(err.Error(), "网络隔离不可用") {
		t.Fatalf("unshare 探测通过但执行失败: %v", err)
	}
}
