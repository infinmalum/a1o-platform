package native

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"
	"github.com/infinmalum/a1o-platform/common/client"
	"github.com/infinmalum/a1o-platform/relay/channeltype"
)

// Converse provides the common native Bedrock inference protocol for models
// outside the Claude InvokeModel family. The downstream wire remains Messages.
func converseBody(body []byte) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	var messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(fields["messages"], &messages); err != nil {
		return nil, err
	}
	out := map[string]any{}
	var converted []any
	for _, message := range messages {
		var plain string
		var blocks []map[string]json.RawMessage
		if json.Unmarshal(message.Content, &plain) == nil {
			blocks = []map[string]json.RawMessage{{"type": json.RawMessage(`"text"`), "text": message.Content}}
		} else if err := json.Unmarshal(message.Content, &blocks); err != nil {
			return nil, err
		}
		content := make([]any, 0, len(blocks))
		for _, block := range blocks {
			switch string(block["type"]) {
			case `"text"`:
				content = append(content, map[string]any{"text": block["text"]})
			case `"tool_use"`:
				content = append(content, map[string]any{"toolUse": map[string]any{"toolUseId": block["id"], "name": block["name"], "input": block["input"]}})
			case `"tool_result"`:
				var text string
				_ = json.Unmarshal(block["content"], &text)
				if text == "" {
					text = string(block["content"])
				}
				result := map[string]any{"toolUseId": block["tool_use_id"], "content": []any{map[string]string{"text": text}}}
				if string(block["is_error"]) == "true" {
					result["status"] = "error"
				}
				content = append(content, map[string]any{"toolResult": result})
			default:
				return nil, fmt.Errorf("Converse conversion cannot represent block %s; use native request forwarding", block["type"])
			}
		}
		converted = append(converted, map[string]any{"role": message.Role, "content": content})
	}
	out["messages"] = converted
	if raw := fields["system"]; len(raw) > 0 {
		var text string
		if json.Unmarshal(raw, &text) == nil {
			out["system"] = []any{map[string]string{"text": text}}
		} else {
			var blocks []map[string]json.RawMessage
			if err := json.Unmarshal(raw, &blocks); err != nil {
				return nil, err
			}
			var result []any
			for _, b := range blocks {
				result = append(result, map[string]any{"text": b["text"]})
			}
			out["system"] = result
		}
	}
	inference := map[string]any{}
	for from, to := range map[string]string{"max_tokens": "maxTokens", "temperature": "temperature", "top_p": "topP", "stop_sequences": "stopSequences"} {
		if v := fields[from]; v != nil {
			inference[to] = v
		}
	}
	if len(inference) > 0 {
		out["inferenceConfig"] = inference
	}
	if fields["tools"] != nil {
		var tools []map[string]json.RawMessage
		if err := json.Unmarshal(fields["tools"], &tools); err != nil {
			return nil, err
		}
		converted := make([]any, 0, len(tools))
		for _, tool := range tools {
			spec := map[string]any{"name": tool["name"], "inputSchema": map[string]any{"json": tool["input_schema"]}}
			if tool["description"] != nil {
				spec["description"] = tool["description"]
			}
			converted = append(converted, map[string]any{"toolSpec": spec})
		}
		cfg := map[string]any{"tools": converted}
		var choice struct{ Type, Name string }
		_ = json.Unmarshal(fields["tool_choice"], &choice)
		if choice.Type == "auto" || choice.Type == "any" {
			cfg["toolChoice"] = map[string]any{choice.Type: map[string]any{}}
		} else if choice.Type == "tool" {
			cfg["toolChoice"] = map[string]any{"tool": map[string]string{"name": choice.Name}}
		}
		out["toolConfig"] = cfg
	}
	extra := map[string]any{}
	for k, v := range fields {
		switch k {
		case "model", "messages", "system", "max_tokens", "temperature", "top_p", "stop_sequences", "tools", "tool_choice", "stream":
		default:
			extra[k] = v
		}
	}
	if len(extra) > 0 {
		out["additionalModelRequestFields"] = extra
	}
	return json.Marshal(out)
}
func bedrockConverse(ctx context.Context, input Request) (*http.Response, error) {
	body, err := converseBody(input.Body)
	if err != nil {
		return nil, err
	}
	base := input.BaseURL
	if base == "" {
		base = "https://bedrock-runtime." + input.Region + ".amazonaws.com"
	}
	action := "converse"
	if input.Stream {
		action = "converse-stream"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/model/"+url.PathEscape(input.Model)+"/"+action, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	input.ChannelType = channeltype.AwsClaude
	if err := AuthenticateProviderRequest(ctx, req, input); err != nil {
		return nil, err
	}
	hc := client.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	safe := *hc
	safe.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := safe.Do(req)
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp, err
	}
	if !input.Stream {
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		converted, err := converseResponse(raw, input.Model)
		if err != nil {
			return nil, err
		}
		resp.Body = io.NopCloser(bytes.NewReader(converted))
		resp.Header.Set("Content-Type", "application/json")
		resp.ContentLength = int64(len(converted))
		return resp, nil
	}
	source := resp.Body
	r, w := io.Pipe()
	resp.Body = &providerStreamBody{PipeReader: r, source: source}
	resp.Header.Set("Content-Type", "text/event-stream")
	resp.ContentLength = -1
	go func() {
		defer source.Close()
		defer w.Close()
		decoder := eventstream.NewDecoder()
		started := false
		stopped := false
		opened := map[string]bool{}
		emit := func(v any) error {
			b, e := json.Marshal(v)
			if e != nil {
				return e
			}
			_, e = w.Write(appendSSEFrame(b))
			return e
		}
		for {
			message, err := decoder.Decode(source, nil)
			if err != nil {
				if err == io.EOF && stopped {
					_ = emit(map[string]any{"type": "message_stop"})
					return
				}
				_ = w.CloseWithError(err)
				return
			}
			kind := ""
			for _, h := range message.Headers {
				if h.Name == ":event-type" {
					kind = h.Value.String()
				}
			}
			var event map[string]json.RawMessage
			if err := json.Unmarshal(message.Payload, &event); err != nil {
				_ = w.CloseWithError(err)
				return
			}
			var output any
			switch kind {
			case "messageStart":
				started = true
				output = map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_bedrock", "type": "message", "role": "assistant", "model": input.Model, "content": []any{}, "usage": map[string]int{"input_tokens": 0, "output_tokens": 0}}}
			case "contentBlockStart":
				opened[string(event["contentBlockIndex"])] = true
				var start map[string]json.RawMessage
				_ = json.Unmarshal(event["start"], &start)
				var tool map[string]json.RawMessage
				_ = json.Unmarshal(start["toolUse"], &tool)
				block := map[string]any{"type": "text", "text": ""}
				if len(tool) > 0 {
					block = map[string]any{"type": "tool_use", "id": tool["toolUseId"], "name": tool["name"], "input": map[string]any{}}
				}
				output = map[string]any{"type": "content_block_start", "index": event["contentBlockIndex"], "content_block": block}
			case "contentBlockDelta":
				if !opened[string(event["contentBlockIndex"])] {
					if err := emit(map[string]any{"type": "content_block_start", "index": event["contentBlockIndex"], "content_block": map[string]string{"type": "text", "text": ""}}); err != nil {
						return
					}
					opened[string(event["contentBlockIndex"])] = true
				}
				var delta map[string]json.RawMessage
				_ = json.Unmarshal(event["delta"], &delta)
				d := map[string]any{"type": "text_delta", "text": delta["text"]}
				if delta["toolUse"] != nil {
					var tool map[string]json.RawMessage
					_ = json.Unmarshal(delta["toolUse"], &tool)
					d = map[string]any{"type": "input_json_delta", "partial_json": tool["input"]}
				}
				output = map[string]any{"type": "content_block_delta", "index": event["contentBlockIndex"], "delta": d}
			case "contentBlockStop":
				output = map[string]any{"type": "content_block_stop", "index": event["contentBlockIndex"]}
			case "messageStop":
				stopped = true
				var reason string
				_ = json.Unmarshal(event["stopReason"], &reason)
				output = map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": converseStop(reason)}}
			case "metadata":
				var u struct {
					Input  int64 `json:"inputTokens"`
					Output int64 `json:"outputTokens"`
				}
				_ = json.Unmarshal(event["usage"], &u)
				output = map[string]any{"type": "message_delta", "delta": map[string]any{}, "usage": map[string]int64{"input_tokens": u.Input, "output_tokens": u.Output}}
			default:
				_ = w.CloseWithError(fmt.Errorf("Bedrock event %s: %s", kind, message.Payload))
				return
			}
			if !started {
				_ = w.CloseWithError(fmt.Errorf("Converse stream missing messageStart"))
				return
			}
			if err := emit(output); err != nil {
				return
			}
		}
	}()
	return resp, nil
}
func converseStop(reason string) string {
	switch reason {
	case "tool_use":
		return "tool_use"
	case "max_tokens":
		return "max_tokens"
	case "stop_sequence":
		return "stop_sequence"
	default:
		return "end_turn"
	}
}
func converseResponse(raw []byte, model string) ([]byte, error) {
	var result struct {
		Output struct {
			Message struct {
				Content []map[string]json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"output"`
		StopReason string `json:"stopReason"`
		Usage      struct {
			Input  int64 `json:"inputTokens"`
			Output int64 `json:"outputTokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	var blocks []any
	for _, c := range result.Output.Message.Content {
		if c["text"] != nil {
			blocks = append(blocks, map[string]any{"type": "text", "text": c["text"]})
		} else if c["toolUse"] != nil {
			var tool map[string]json.RawMessage
			_ = json.Unmarshal(c["toolUse"], &tool)
			blocks = append(blocks, map[string]any{"type": "tool_use", "id": tool["toolUseId"], "name": tool["name"], "input": tool["input"]})
		} else {
			return nil, fmt.Errorf("unrepresentable Converse response block")
		}
	}
	return json.Marshal(map[string]any{"id": "msg_bedrock", "type": "message", "role": "assistant", "model": model, "content": blocks, "stop_reason": converseStop(result.StopReason), "usage": map[string]int64{"input_tokens": result.Usage.Input, "output_tokens": result.Usage.Output}})
}
