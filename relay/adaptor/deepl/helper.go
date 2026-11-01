package deepl

import (
	"errors"
	"fmt"
	"strings"
)

// parseLangFromModelName extracts the DeepL target language from the model
// name. Both catalog language IDs and the historical deepl-LANG aliases are
// valid: a bare ISO code (DE, ZH), a regional code (EN-US), or the deepl-
// prefixed form (deepl-de). Anything else fails loudly instead of silently
// defaulting to a language the client never asked for.
func parseLangFromModelName(modelName string) (string, error) {
	lang := strings.TrimSpace(modelName)
	upper := strings.ToUpper(lang)
	if strings.EqualFold(lang, "deepl") || lang == "" {
		return "", errors.New("model name must carry a DeepL target language; use e.g. DE, EN-US, or deepl-de")
	}
	if len(lang) > 6 && strings.EqualFold(lang[:6], "deepl-") {
		upper = strings.ToUpper(lang[6:])
	}
	if len(upper) == 2 || (len(upper) == 5 && upper[2] == '-') {
		return upper, nil
	}
	return "", fmt.Errorf("model name %q does not carry a DeepL target language; use e.g. DE, EN-US, or deepl-de", modelName)
}
