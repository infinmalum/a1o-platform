package channeltype

// BaseURL returns the default upstream base URL for a channel kind, or ""
// when the admin must supply one (Azure deployment hosts, Gemini/Anthropic
// proxy targets, TypeSafe when pointed at a non-official SystemOne endpoint).
// OpenRouter's base is fixed by the kind: its unified API serves every wire
// it supports from https://openrouter.ai/api/v1.
func BaseURL(kind int) string {
	switch kind {
	case OpenAI, OpenAIResponses:
		return "https://api.openai.com"
	case Anthropic:
		return "https://api.anthropic.com"
	case Gemini:
		return "https://generativelanguage.googleapis.com"
	case Coze:
		return "https://api.coze.com"
	case Cloudflare:
		return "https://api.cloudflare.com"
	case DeepL:
		return "https://api-free.deepl.com"
	case TypeSafe:
		return "https://api.typesafe.ai"
	case OpenRouter:
		return "https://openrouter.ai/api/v1"
	}
	return ""
}
