package model

import (
	"time"

	"github.com/google/uuid"
)

// Secret is the persisted domain object.
type Secret struct {
	ID         uuid.UUID
	Token      string
	Ciphertext []byte
	Nonce      []byte
	ExpiresAt  *time.Time
	CreatedAt  time.Time
}

// CreateSecretInput carries the browser-encrypted envelope from client to service.
// content_type and filename are encrypted inside the ciphertext (zero-knowledge).
type CreateSecretInput struct {
	Ciphertext []byte
	Nonce      []byte
	ExpiresIn  int // seconds; 0 = never expires
}

// SecretPayload is the JSON response returned on successful reveal.
// The client decrypts the ciphertext to obtain the metadata and data.
type SecretPayload struct {
	Ciphertext string `json:"ciphertext"` // base64-encoded ciphertext
	Nonce      string `json:"nonce"`      // base64-encoded 96-bit IV
}
