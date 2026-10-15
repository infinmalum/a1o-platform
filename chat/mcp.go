package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sync/singleflight"

	"github.com/infinmalum/one-gateway/common/config"
	"github.com/infinmalum/one-gateway/model"
)

const (
	mcpConnectTimeout = 15 * time.Second
	mcpListTimeout    = 15 * time.Second
	mcpCallTimeout    = 120 * time.Second
	mcpMaxNameLength  = 64
)

// mcpFailureBackoff is how long a failed connection is remembered before the
// next retry, so a dead server does not stall every turn again.
const mcpFailureBackoff = 30 * time.Second

// MCPManager maintains one MCP client session per configured server, keyed by
// the row's update time so edits reconnect lazily. Connections are dialed
// outside the manager lock and collapsed per server version, so one slow or
// broken server only affects its own tools; recent failures fail fast.
type MCPManager struct {
	mu       sync.Mutex
	sessions map[string]*mcpSession
	failures map[string]mcpFailure
	connects singleflight.Group
}

type mcpSession struct {
	session *mcp.ClientSession
	tools   []*mcp.Tool
}

type mcpFailure struct {
	err      error
	failedAt time.Time
}

// NewMCPManager creates an empty manager.
func NewMCPManager() *MCPManager {
	return &MCPManager{
		sessions: map[string]*mcpSession{},
		failures: map[string]mcpFailure{},
	}
}

// MCPTool couples a server's tool with the virtual name the model sees.
type MCPTool struct {
	VirtualName string
	Server      *model.ChatMCPServer
	Tool        mcp.Tool
}

// ListTools returns the tools of every enabled server visible to the user,
// with stable virtual names and duplicates resolved by suffixing.
func (m *MCPManager) ListTools(ctx context.Context, userID int) []MCPTool {
	servers, err := model.ListEnabledMCPServers(userID)
	if err != nil {
		return nil
	}
	var tools []MCPTool
	used := map[string]bool{}
	for _, server := range servers {
		if server.OwnerId != model.ChatSkillOwnerGlobal && server.Transport != model.ChatMCPServerHTTP {
			// User-owned servers may not spawn local processes.
			continue
		}
		entry, err := m.ensure(ctx, server)
		if err != nil {
			continue
		}
		for _, tool := range entry.tools {
			name := VirtualToolName(server.Name, tool.Name, used)
			if name == "" {
				continue
			}
			used[name] = true
			tools = append(tools, MCPTool{VirtualName: name, Server: server, Tool: *tool})
		}
	}
	return tools
}

// CallTool invokes one tool on its server, returning the textual result and
// whether the server flagged it as an error.
func (m *MCPManager) CallTool(ctx context.Context, server *model.ChatMCPServer, toolName string, arguments string) (string, bool, error) {
	entry, err := m.ensure(ctx, server)
	if err != nil {
		return "", false, err
	}
	var args any = json.RawMessage(arguments)
	if strings.TrimSpace(arguments) == "" {
		args = map[string]any{}
	}
	callCtx, cancel := context.WithTimeout(ctx, mcpCallTimeout)
	defer cancel()
	result, err := entry.session.CallTool(callCtx, &mcp.CallToolParams{Name: toolName, Arguments: args})
	if err != nil {
		return "", false, err
	}
	return mcpContentText(result), result.IsError, nil
}

// ensure connects (or reuses) the session for one server and caches its tool
// listing. Concurrent connects for the same server version collapse into one
// dial, and a recent failure is returned without touching the network.
// Callers must treat err as "this server is unavailable".
func (m *MCPManager) ensure(ctx context.Context, server *model.ChatMCPServer) (*mcpSession, error) {
	key := fmt.Sprintf("%d@%d", server.Id, server.UpdatedTime)
	if entry, failure, ok := m.cacheLookup(key); ok {
		if entry != nil {
			return entry, nil
		}
		return nil, failure
	}
	result, err, _ := m.connects.Do(key, func() (any, error) {
		// The flight may have started after a previous one finished; serve
		// its outcome instead of dialing again.
		if entry, failure, ok := m.cacheLookup(key); ok {
			if entry != nil {
				return entry, nil
			}
			return nil, failure
		}
		return m.connect(ctx, server, key)
	})
	if err != nil {
		return nil, err
	}
	return result.(*mcpSession), nil
}

// cacheLookup checks the session and failure caches under the manager lock.
// ok is false when the key needs a fresh dial; otherwise exactly one of
// entry/failure is set.
func (m *MCPManager) cacheLookup(key string) (entry *mcpSession, failure error, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry, cached := m.sessions[key]; cached {
		return entry, nil, true
	}
	if failure, cached := m.failures[key]; cached {
		if time.Since(failure.failedAt) < mcpFailureBackoff {
			return nil, failure.err, true
		}
		delete(m.failures, key)
	}
	return nil, nil, false
}

// connect dials one server version. It runs outside the manager lock; only
// cache updates take the lock. The dial context is detached from the calling
// turn so a cancelled turn cannot abort a connection other turns are waiting
// on; the connect/list timeouts bound the work.
func (m *MCPManager) connect(ctx context.Context, server *model.ChatMCPServer, key string) (*mcpSession, error) {
	prefix := fmt.Sprintf("%d@", server.Id)
	transport, err := transportFor(server)
	if err != nil {
		m.recordFailure(key, err)
		return nil, err
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "one-gateway-chat", Version: "1.0"}, nil)
	dialCtx := context.WithoutCancel(ctx)
	connectCtx, cancelConnect := context.WithTimeout(dialCtx, mcpConnectTimeout)
	defer cancelConnect()
	session, err := client.Connect(connectCtx, transport, nil)
	if err != nil {
		err = fmt.Errorf("MCP 服务器 %s 连接失败: %w", server.Name, err)
		m.recordFailure(key, err)
		return nil, err
	}
	listCtx, cancelList := context.WithTimeout(dialCtx, mcpListTimeout)
	defer cancelList()
	listing, err := session.ListTools(listCtx, nil)
	if err != nil {
		_ = session.Close()
		err = fmt.Errorf("MCP 服务器 %s 工具列表失败: %w", server.Name, err)
		m.recordFailure(key, err)
		return nil, err
	}
	entry := &mcpSession{session: session, tools: listing.Tools}
	m.mu.Lock()
	m.sessions[key] = entry
	var stale []*mcpSession
	for existing, old := range m.sessions {
		if existing != key && strings.HasPrefix(existing, prefix) {
			stale = append(stale, old)
			delete(m.sessions, existing)
		}
	}
	delete(m.failures, key)
	m.mu.Unlock()
	for _, old := range stale {
		_ = old.session.Close()
	}
	return entry, nil
}

// recordFailure remembers a connection failure for the backoff window and
// drops entries that already expired so the cache stays bounded.
func (m *MCPManager) recordFailure(key string, err error) {
	now := time.Now()
	m.mu.Lock()
	for existing, failure := range m.failures {
		if now.Sub(failure.failedAt) >= mcpFailureBackoff {
			delete(m.failures, existing)
		}
	}
	m.failures[key] = mcpFailure{err: err, failedAt: now}
	m.mu.Unlock()
}

// mcpEnvAllowlist is the set of environment variables stdio MCP servers may
// inherit from the gateway. It deliberately excludes anything that can carry
// secrets (credential master keys, SQL DSN, session secret, proxy
// credentials); admins who need more can pass extra vars through the server's
// own Env field.
var mcpEnvAllowlist = []string{
	"PATH",
	"HOME",
	"TMPDIR",
	"LANG",
	"LC_ALL",
	"TZ",
	"SSL_CERT_FILE",
	"SSL_CERT_DIR",
	"NODE_EXTRA_CA_CERTS",
}

// mcpChildEnv builds the filtered environment for a stdio MCP child process.
func mcpChildEnv() []string {
	var env []string
	for _, name := range mcpEnvAllowlist {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	return env
}

// transportFor builds the MCP transport for one server row.
func transportFor(server *model.ChatMCPServer) (mcp.Transport, error) {
	switch server.Transport {
	case model.ChatMCPServerStdio:
		// A stdio server launches an administrator-configured command on the
		// gateway host. Launches are gated behind ChatMCPStdioEnabled and the
		// ChatMCPStdioAllowlist, the command resolves through LookPath, and
		// arguments pass as argv — never through a shell.
		if !config.ChatMCPStdioEnabled {
			return nil, errors.New("stdio MCP 服务器已被禁用：请在管理后台开启 ChatMCPStdioEnabled 后再配置命令行服务器")
		}
		if strings.TrimSpace(server.Command) == "" {
			return nil, fmt.Errorf("MCP 服务器 %s 缺少命令", server.Name)
		}
		resolved, err := exec.LookPath(strings.TrimSpace(server.Command))
		if err != nil {
			return nil, fmt.Errorf("MCP 服务器 %s 的命令不可用: %w", server.Name, err)
		}
		if !mcpStdioCommandAllowed(resolved) {
			return nil, fmt.Errorf("MCP 服务器 %s 的命令 %q 不在 ChatMCPStdioAllowlist 允许列表中", server.Name, resolved)
		}
		// resolved is an absolute path from LookPath; the child is launched
		// from an argv list without any shell interpretation.
		cmd := &exec.Cmd{
			Path: resolved,
			Args: append([]string{resolved}, decodeStringList(server.Args)...),
			Env:  append(mcpChildEnv(), decodeStringList(server.Env)...),
		}
		return &mcp.CommandTransport{Command: cmd}, nil
	case model.ChatMCPServerHTTP:
		if strings.TrimSpace(server.URL) == "" {
			return nil, fmt.Errorf("MCP 服务器 %s 缺少 URL", server.Name)
		}
		return &mcp.StreamableClientTransport{
			Endpoint:             server.URL,
			HTTPClient:           &http.Client{Transport: &headerTransport{base: http.DefaultTransport, headers: decodeStringMap(server.Headers)}},
			DisableStandaloneSSE: true,
		}, nil
	default:
		return nil, fmt.Errorf("未知的 MCP 传输类型 %q", server.Transport)
	}
}

// mcpStdioCommandAllowed reports whether the resolved stdio server command is
// permitted by the ChatMCPStdioAllowlist. Entries are comma-separated
// executable names matched against the resolved path's base name; "*" allows
// every command and an empty list allows none.
func mcpStdioCommandAllowed(resolved string) bool {
	base := filepath.Base(resolved)
	for _, entry := range strings.Split(config.ChatMCPStdioAllowlist, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "*" || entry == base {
			return true
		}
	}
	return false
}

// headerTransport attaches configured headers to every request.
type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	for name, value := range t.headers {
		clone.Header.Set(name, value)
	}
	return t.base.RoundTrip(clone)
}

// mcpContentText flattens a tool result into the text fed back to the model.
func mcpContentText(result *mcp.CallToolResult) string {
	if result == nil {
		return ""
	}
	var parts []string
	for _, content := range result.Content {
		switch value := content.(type) {
		case *mcp.TextContent:
			parts = append(parts, value.Text)
		default:
			raw, err := json.Marshal(content)
			if err == nil {
				parts = append(parts, string(raw))
			}
		}
	}
	if len(parts) == 0 && result.StructuredContent != nil {
		raw, err := json.Marshal(result.StructuredContent)
		if err == nil {
			parts = append(parts, string(raw))
		}
	}
	return strings.Join(parts, "\n")
}

// VirtualToolName maps (server, tool) to a model-safe function name:
// letters, digits, underscore and dash only, capped at 64 chars. used holds
// names already taken; empty means the name collapsed to nothing.
func VirtualToolName(serverName string, toolName string, used map[string]bool) string {
	base := "mcp_" + slugify(serverName) + "_" + slugify(toolName)
	base = strings.Trim(base, "_-")
	if base == "" {
		return ""
	}
	if len(base) > mcpMaxNameLength {
		base = base[:mcpMaxNameLength]
	}
	name := base
	for i := 2; used[name]; i++ {
		suffix := fmt.Sprintf("_%d", i)
		cut := mcpMaxNameLength - len(suffix)
		if cut > len(base) {
			cut = len(base)
		}
		name = base[:cut] + suffix
	}
	return name
}

// slugify replaces every character outside [a-zA-Z0-9_-] with an underscore.
func slugify(raw string) string {
	var out strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			out.WriteRune(r)
		default:
			out.WriteByte('_')
		}
	}
	return out.String()
}

// decodeStringList parses a JSON string array, tolerating nil.
func decodeStringList(raw *string) []string {
	if raw == nil || *raw == "" {
		return nil
	}
	var values []string
	if err := json.Unmarshal([]byte(*raw), &values); err != nil {
		return nil
	}
	return values
}

// decodeStringMap parses a JSON string object, tolerating nil.
func decodeStringMap(raw *string) map[string]string {
	if raw == nil || *raw == "" {
		return nil
	}
	var values map[string]string
	if err := json.Unmarshal([]byte(*raw), &values); err != nil {
		return nil
	}
	return values
}
