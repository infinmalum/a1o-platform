package channeltype

// BaseURL returns the default upstream base URL for a channel kind, or ""
// when the admin must supply one (Azure deployment hosts, the compatible
// kind's full version-prefixed prefix, proxy pass-through targets).
func BaseURL(kind int) string {
	switch kind {
	case OpenAI, OpenAIResponses:
		return "https://api.openai.com"
	case Anthropic:
		return "https://api.anthropic.com"
	case Gemini:
		return "https://generativelanguage.googleapis.com"
	case Minimax:
		return "https://api.minimax.chat"
	case Coze:
		return "https://api.coze.com"
	case Cloudflare:
		return "https://api.cloudflare.com"
	case DeepL:
		return "https://api-free.deepl.com"
	case TypeSafe:
		return "https://api.typesafe.ai"
	}
	return ""
}
