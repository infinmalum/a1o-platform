package native

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"
	"github.com/infinmalum/a1o-platform/relay/channeltype"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRawProviderAuthPreservesNativePayload(t *testing.T) {
	raw := `{"input":{"steps":99,"arbitrary_model_field":true},"tool_outputs":[{"tool_call_id":"call","output":"ok"}]}`
	for _, kind := range []int{channeltype.Coze, channeltype.Replicate, channeltype.Vercel, channeltype.OpenRouter, channeltype.Cloudflare, channeltype.AwsClaude} {
		req := httptest.NewRequest("POST", "https://example.test/native?conversation_id=c&chat_id=h", strings.NewReader(raw))
		req.Header.Set("Authorization", "Bearer caller")
		req.Header.Set("x-api-key", "caller")
		err := AuthenticateProviderRequest(context.Background(), req, Request{ChannelType: kind, APIKey: "provider", Region: "us-east-1", AccessKey: "ak", SecretKey: "sk"})
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(req.Body)
		if string(b) != raw || req.URL.Query().Get("conversation_id") != "c" || req.URL.Query().Get("chat_id") != "h" {
			t.Fatal("native request changed")
		}
		if req.Header.Get("x-api-key") != "" || strings.Contains(req.Header.Get("Authorization"), "caller") {
			t.Fatal("caller credentials leaked")
		}
		if kind == channeltype.AwsClaude {
			if !strings.HasPrefix(req.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
				t.Fatal("not signed")
			}
		} else if req.Header.Get("Authorization") != "Bearer provider" {
			t.Fatal("incorrect bearer auth")
		}
	}
}
func TestConverseNativeTransportSyncAndStream(t *testing.T) {
	for _, stream := range []bool{false, true} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			if !bytes.Contains(b, []byte(`"inferenceConfig"`)) || bytes.Contains(b, []byte(`"anthropic_version"`)) {
				t.Errorf("bad native body: %s", b)
			}
			if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
				t.Error("unsigned request")
			}
			if !stream {
				io.WriteString(w, `{"output":{"message":{"content":[{"text":"hello"}]}},"stopReason":"end_turn","usage":{"inputTokens":2,"outputTokens":3}}`)
				return
			}
			encoder := eventstream.NewEncoder()
			for _, e := range []struct{ name, data string }{{"messageStart", `{"role":"assistant"}`}, {"contentBlockDelta", `{"contentBlockIndex":0,"delta":{"text":"hello"}}`}, {"contentBlockStop", `{"contentBlockIndex":0}`}, {"messageStop", `{"stopReason":"end_turn"}`}, {"metadata", `{"usage":{"inputTokens":2,"outputTokens":3}}`}} {
				err := encoder.Encode(w, eventstream.Message{Headers: eventstream.Headers{{Name: ":event-type", Value: eventstream.StringValue(e.name)}}, Payload: []byte(e.data)})
				if err != nil {
					t.Error(err)
				}
			}
		}))
		response, err := bedrockConverse(context.Background(), Request{Protocol: Anthropic, BaseURL: srv.URL, Model: "amazon.nova-pro-v1:0", Region: "us-east-1", AccessKey: "ak", SecretKey: "sk", Stream: stream, Body: []byte(`{"model":"alias","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`)})
		if err != nil {
			srv.Close()
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		usage, err := CopyResponse(rec, response.Body, Anthropic, stream)
		response.Body.Close()
		srv.Close()
		if err != nil || usage.Input != 2 || usage.Output != 3 || !strings.Contains(rec.Body.String(), "hello") {
			t.Fatalf("%+v %v %s", usage, err, rec.Body.String())
		}
		if !stream {
			var obj map[string]any
			if json.Unmarshal(rec.Body.Bytes(), &obj) != nil {
				t.Fatal("invalid response")
			}
		}
	}
}
