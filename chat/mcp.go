package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/infinmalum/one-gateway/model"
)

const (
	mcpConnectTimeout = 15 * time.Second
	mcpListTimeout    = 15 * time.Second
	mcpCallTimeout    = 120 * time.Second
	mcpMaxNameLength  = 64
)

// MCPManager maintains one MCP client session per configured server, keyed by
// the row's update time so edits reconnect lazily. Failing servers are
// skipped; they never block a turn.
type MCPManager struct {
	mu       sync.Mutex
	sessions map[string]*mcpSession
}

type mcpSession struct {
	session *mcp.ClientSession
	tools   []*mcp.Tool
}

// NewMCPManager creates an empty manager.
func NewMCPManager() *MCPManager {
	return &MCPManager{sessions: map[string]*mcpSession{}}
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
// listing. Callers must treat err as "this server is unavailable".
func (m *MCPManager) ensure(ctx context.Context, server *model.ChatMCPServer) (*mcpSession, error) {
	key := fmt.Sprintf("%d@%d", server.Id, server.UpdatedTime)
	prefix := fmt.Sprintf("%d@", server.Id)
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry, ok := m.sessions[key]; ok {
		return entry, nil
	}
	for existing, entry := range m.sessions {
		if strings.HasPrefix(existing, prefix) {
			_ = entry.session.Close()
			delete(m.sessions, existing)
		}
	}
	transport, err := transportFor(server)
	if err != nil {
		return nil, err
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "one-gateway-chat", Version: "1.0"}, nil)
	connectCtx, cancelConnect := context.WithTimeout(ctx, mcpConnectTimeout)
	defer cancelConnect()
	session, err := client.Connect(connectCtx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("MCP 服务器 %s 连接失败: %w", server.Name, err)
	}
	listCtx, cancelList := context.WithTimeout(ctx, mcpListTimeout)
	defer cancelList()
	listing, err := session.ListTools(listCtx, nil)
	if err != nil {
		_ = session.Close()
		return nil, fmt.Errorf("MCP 服务器 %s 工具列表失败: %w", server.Name, err)
	}
	entry := &mcpSession{session: session, tools: listing.Tools}
	m.sessions[key] = entry
	return entry, nil
}

// transportFor builds the MCP transport for one server row.
func transportFor(server *model.ChatMCPServer) (mcp.Transport, error) {
	switch server.Transport {
	case model.ChatMCPServerStdio:
		if strings.TrimSpace(server.Command) == "" {
			return nil, fmt.Errorf("MCP 服务器 %s 缺少命令", server.Name)
		}
		cmd := exec.Command(server.Command, decodeStringList(server.Args)...)
		cmd.Env = append(os.Environ(), decodeStringList(server.Env)...)
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
