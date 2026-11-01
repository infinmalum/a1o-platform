package native

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/aws/smithy-go"
	awshttp "github.com/aws/smithy-go/transport/http"
	"github.com/infinmalum/a1o-platform/common/logger"
	"github.com/infinmalum/a1o-platform/relay/channeltype"
)

// UsesProviderTransport reports channel kinds whose upstream I/O cannot run
// over the shared HTTP+SSE path: AWS Bedrock streaming is framed in the AWS
// binary event-stream encoding, so the Bedrock runtime SDK owns the wire and
// this package re-frames its decoded chunks as Anthropic SSE for the bridge.
func UsesProviderTransport(channelType int) bool {
	return channelType == channeltype.AwsClaude
}

// bedrockModelID resolves the configured model name into a Bedrock model ID.
// A name that already carries the inference-profile or on-demand ID shape
// (contains the version suffix ":0", e.g. "anthropic.claude-3-5-sonnet-20240620-v1:0"
// or "us.anthropic.claude-sonnet-4-20250514-v1:0") is used verbatim; a bare
// Claude name is wrapped onto the default on-demand ID. Inference-profile and
// v2 IDs must be configured exactly, via the channel model mapping.
func bedrockModelID(model string) (string, error) {
	if strings.HasPrefix(model, "arn:aws") && strings.Contains(model, ":bedrock:") && !strings.ContainsAny(model, "?#") {
		return model, nil
	}
	if model == "" || strings.ContainsAny(model, "/?#") {
		return "", fmt.Errorf("invalid Bedrock model name %q", model)
	}
	if strings.Contains(model, ":") {
		return model, nil
	}
	if strings.HasPrefix(model, "anthropic.") {
		return model + ":0", nil
	}
	return "anthropic." + model + "-v1:0", nil
}

// prepareBedrockBody adapts a bridge-produced Anthropic Messages body onto the
// Bedrock InvokeModel envelope: the model lives in the URL/ModelId, not the
// body, and the Bedrock Anthropic version replaces the api.anthropic.com one.
func prepareBedrockBody(body []byte, model string, stream bool) ([]byte, string, error) {
	modelID, err := bedrockModelID(model)
	if err != nil {
		return nil, "", err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return nil, "", fmt.Errorf("invalid Bedrock Anthropic JSON body")
	}
	delete(fields, "model")
	fields["anthropic_version"], _ = json.Marshal("bedrock-2023-05-31")
	// InvokeModelWithResponseStream selects streaming; the body has no stream field.
	delete(fields, "stream")
	encoded, err := json.Marshal(fields)
	return encoded, modelID, err
}

// BedrockInvoke sends an Anthropic-protocol body through the Bedrock runtime
// SDK and returns a synthetic HTTP response: the decoded InvokeModel payload
// for non-streaming requests, or the InvokeModelWithResponseStream chunks
// re-framed as Anthropic SSE for streaming ones. Errors surface as synthetic
// error responses so the shared lifecycle treats them like any upstream.
func BedrockInvoke(ctx context.Context, input Request) (*http.Response, error) {
	if input.ChannelType != channeltype.AwsClaude {
		return nil, fmt.Errorf("channel type %d has no provider transport", input.ChannelType)
	}
	if input.Version == "converse" {
		return bedrockConverse(ctx, input)
	}
	if input.Version == "messages" || input.Version == LatestAnthropicVersion {
		return bedrockMessages(ctx, input)
	}
	if input.Region == "" || input.AccessKey == "" || input.SecretKey == "" {
		return nil, fmt.Errorf("Bedrock channel requires region, ak, and sk in its config")
	}
	body, modelID, err := prepareBedrockBody(input.Body, input.Model, input.Stream)
	if err != nil {
		return nil, err
	}
	client := bedrockruntime.New(bedrockruntime.Options{
		Region:      input.Region,
		Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(input.AccessKey, input.SecretKey, "")),
	})
	if !input.Stream {
		invoked, err := client.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
			ModelId:     aws.String(modelID),
			Accept:      aws.String("application/json"),
			ContentType: aws.String("application/json"),
			Body:        body,
		})
		if err != nil {
			return bedrockErrorResponse(err), nil
		}
		header := http.Header{}
		header.Set("Content-Type", "application/json")
		return &http.Response{
			StatusCode: http.StatusOK, Header: header,
			Body: io.NopCloser(bytes.NewReader(invoked.Body)),
		}, nil
	}
	streamed, err := client.InvokeModelWithResponseStream(ctx, &bedrockruntime.InvokeModelWithResponseStreamInput{
		ModelId:     aws.String(modelID),
		Accept:      aws.String("application/json"),
		ContentType: aws.String("application/json"),
		Body:        body,
	})
	if err != nil {
		return bedrockErrorResponse(err), nil
	}
	reader, writer := io.Pipe()
	header := http.Header{}
	header.Set("Content-Type", "text/event-stream")
	response := &http.Response{
		StatusCode: http.StatusOK, Header: header, Body: reader,
	}
	go drainBedrockStream(streamed.GetStream(), writer)
	return response, nil
}

// drainBedrockStream re-frames the SDK's decoded event chunks as Anthropic
// SSE. Each ResponseStreamMemberChunk payload is already one Anthropic event
// JSON object, exactly the payload an api.anthropic.com SSE data line carries.
func drainBedrockStream(stream *bedrockruntime.InvokeModelWithResponseStreamEventStream, writer *io.PipeWriter) {
	defer writer.Close()
	defer stream.Close()
	complete := false
	for {
		event, ok := <-stream.Events()
		if !ok {
			if err := stream.Err(); err != nil {
				_ = writer.CloseWithError(err)
			} else if !complete {
				_ = writer.CloseWithError(errors.New("bedrock event stream ended before message_stop"))
			}
			return
		}
		switch chunk := event.(type) {
		case *types.ResponseStreamMemberChunk:
			if _, err := writer.Write(appendSSEFrame(chunk.Value.Bytes)); err != nil {
				return
			}
			if isBedrockStopChunk(chunk.Value.Bytes) {
				complete = true
				return
			}
		case *types.UnknownUnionMember:
			logger.SysError("bedrock event stream delivered an unknown union member: " + chunk.Tag)
			return
		default:
			logger.SysError("bedrock event stream delivered no chunk payload")
			return
		}
	}
}

// isBedrockStopChunk reports whether an event chunk is the terminal
// message_stop event, so the re-framed SSE stream can close cleanly.
func isBedrockStopChunk(payload []byte) bool {
	var header struct {
		Type string `json:"type"`
	}
	return json.Unmarshal(payload, &header) == nil && header.Type == "message_stop"
}

func appendSSEFrame(payload []byte) []byte {
	frame := make([]byte, 0, len(payload)+8)
	frame = append(frame, "data: "...)
	frame = append(frame, payload...)
	frame = append(frame, '\n', '\n')
	return frame
}

// bedrockErrorResponse converts an SDK failure into a synthetic HTTP error
// response so the shared lifecycle's upstream-error path parses it like any
// provider error body. The status comes from the HTTP layer of the SDK call
// when present, falling back to the API error fault class.
func bedrockErrorResponse(err error) *http.Response {
	message := err.Error()
	status := http.StatusInternalServerError
	var httpErr *awshttp.ResponseError
	if errors.As(err, &httpErr) {
		status = httpErr.HTTPStatusCode()
	} else {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			switch apiErr.ErrorFault() {
			case smithy.FaultClient:
				status = http.StatusBadRequest
			case smithy.FaultServer:
				status = http.StatusBadGateway
			}
		}
	}
	payload, _ := json.Marshal(map[string]any{"error": map[string]any{"message": message, "type": "bedrock_error", "code": status}})
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	return &http.Response{
		StatusCode: status, Header: header,
		Body: io.NopCloser(bytes.NewReader(payload)),
	}
}

// The Messages transport is opt-in via config.api_version=messages (or an
// explicit current version). InvokeModel remains available for older models.
func buildBedrockMessages(ctx context.Context, input Request) (*http.Request, error) {
	if input.Region == "" {
		return nil, errors.New("Bedrock Messages requires region")
	}
	input.ChannelType = channeltype.Anthropic
	if input.BaseURL == "" {
		input.BaseURL = "https://bedrock-runtime." + input.Region + ".amazonaws.com/anthropic"
	}
	if input.APIKey == "" {
		input.APIKey = "sigv4"
	}
	req, err := BuildRequest(ctx, input)
	if err != nil {
		return nil, err
	}
	req.Header.Del("x-api-key")
	if input.AccessKey != "" && input.SecretKey != "" {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
		sum := sha256.Sum256(body)
		err = v4.NewSigner().SignHTTP(ctx, aws.Credentials{AccessKeyID: input.AccessKey, SecretAccessKey: input.SecretKey}, req, hex.EncodeToString(sum[:]), "bedrock", input.Region, time.Now())
		if err != nil {
			return nil, err
		}
	} else {
		if input.APIKey == "sigv4" {
			return nil, errors.New("Bedrock Messages requires AWS credentials or a Bedrock API key")
		}
		req.Header.Set("Authorization", "Bearer "+input.APIKey)
	}
	return req, nil
}
func bedrockMessages(ctx context.Context, input Request) (*http.Response, error) {
	req, err := buildBedrockMessages(ctx, input)
	if err != nil {
		return nil, err
	}
	client := *http.DefaultClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return client.Do(req)
}
