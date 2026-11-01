package native

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/infinmalum/a1o-platform/relay/channeltype"
)

func TestWorkersAIRunContract(t *testing.T) {
	for _, protocol := range []Protocol{OpenAIChat, OpenAIEmbeddings} {
		body := `{"model":"alias","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"weather","parameters":{"type":"object"}}}]}`
		if protocol == OpenAIEmbeddings {
			body = `{"model":"alias","input":["a","b"]}`
		}
		in := Request{Protocol: protocol, ChannelType: channeltype.Cloudflare, BaseURL: "https://api.cloudflare.com", CloudflareAcct: "account", Model: "@cf/test/model", APIKey: "provider-key", Body: []byte(body)}
		req, err := BuildRequest(context.Background(), in)
		if err != nil {
			t.Fatal(err)
		}
		if req.URL.Path != "/client/v4/accounts/account/ai/run/@cf/test/model" {
			t.Fatal(req.URL)
		}
		raw, _ := io.ReadAll(req.Body)
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		if fields["model"] != nil {
			t.Fatal("model must be addressed in path")
		}
		if protocol == OpenAIChat && !strings.Contains(string(fields["tools"]), `"name":"weather"`) {
			t.Fatal(string(raw))
		}
		response := `{"success":true,"result":{"response":"hello","usage":{"prompt_tokens":2,"completion_tokens":3}}}`
		if protocol == OpenAIEmbeddings {
			if string(fields["text"]) != `["a","b"]` {
				t.Fatal(string(raw))
			}
			response = `{"success":true,"result":{"shape":[2,2],"data":[[1,2],[3,4]]}}`
		}
		resp := NormalizeCloudflareResponse(&http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(response))}, in)
		raw, _ = io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			t.Fatal(string(raw))
		}
		if protocol == OpenAIChat && !strings.Contains(string(raw), `"content":"hello"`) {
			t.Fatal(string(raw))
		}
		if protocol == OpenAIEmbeddings && !strings.Contains(string(raw), `"index":1`) {
			t.Fatal(string(raw))
		}
	}
}
func TestWorkersAIStreamAndFailure(t *testing.T) {
	for _, failure := range []bool{false, true} {
		content := "data: {\"response\":\"hello\"}\n\n"
		if !failure {
			content += "data: [DONE]\n\n"
		}
		in := Request{Protocol: OpenAIChat, Stream: true, Model: "@cf/test/model"}
		resp := NormalizeCloudflareResponse(&http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(content))}, in)
		w := httptest.NewRecorder()
		usage, err := CopyResponse(w, resp.Body, OpenAIChat, true)
		_ = resp.Body.Close()
		if failure && err == nil {
			t.Fatal("truncated upstream reported success")
		}
		if !failure && (err != nil || !usage.Complete) {
			t.Fatalf("%+v %v", usage, err)
		}
		if !strings.Contains(w.Body.String(), `"content":"hello"`) {
			t.Fatal(w.Body.String())
		}
	}
}
func TestProviderVersionContracts(t *testing.T) {
	for _, tc := range []struct{ body, version string }{
		{`{"model":"claude","messages":[]}`, LatestAnthropicVersion},
		{`{"model":"claude","temperature":0.2,"messages":[]}`, "2023-06-01"},
		{`{"model":"claude","messages":[{"role":"assistant","content":[{"type":"document"}]}]}`, "2023-06-01"},
	} {
		req, err := BuildRequest(context.Background(), Request{Protocol: Anthropic, ChannelType: channeltype.Anthropic, BaseURL: "https://api.anthropic.com", APIKey: "key", Model: "claude", Body: []byte(tc.body)})
		if err != nil || req.Header.Get("anthropic-version") != tc.version {
			t.Fatalf("%v %v", req, err)
		}
	}
	for _, version := range []string{"", "v1", "2024-10-21"} {
		req, err := BuildRequest(context.Background(), Request{Protocol: OpenAIChat, ChannelType: channeltype.Azure, BaseURL: "https://example.openai.azure.com", Version: version, Model: "deployment.1", APIKey: "key", Body: []byte(`{"model":"deployment.1","messages":[]}`)})
		if err != nil {
			t.Fatal(err)
		}
		if version == "2024-10-21" {
			if !strings.Contains(req.URL.Path, "deployment.1") || req.URL.Query().Get("api-version") != version {
				t.Fatal(req.URL)
			}
		} else if req.URL.Path != "/openai/v1/chat/completions" || req.URL.Query().Get("api-version") != "" {
			t.Fatal(req.URL)
		}
	}
	if got := vertexAwareBaseURL(Request{ChannelType: channeltype.VertextAI, Region: "global"}); got != "https://aiplatform.googleapis.com" {
		t.Fatal(got)
	}
	arn := "arn:aws:bedrock:us-east-1:123:inference-profile/example"
	body, id, err := prepareBedrockBody([]byte(`{"model":"alias","stream":true,"messages":[]}`), arn, true)
	if err != nil || id != arn || strings.Contains(string(body), `"stream"`) {
		t.Fatalf("%s %s %v", body, id, err)
	}
	req, err := buildBedrockMessages(context.Background(), Request{Protocol: Anthropic, ChannelType: channeltype.AwsClaude, Region: "us-east-1", AccessKey: "test", SecretKey: "test", Version: "messages", Model: "claude-opus-4-7", Body: []byte(`{"model":"claude-opus-4-7","max_tokens":10,"messages":[]}`)})
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.Path != "/anthropic/v1/messages" || !strings.HasPrefix(req.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
		t.Fatalf("%s %v", req.URL, req.Header)
	}
}
func TestVercelOpenResponsesKeepsProviderOptions(t *testing.T) {
	raw := []byte(`{"model":"anthropic/claude","input":[{"type":"message","role":"user","content":"hi"}],"providerOptions":{"gateway":{"models":["other/model"]}},"vendor:new":true}`)
	req, err := BuildRequest(context.Background(), Request{Protocol: OpenResponses, ChannelType: channeltype.Vercel, BaseURL: channeltype.BaseURL(channeltype.Vercel), Model: "anthropic/claude", APIKey: "key", Body: raw})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(req.Body)
	if string(body) != string(raw) || req.URL.Path != "/v1/responses" {
		t.Fatalf("%s %s", req.URL, body)
	}
}
func TestOpenRouterExtensionsSurviveNativeWire(t *testing.T) {
	raw := []byte(`{"model":"provider/model","messages":[],"provider":{"order":["A","B"],"allow_fallbacks":false},"plugins":[{"id":"web"}],"models":["other/model"],"reasoning":{"effort":"high"}}`)
	req, err := BuildRequest(context.Background(), Request{Protocol: OpenAIChat, ChannelType: channeltype.OpenRouter, BaseURL: channeltype.BaseURL(channeltype.OpenRouter), Model: "provider/model", APIKey: "key", Body: raw})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(req.Body)
	if string(body) != string(raw) {
		t.Fatal(string(body))
	}
}

func TestCloudflareGatewayRoutingAndAuthentication(t *testing.T) {
	for _, base := range []string{"https://api.cloudflare.com", "https://gateway.ai.cloudflare.com/v1/account/gateway/workers-ai"} {
		req, err := BuildRequest(context.Background(), Request{Protocol: OpenAIChat, ChannelType: channeltype.Cloudflare, BaseURL: base, CloudflareAcct: "account", CloudflareGatewayID: "gateway", CloudflareGatewayToken: "gateway-secret", Model: "@cf/test/model", APIKey: "workers-secret", Body: []byte(`{"model":"@cf/test/model","messages":[]}`)})
		if err != nil {
			t.Fatal(err)
		}
		if req.Header.Get("Authorization") != "Bearer workers-secret" || req.Header.Get("cf-aig-authorization") != "Bearer gateway-secret" || req.Header.Get("cf-aig-gateway-id") != "gateway" {
			t.Fatal("gateway credentials or routing missing")
		}
		if strings.Contains(base, "gateway.ai") && req.URL.String() != base+"/@cf/test/model" {
			t.Fatal(req.URL)
		}
	}
}

func TestCloudflareGatewayURLIsGenerated(t *testing.T) {
	for _, base := range []string{"", "https://api.cloudflare.com"} {
		req, err := BuildRequest(context.Background(), Request{Protocol: OpenAIChat, ChannelType: channeltype.Cloudflare, BaseURL: base, CloudflareAcct: "account", CloudflareGatewayID: "gateway", Model: "@cf/test/model", APIKey: "key", Body: []byte(`{"model":"@cf/test/model","messages":[]}`)})
		if err != nil {
			t.Fatal(err)
		}
		if req.URL.String() != "https://gateway.ai.cloudflare.com/v1/account/gateway/workers-ai/@cf/test/model" {
			t.Fatal(req.URL)
		}
	}
}
