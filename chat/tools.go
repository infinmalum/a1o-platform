package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/infinmalum/a1o-platform/model"
)

// Tool is one callable tool advertised to the model in OpenAI function
// format.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// toolRequest is a model-initiated invocation with raw JSON arguments.
type toolRequest struct {
	Name      string
	Arguments string
}

// toolResult feeds the outcome back to the model; errors travel in-band so
// the model can react.
type toolResult struct {
	Content string
	IsError bool
}

type toolFunc func(ctx context.Context, req toolRequest) toolResult

// toolSet holds the tools available for one turn.
type toolSet struct {
	defs []Tool
	exec map[string]toolFunc
}

func newToolSet() *toolSet {
	return &toolSet{exec: map[string]toolFunc{}}
}

func (t *toolSet) add(def Tool, fn toolFunc) {
	if _, exists := t.exec[def.Name]; exists {
		return
	}
	t.defs = append(t.defs, def)
	t.exec[def.Name] = fn
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
func (r *Runtime) runSandbox(ctx context.Context, req toolRequest) toolResult {
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
	output, err := r.Sandbox.Execute(ctx, args.Language, args.Code)
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
