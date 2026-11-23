package controller

import (
	"encoding/json"
	"testing"

	"github.com/infinmalum/a1o-platform/model"
	"github.com/stretchr/testify/require"
)

func TestGeminiImageRequestAndProducts(t *testing.T) {
	gen := &model.ImageGeneration{Model: "gemini-3.1-flash-image", Prompt: "A book", Size: "9:16", Quality: "2K"}
	body, err := geminiImageRequest(gen)
	require.NoError(t, err)
	require.JSONEq(t, `{"contents":[{"role":"user","parts":[{"text":"A book"}]}],"generationConfig":{"responseModalities":["TEXT","IMAGE"],"imageConfig":{"aspectRatio":"9:16","imageSize":"2K"}}}`, string(body))
	require.Error(t, validateImageGeneration(gen.Model, gen.Prompt, "1024x1024", "2K", 1))
	require.Error(t, validateImageGeneration("gemini-2.5-flash-image", gen.Prompt, "9:16", "2K", 1))
	require.Error(t, validateImageGeneration(gen.Model, gen.Prompt, "9:16", "2K", 2))

	result := json.RawMessage(`{"candidates":[{"content":{"parts":[{"text":"Here is the result"},{"thought":true,"inlineData":{"mimeType":"image/png","data":"dGhvdWdodA=="}},{"inlineData":{"mimeType":"image/png","data":"ZmluYWw="}}]}}]}`)
	images, err := geminiImageResults(result)
	require.NoError(t, err)
	require.Equal(t, []imageResultItem{{B64: "ZmluYWw="}}, images)
}
