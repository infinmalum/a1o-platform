package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/infinmalum/norma-e/llm"
	normatool "github.com/infinmalum/norma-e/tool"

	"github.com/infinmalum/a1o-platform/model"
)

// toolRequest is a model-initiated invocation with raw JSON arguments.
type toolRequest struct {
	Name      string
	Arguments string
}

// toolResult feeds the outcome back to the model; errors travel in-band so
// the model can react. Blocks, when set, carries the full Norma content
// blocks (text plus media) and Content keeps the plain-text projection for
// persistence paths that have no media representation yet.
type toolResult struct {
	Content string
	Blocks  []llm.ContentBlock
	IsError bool
}

// sdkResultText renders a Norma tool result's text blocks into one string.
func sdkResultText(result normatool.Result) string {
	var b strings.Builder
	for _, block := range result.Content {
		b.WriteString(block.Text)
	}
	return b.String()
}

// mediaResultItems converts a tool result's media blocks into a synthetic
// user message item carrying input_image parts, appended after the
// function_call_output so the next leg's provider payload includes them.
// Every bridge already maps input_image parts on user messages, and Chat
// Completions upstreams conventionally receive tool-result media this way.
// The synthetic item lives only within the current run: history persistence
// keeps tool rows as text, so cross-turn replay drops the media. Tools that
// produce durable media should also surface a persisted asset reference in
// their text content.
func mediaResultItems(result toolResult) []Item {
	parts := make([]ContentPart, 0, len(result.Blocks))
	for _, block := range result.Blocks {
		if block.Type != llm.BlockImage || block.Source == nil || block.Source.Type != "base64" {
			continue
		}
		parts = append(parts, ContentPart{Type: PartInputImage,
			ImageURL: "data:" + block.Source.MediaType + ";base64," + block.Source.Data})
	}
	if len(parts) == 0 {
		return nil
	}
	return []Item{{Type: ItemMessage, Role: "user", Content: parts}}
}

type toolFunc func(ctx context.Context, req toolRequest) toolResult

// toolSet holds the tools available for one turn.
type toolSet struct {
	defs []Tool
	exec map[string]toolFunc
	// sdk holds Norma core tools dispatched inside normaLoop. They share the
	// defs surface (skill filtering, bootstrap visibility) but their Call is
	// the SDK implementation, not a host closure.
	sdk map[string]normatool.CoreTool
	// todos is the per-run todo store backing the TodoWrite tool; normaLoop
	// passes it to the harness for reminder re-surfacing.
	todos         *normatool.TodoStore
	skillPrompt   string
	skillGuidance []positionedSkillGuidance
}

func newToolSet() *toolSet {
	return &toolSet{exec: map[string]toolFunc{}, sdk: map[string]normatool.CoreTool{}}
}

func (t *toolSet) add(def Tool, fn toolFunc) {
	if _, exists := t.exec[def.Name]; exists {
		return
	}
	t.defs = append(t.defs, def)
	t.exec[def.Name] = fn
}

// addSDK registers a Norma core tool. The tool def mirrors the core tool's
// schema so the rest of the host treats it like any other tool.
func (t *toolSet) addSDK(core normatool.CoreTool) {
	name := core.Name()
	if _, exists := t.sdk[name]; exists {
		return
	}
	raw, err := json.Marshal(core.InputSchema())
	if err != nil {
		return
	}
	t.sdk[name] = core
	t.defs = append(t.defs, Tool{
		Name:        name,
		Description: core.Description(),
		Parameters:  raw,
	})
}

// filterBySkills narrows the tool surface when any selected skill declares a
// non-empty tool list: only the union of those lists stays available.
func (t *toolSet) filterBySkills(skills []*model.ChatSkill) {
	var allowed map[string]bool
	for _, skill := range skills {
		for _, name := range skill.ToolList() {
			if allowed == nil {
				allowed = map[string]bool{}
			}
			allowed[name] = true
		}
	}
	if allowed == nil {
		return
	}
	filtered := newToolSet()
	for _, def := range t.defs {
		if allowed[def.Name] {
			filtered.defs = append(filtered.defs, def)
			filtered.exec[def.Name] = t.exec[def.Name]
		}
	}
	t.defs, t.exec = filtered.defs, filtered.exec
}

// sandboxToolName is the virtual name of the built-in code execution tool.
const sandboxToolName = "sandbox"

func sandboxToolDef(languages []string) Tool {
	description := "在隔离的本地环境中执行一段代码并返回标准输出与标准错误。适合计算、文本处理、数据转换等任务。"
	if len(languages) > 0 {
		description += fmt.Sprintf("可用语言：%s。", strings.Join(languages, "、"))
	}
	return Tool{
		Name:        sandboxToolName,
		Description: description,
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"language": {"type": "string", "description": "代码语言", "enum": ["python", "javascript"]},
				"code": {"type": "string", "description": "要执行的代码"}
			},
			"required": ["language", "code"]
		}`),
	}
}

// runSandbox parses the model's arguments and executes the code.
func (e *Engine) runSandbox(ctx context.Context, req toolRequest) toolResult {
	var args struct {
		Language string `json:"language"`
		Code     string `json:"code"`
	}
	if err := json.Unmarshal([]byte(req.Arguments), &args); err != nil {
		return toolResult{Content: "参数不是合法 JSON 对象: " + err.Error(), IsError: true}
	}
	if strings.TrimSpace(args.Code) == "" {
		return toolResult{Content: "code 不能为空", IsError: true}
	}
	ctx, cancel := sandboxContext(ctx)
	defer cancel()
	output, err := e.Sandbox.Execute(ctx, args.Language, args.Code)
	content := output
	if err != nil {
		if content != "" {
			content += "\n"
		}
		content += "[错误] " + err.Error()
		return toolResult{Content: content, IsError: true}
	}
	if strings.TrimSpace(content) == "" {
		content = "[无输出]"
	}
	return toolResult{Content: content}
}
