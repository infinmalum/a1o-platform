package model

import (
	"encoding/json"
	"errors"

	"github.com/infinmalum/one-gateway/common/logger"
)

// CredentialSealer seals credential material for storage. relay/credential
// provides the production implementation; the indirection keeps model free
// of a dependency on a package that itself imports model.
type CredentialSealer interface {
	Seal(plaintext []byte) (string, error)
	Unseal(sealed string) ([]byte, error)
}

// CredentialSealerFactory builds the process sealer; main wires it to
// credential.DefaultStore. A nil factory or a nil sealer means credential
// sealing is not configured.
var CredentialSealerFactory func() (CredentialSealer, error)

// mcpSealedEnvelope mirrors the sealed-credential envelope just enough to
// recognize one. Legacy plaintext env/header JSON never carries these fields.
type mcpSealedEnvelope struct {
	Format     int    `json:"format"`
	Ciphertext string `json:"ciphertext"`
}

func mcpIsSealed(value string) bool {
	var envelope mcpSealedEnvelope
	return json.Unmarshal([]byte(value), &envelope) == nil &&
		envelope.Format == 1 && envelope.Ciphertext != ""
}

// sealMCPCredentialField encrypts one Env/Headers field before it reaches the
// database. Empty and already-sealed values pass through; a plaintext value
// without a configured sealer is rejected so new secrets never land in
// cleartext.
func sealMCPCredentialField(value *string) error {
	if value == nil || *value == "" || mcpIsSealed(*value) {
		return nil
	}
	sealer, err := credentialSealer()
	if err != nil {
		return err
	}
	sealed, err := sealer.Seal([]byte(*value))
	if err != nil {
		return err
	}
	*value = sealed
	return nil
}

// unsealMCPCredentialField decrypts one Env/Headers field in place after a
// load. Legacy plaintext passes through; a failed decryption keeps the
// envelope and logs, so one unreadable row degrades to a failing MCP call
// instead of erroring the whole list.
func unsealMCPCredentialField(scope string, value *string) {
	if value == nil || *value == "" || !mcpIsSealed(*value) {
		return
	}
	sealer, err := credentialSealer()
	if err != nil {
		logger.SysError("credential sealer unavailable, cannot unseal " + scope + ": " + err.Error())
		return
	}
	plaintext, err := sealer.Unseal(*value)
	if err != nil {
		logger.SysError("failed to unseal " + scope + ": " + err.Error())
		return
	}
	*value = string(plaintext)
}

// credentialSealer resolves the configured sealer, failing closed when
// credential sealing is not set up.
func credentialSealer() (CredentialSealer, error) {
	if CredentialSealerFactory == nil {
		return nil, errors.New("凭据加密未配置（UPSTREAM_CREDENTIAL_KEYS），无法保存 MCP 凭据")
	}
	sealer, err := CredentialSealerFactory()
	if err != nil {
		return nil, err
	}
	if sealer == nil {
		return nil, errors.New("凭据加密未配置（UPSTREAM_CREDENTIAL_KEYS），无法保存 MCP 凭据")
	}
	return sealer, nil
}
