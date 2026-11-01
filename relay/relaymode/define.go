package relaymode

const (
	Unknown = iota
	ChatCompletions
	Embeddings
	Moderations
	ImagesGenerations
	AudioSpeech
	AudioTranscription
	AudioTranslation
	// Proxy is a special relay mode for proxying requests to custom upstream
	Proxy
)
