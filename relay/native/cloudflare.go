package native

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/infinmalum/a1o-platform/relay/sse"
)

// Workers AI Run is a provider protocol: the model is in the URL, embeddings
// use text, and results use a Cloudflare envelope rather than choices/data.
func prepareCloudflareRun(input Request, body []byte, base *url.URL) (string, []byte, error) {
	if input.Model == "" || strings.ContainsAny(input.Model, "?#%\\") {
		return "", nil, errors.New("invalid Workers AI model")
	}
	for _, part := range strings.Split(input.Model, "/") {
		if part == "." || part == ".." || part == "" {
			return "", nil, errors.New("invalid Workers AI model path")
		}
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return "", nil, err
	}
	delete(fields, "model")
	switch input.Protocol {
	case OpenAIChat:
		if raw := fields["tools"]; raw != nil {
			var tools []map[string]json.RawMessage
			if err := json.Unmarshal(raw, &tools); err != nil {
				return "", nil, err
			}
			converted := make([]json.RawMessage, 0, len(tools))
			for _, tool := range tools {
				if fn := tool["function"]; fn != nil {
					converted = append(converted, fn)
				} else {
					encoded, _ := json.Marshal(tool)
					converted = append(converted, encoded)
				}
			}
			fields["tools"], _ = json.Marshal(converted)
		}
		delete(fields, "stream_options") // client reporting option, not model input
		if value, ok := fields["max_completion_tokens"]; ok {
			fields["max_tokens"] = value
			delete(fields, "max_completion_tokens")
		}
	case OpenAIEmbeddings:
		var texts []string
		var text string
		if json.Unmarshal(fields["input"], &text) == nil {
			texts = []string{text}
		} else if json.Unmarshal(fields["input"], &texts) != nil || len(texts) == 0 {
			return "", nil, errors.New("Workers AI embeddings require text input")
		}
		fields["text"], _ = json.Marshal(texts)
		delete(fields, "input")
		delete(fields, "encoding_format") // converted on the response
	default:
		return "", nil, errors.New("unsupported Workers AI Run operation")
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return "", nil, err
	}
	if base.Hostname() == "gateway.ai.cloudflare.com" {
		return "/" + input.Model, encoded, nil
	}
	if input.CloudflareAcct == "" {
		return "", nil, errors.New("Workers AI requires user_id (account ID)")
	}
	return "/client/v4/accounts/" + url.PathEscape(input.CloudflareAcct) + "/ai/run/" + input.Model, encoded, nil
}

// NormalizeCloudflareResponse keeps provider decoding at the transport boundary
// so the existing downstream bridges, accounting and error handling are shared.
func NormalizeCloudflareResponse(resp *http.Response, input Request) *http.Response {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp
	}
	if input.Stream && strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		source := resp.Body
		reader, writer := io.Pipe()
		resp.Body = &providerStreamBody{PipeReader: reader, source: source}
		go func() {
			defer source.Close()
			terminal := false
			err := sse.Read(source, func(e sse.Event) error {
				if e.Data == "[DONE]" {
					terminal = true
					_, err := writer.Write([]byte("data: [DONE]\n\n"))
					if err != nil {
						return err
					}
					return io.EOF
				}
				payload, err := cloudflareResult([]byte(e.Data), input, true)
				if err != nil {
					return err
				}
				_, err = writer.Write(appendSSEFrame(payload))
				return err
			})
			if err == nil && !terminal {
				err = errors.New("Workers AI stream ended before [DONE]")
			}
			_ = writer.CloseWithError(err)
		}()
		return resp
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err == nil {
		body, err = cloudflareResult(body, input, false)
	}
	if err != nil {
		resp.StatusCode = http.StatusBadGateway
		body, _ = json.Marshal(map[string]any{"error": map[string]string{"message": err.Error(), "type": "workers_ai_error"}})
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.Header.Set("Content-Type", "application/json")
	resp.ContentLength = int64(len(body))
	return resp
}

type providerStreamBody struct {
	*io.PipeReader
	source io.ReadCloser
}

func (b *providerStreamBody) Close() error { _ = b.source.Close(); return b.PipeReader.Close() }

func cloudflareResult(body []byte, input Request, stream bool) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	if value := fields["success"]; string(value) == "false" {
		return nil, fmt.Errorf("Workers AI failed: %s", fields["errors"])
	}
	if raw, ok := fields["result"]; ok {
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, err
		}
	}
	if fields == nil {
		return nil, errors.New("empty Workers AI result")
	}
	if _, ok := fields["error"]; ok {
		return nil, fmt.Errorf("Workers AI failed: %s", fields["error"])
	}
	// Some Run models use choices internally; retain their complete schema.
	if _, ok := fields["choices"]; ok {
		return json.Marshal(fields)
	}
	if input.Protocol == OpenAIEmbeddings {
		var vectors [][]float64
		if err := json.Unmarshal(fields["data"], &vectors); err != nil || vectors == nil {
			return nil, errors.New("invalid Workers AI embedding result")
		}
		var options struct {
			Encoding string `json:"encoding_format"`
		}
		_ = json.Unmarshal(input.Body, &options)
		data := make([]any, len(vectors))
		for i, vector := range vectors {
			var embedding any = vector
			if options.Encoding == "base64" {
				raw := make([]byte, len(vector)*4)
				for j, value := range vector {
					binary.LittleEndian.PutUint32(raw[j*4:], math.Float32bits(float32(value)))
				}
				embedding = base64.StdEncoding.EncodeToString(raw)
			}
			data[i] = map[string]any{"object": "embedding", "index": i, "embedding": embedding}
		}
		result := map[string]any{"object": "list", "model": input.Model, "data": data}
		if fields["usage"] != nil {
			result["usage"] = fields["usage"]
		}
		return json.Marshal(result)
	}
	var text string
	if raw, ok := fields["response"]; ok && string(raw) != "null" {
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, err
		}
	}
	message := map[string]any{"content": text}
	finish := "stop"
	if raw := fields["tool_calls"]; raw != nil {
		var calls []map[string]json.RawMessage
		if err := json.Unmarshal(raw, &calls); err != nil {
			return nil, err
		}
		converted := make([]any, 0, len(calls))
		for i, call := range calls {
			if call["function"] != nil {
				converted = append(converted, call)
				continue
			}
			var name string
			_ = json.Unmarshal(call["name"], &name)
			args := string(call["arguments"])
			if args == "" {
				args = "{}"
			}
			item := map[string]any{"id": fmt.Sprintf("call_cf_%d", i), "type": "function", "function": map[string]string{"name": name, "arguments": args}}
			if stream {
				item["index"] = i
			}
			converted = append(converted, item)
		}
		if len(converted) > 0 {
			message["tool_calls"] = converted
			finish = "tool_calls"
		}
	}
	choice := map[string]any{"index": 0, "message": message, "finish_reason": finish}
	object := "chat.completion"
	if stream {
		object = "chat.completion.chunk"
		delete(choice, "message")
		choice["delta"] = message
		choice["finish_reason"] = nil
	} else {
		message["role"] = "assistant"
	}
	result := map[string]any{"id": "chatcmpl-workers-ai", "object": object, "created": time.Now().Unix(), "model": input.Model, "choices": []any{choice}}
	if raw := fields["usage"]; raw != nil {
		result["usage"] = raw
	}
	return json.Marshal(result)
}
