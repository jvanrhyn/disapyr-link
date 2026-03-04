package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jvanrhyn/disapyr-link/internal/model"
)

// ErrNotFound is returned when a secret is missing, already consumed, or expired.
var ErrNotFound = errors.New("secret not found or expired")

// SecretRepository handles persistence of secrets.
type SecretRepository struct {
	db *pgxpool.Pool
}

// New returns a new SecretRepository backed by the given connection pool.
func New(db *pgxpool.Pool) *SecretRepository {
	return &SecretRepository{db: db}
}

// Store persists an encrypted secret with its token.
func (r *SecretRepository) Store(ctx context.Context, token string, input model.CreateSecretInput) error {
	var expiresAt *time.Time
	if input.ExpiresIn > 0 {
		t := time.Now().Add(time.Duration(input.ExpiresIn) * time.Second)
		expiresAt = &t
	}

	_, err := r.db.Exec(ctx,
		`INSERT INTO secrets (token, ciphertext, nonce, expires_at)
		 VALUES ($1, $2, $3, $4)`,
		token, input.Ciphertext, input.Nonce, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("insert secret: %w", err)
	}

	r.recordEvent(ctx, "created", 1)
	return nil
}

// FetchAndDelete atomically retrieves and deletes a secret in a single statement.
// Returns ErrNotFound if the token does not exist or has expired.
func (r *SecretRepository) FetchAndDelete(ctx context.Context, token string) (*model.Secret, error) {
	var s model.Secret

	// DELETE ... RETURNING is inherently atomic — no separate transaction required.
	err := r.db.QueryRow(ctx,
		`DELETE FROM secrets
		 WHERE token = $1
		   AND (expires_at IS NULL OR expires_at > NOW())
		 RETURNING id, token, ciphertext, nonce, expires_at, created_at`,
		token,
	).Scan(&s.ID, &s.Token, &s.Ciphertext, &s.Nonce, &s.ExpiresAt, &s.CreatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("fetch and delete secret: %w", err)
	}

	r.recordEvent(ctx, "retrieved", 1)
	return &s, nil
}

// CleanupExpired deletes all secrets whose expiry time has passed and records
// the count as an "expired" event. Returns the number of secrets deleted and
// any error encountered. Event recording is best-effort and never causes the
// error return to be set. It is intended to be called from a background goroutine.
func (r *SecretRepository) CleanupExpired(ctx context.Context) (int64, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM secrets WHERE expires_at IS NOT NULL AND expires_at < NOW()`)
	if err != nil {
		return 0, fmt.Errorf("cleanup expired secrets: %w", err)
	}
	n := tag.RowsAffected()
	if n > 0 {
		r.recordEvent(ctx, "expired", n)
	}
	return n, nil
}

// recordEvent inserts a lifecycle event into secret_events on a best-effort basis.
// Errors are silently discarded so that a stats write failure never interrupts the
// critical secret creation or retrieval path.
func (r *SecretRepository) recordEvent(ctx context.Context, eventType string, count int64) {
	_, _ = r.db.Exec(ctx,
		`INSERT INTO secret_events (event_type, count) VALUES ($1, $2)`,
		eventType, count,
	)
}

