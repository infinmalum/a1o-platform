package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/infinmalum/a1o-platform/common/config"
	"github.com/infinmalum/a1o-platform/model"
	"github.com/infinmalum/a1o-platform/relay/billing/ratio"
)

func setWebTools(t *testing.T, enabled bool) {
	t.Helper()
	previous := config.ChatWebToolsEnabled
	config.ChatWebToolsEnabled = enabled
	t.Cleanup(func() { config.ChatWebToolsEnabled = previous })
}

func defsContain(defs []Tool, name string) bool {
	for _, def := range defs {
		if def.Name == name {
			return true
		}
	}
	return false
}

func setMemoryTools(t *testing.T, enabled bool) {
	t.Helper()
	previous := config.ChatMemoryEnabled
	config.ChatMemoryEnabled = enabled
	t.Cleanup(func() { config.ChatMemoryEnabled = previous })
}

func TestAddNormaToolsWebToggle(t *testing.T) {
	setWebTools(t, true)
	setMemoryTools(t, false)
	set := newToolSet()
	addNormaTools(context.Background(), set, 0)
	if !defsContain(set.defs, "web_search") || !defsContain(set.defs, "WebFetch") {
		t.Fatalf("web tools missing with toggle on: %v", set.defs)
	}
	if len(set.sdk) != 2 {
		t.Fatalf("sdk tools not registered: %v", set.sdk)
	}

	setWebTools(t, false)
	set = newToolSet()
	addNormaTools(context.Background(), set, 0)
	if len(set.defs) != 0 || len(set.sdk) != 0 {
		t.Fatalf("web tools present with toggle off: %v", set.defs)
	}
}

func TestAddMemoryTools(t *testing.T) {
	setMemoryTools(t, true)
	setupExecutorDB(t)
	user := model.User{Username: "memory-opt-in", Password: "unused", AccessToken: "memory-opt-in-key", AffCode: "memory-opt-in-aff"}
	if err := model.DB.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { model.DB.Delete(&user) })
	set := newToolSet()
	addMemoryTools(t.Context(), set, user.Id)
	if len(set.sdk) != 0 {
		t.Fatal("memory must be off until enabled by the user")
	}
	if err := model.SetUserMemory(t.Context(), user.Id, true); err != nil {
		t.Fatal(err)
	}
	addMemoryTools(t.Context(), set, user.Id)
	if !defsContain(set.defs, "RecallMemory") || !defsContain(set.defs, "RecordMemory") {
		t.Fatalf("memory tools missing: %v", set.defs)
	}
	if len(set.sdk) != 2 {
		t.Fatalf("memory sdk tools not registered: %v", set.sdk)
	}

	if err := model.SetUserMemory(t.Context(), user.Id, false); err != nil {
		t.Fatal(err)
	}
	_, err := set.sdk["RecordMemory"].Call(t.Context(), json.RawMessage(`{"name":"test","content":"disabled"}`), nil)
	if !errors.Is(err, model.ErrMemoryDisabled) {
		t.Fatalf("stale memory tool: %v", err)
	}
	setMemoryTools(t, false)
	set = newToolSet()
	addMemoryTools(t.Context(), set, user.Id)
	if len(set.defs) != 0 || len(set.sdk) != 0 {
		t.Fatalf("memory tools present with toggle off: %v", set.defs)
	}
}

func TestUserMemoryStoreIsolation(t *testing.T) {
	a := model.UserMemoryStore(1)
	b := model.UserMemoryStore(2)
	if a.Dir == b.Dir {
		t.Fatalf("memory stores must be per-user: %s", a.Dir)
	}
	if !strings.Contains(a.Dir, filepath.Join("memory", "users", "1")) {
		t.Fatalf("unexpected memory dir: %s", a.Dir)
	}
}

func TestResolveWorkToolsTodosAndWeb(t *testing.T) {
	setWebTools(t, true)
	engine := &Engine{}
	set := engine.resolveWorkTools(context.Background(), WorkTurnRequest{
		Run: &model.AgentRun{UserId: 1},
	})
	if !defsContain(set.defs, "TodoWrite") {
		t.Fatalf("TodoWrite missing in work toolset: %v", set.defs)
	}
	if set.todos == nil {
		t.Fatalf("work toolset has no todo store")
	}
	if !defsContain(set.defs, "web_search") {
		t.Fatalf("web search missing in work toolset: %v", set.defs)
	}
}

func TestDefaultImageSize(t *testing.T) {
	for name, want := range map[string]string{
		"dall-e-3":                  "1024x1024",
		"ali-stable-diffusion-v1.5": "1024x1024",
	} {
		if got := defaultImageSize(name); got != want {
			t.Fatalf("defaultImageSize(%q) = %q, want %q", name, got, want)
		}
	}
	sizes := make([]string, 0)
	for size := range ratio.ImageSizeRatios["wanx-v1"] {
		sizes = append(sizes, size)
	}
	sort.Strings(sizes)
	if got := defaultImageSize("wanx-v1"); got != sizes[0] {
		t.Fatalf("defaultImageSize(wanx-v1) = %q, want %q", got, sizes[0])
	}
	if defaultImageSize("gpt-4o") != "" {
		t.Fatalf("non-image model should have no size")
	}
}

func TestGenerateImageArgValidation(t *testing.T) {
	exec := imageGenExec(0)
	cases := []string{
		`{"prompt":""}`,
		`{"prompt":"a cat","n":5}`,
		`{bad json`,
	}
	for _, args := range cases {
		result := exec(context.Background(), toolRequest{Arguments: args})
		if !result.IsError {
			t.Fatalf("expected error for args %s", args)
		}
	}
}

func TestValidatePublicURL(t *testing.T) {
	ctx := context.Background()
	for _, raw := range []string{
		"http://127.0.0.1/x",
		"http://[::1]/x",
		"http://10.0.0.1/x",
		"http://172.16.0.1/x",
		"http://192.168.1.1/x",
		"http://169.254.169.254/latest/meta-data/",
		"http://100.64.0.1/x",
		"http://0.0.0.0/x",
		"http://fd00::1/x",
		"http://fc00::1/x",
		"http://user:pass@example.com/x",
		"file:///etc/passwd",
		"http://localhost/x",
	} {
		if err := validatePublicURL(ctx, raw); err == nil {
			t.Fatalf("expected rejection for %s", raw)
		}
	}
	for _, raw := range []string{
		"https://8.8.8.8/x",
		"https://example.com/x",
	} {
		if err := validatePublicURL(ctx, raw); err != nil {
			t.Fatalf("expected acceptance for %s: %v", raw, err)
		}
	}
}
