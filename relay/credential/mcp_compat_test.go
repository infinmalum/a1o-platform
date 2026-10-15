package credential

import (
	"encoding/json"
	"testing"
)

// TestSealedEnvelopeShapeMatchesModelRecognizer pins the cross-package
// contract: model.mcpIsSealed recognizes a sealed value by unmarshalling
// format==1 and a non-empty ciphertext, so the envelope here must keep
// exactly those field names and types.
func TestSealedEnvelopeShapeMatchesModelRecognizer(t *testing.T) {
	store, err := NewStore(map[int][keyBytes]byte{1: {}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := store.Seal([]byte("anything"))
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Format     int    `json:"format"`
		Ciphertext string `json:"ciphertext"`
	}
	if err := json.Unmarshal([]byte(sealed), &envelope); err != nil {
		t.Fatalf("sealed value is not JSON: %v", err)
	}
	if envelope.Format != 1 || envelope.Ciphertext == "" {
		t.Fatalf("envelope shape drifted: format=%d ciphertext-empty=%t",
			envelope.Format, envelope.Ciphertext == "")
	}
}
