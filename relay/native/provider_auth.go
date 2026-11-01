package native

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/infinmalum/a1o-platform/relay/channeltype"
)

// AuthenticateProviderRequest applies channel-owned credentials to a raw native
// request. It never reuses caller credentials, and does not reshape the payload.
func AuthenticateProviderRequest(ctx context.Context, req *http.Request, input Request) error {
	req.Header.Del("Authorization")
	req.Header.Del("x-api-key")
	req.Header.Del("api-key")
	req.Header.Del("x-goog-api-key")
	req.Header.Del("cf-aig-authorization")
	switch input.ChannelType {
	case channeltype.Anthropic:
		if len(input.Body) == 0 && req.Body != nil {
			body, err := io.ReadAll(req.Body)
			if err != nil {
				return err
			}
			input.Body = body
			req.Body = io.NopCloser(bytes.NewReader(body))
		}
		req.Header.Set("x-api-key", input.APIKey)
		if req.Header.Get("anthropic-version") == "" {
			req.Header.Set("anthropic-version", anthropicAPIVersion(input))
		}
	case channeltype.Gemini:
		req.Header.Set("x-goog-api-key", input.APIKey)
	case channeltype.DeepL:
		req.Header.Set("Authorization", "DeepL-Auth-Key "+input.APIKey)
	case channeltype.Azure:
		req.Header.Set("api-key", input.APIKey)
	case channeltype.VertextAI:
		token, err := VertexAccessToken(ctx, input.ChannelID, input.VertexADC)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	case channeltype.AwsClaude:
		if input.AccessKey == "" || input.SecretKey == "" || input.Region == "" {
			return fmt.Errorf("Bedrock native requests require region, ak and sk")
		}
		var body []byte
		var err error
		if req.Body != nil {
			body, err = io.ReadAll(req.Body)
		}
		if err != nil {
			return err
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))
		sum := sha256.Sum256(body)
		return v4.NewSigner().SignHTTP(ctx, aws.Credentials{AccessKeyID: input.AccessKey, SecretAccessKey: input.SecretKey}, req, hex.EncodeToString(sum[:]), "bedrock", input.Region, time.Now())
	default:
		req.Header.Set("Authorization", "Bearer "+input.APIKey)
	}
	if input.CloudflareGatewayID != "" {
		req.Header.Set("cf-aig-gateway-id", input.CloudflareGatewayID)
	}
	if input.CloudflareGatewayToken != "" {
		req.Header.Set("cf-aig-authorization", "Bearer "+input.CloudflareGatewayToken)
	}
	return nil
}
