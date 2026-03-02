package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jvanrhyn/disapyr-link/internal/model"
	"github.com/jvanrhyn/disapyr-link/internal/repository"
)

// ErrNotFound bubbles up from the repository for missing/expired secrets.
var ErrNotFound = repository.ErrNotFound

// ErrTooLarge is returned when the encrypted payload exceeds the configured limit.
var ErrTooLarge = errors.New("secret exceeds maximum allowed size")

// SecretService implements the business logic for creating and retrieving secrets.
type SecretService struct {
	repo     *repository.SecretRepository
	maxBytes int64
}

// New returns a new SecretService.
func New(repo *repository.SecretRepository, maxBytes int64) *SecretService {
	return &SecretService{repo: repo, maxBytes: maxBytes}
}

// Create validates the input, generates a retrieval token, and persists the secret.
// Returns the retrieval token on success.
func (s *SecretService) Create(ctx context.Context, input model.CreateSecretInput) (string, error) {
	if int64(len(input.Ciphertext)) > s.maxBytes {
		return "", ErrTooLarge
	}

	token, err := generateToken()
	if err != nil {
		return "", fmt.Errorf("generating token: %w", err)
	}

	if err := s.repo.Store(ctx, token, input); err != nil {
		return "", fmt.Errorf("storing secret: %w", err)
	}

	return token, nil
}

// Retrieve atomically fetches and deletes the secret for the given token.
func (s *SecretService) Retrieve(ctx context.Context, token string) (*model.Secret, error) {
	return s.repo.FetchAndDelete(ctx, token)
}

// generateToken produces a cryptographically random 128-bit hex string.
func generateToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
