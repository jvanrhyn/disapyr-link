// Package logger provides a slog.Handler that writes log records to a
// PostgreSQL table, designed to be composed with the existing JSON stdout handler.
package logger

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// bufferSize is the number of log records that can be queued before drops start.
	bufferSize = 512
	// drainTimeout is the maximum time Stop() waits for the buffer to drain.
	drainTimeout = 10 * time.Second
)

// dbRecord is the payload queued for async insertion.
type dbRecord struct {
	ts    time.Time
	level string
	msg   string
	attrs map[string]any
}

// sharedStop holds shutdown state shared by all DBHandler copies produced by
// WithAttrs/WithGroup. Using a single pointer prevents double-close panics and
// ensures a single channel/goroutine pair regardless of how many child handlers
// are derived.
type sharedStop struct {
	stopped atomic.Bool
	once    sync.Once
	ch      chan dbRecord
	done    chan struct{}
}

// DBHandler is a slog.Handler that asynchronously inserts log records into the
// app_logs table. It never blocks the caller — if the buffer is full it drops
// the record silently (stdout always captures everything). Call Stop() on
// graceful shutdown to drain the buffer.
type DBHandler struct {
	pool     *pgxpool.Pool
	minLevel slog.Level
	ss       *sharedStop

	// preAttrs stores fully-qualified key→value pairs, with the group prefix
	// already applied at the time WithAttrs was called. This prevents
	// subsequent WithGroup calls from double-prefixing earlier attrs.
	preAttrs map[string]any
	// preGroup is the active group prefix for NEW record attrs.
	preGroup string
}

// NewDBHandler creates a DBHandler and starts its background writer goroutine.
func NewDBHandler(pool *pgxpool.Pool, minLevel slog.Level) *DBHandler {
	ss := &sharedStop{
		ch:   make(chan dbRecord, bufferSize),
		done: make(chan struct{}),
	}
	h := &DBHandler{
		pool:     pool,
		minLevel: minLevel,
		ss:       ss,
		preAttrs: make(map[string]any),
	}
	go h.run()
	return h
}

// Enabled implements slog.Handler.
func (h *DBHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.minLevel
}

// Handle implements slog.Handler. It marshals the record attrs to JSON and
// enqueues it for async insertion. Returns immediately.
func (h *DBHandler) Handle(_ context.Context, r slog.Record) error {
	// Check stopped BEFORE any channel send to avoid send-on-closed-channel panic.
	if h.ss.stopped.Load() {
		return nil
	}

	attrs := make(map[string]any, len(h.preAttrs)+r.NumAttrs())
	for k, v := range h.preAttrs {
		attrs[k] = v
	}
	r.Attrs(func(a slog.Attr) bool {
		resolveAttr(attrs, h.preGroup, a)
		return true
	})

	ts := r.Time
	if ts.IsZero() {
		ts = time.Now()
	}

	rec := dbRecord{ts: ts, level: r.Level.String(), msg: r.Message, attrs: attrs}

	// Re-check after building rec — minimises window between check and send.
	if h.ss.stopped.Load() {
		return nil
	}
	select {
	case h.ss.ch <- rec:
	default:
		// Buffer full — drop silently. stdout handler always captures everything.
	}
	return nil
}

// WithAttrs implements slog.Handler. Attrs are pre-processed into the preAttrs
// map with the current group prefix already applied, so future WithGroup calls
// cannot retroactively re-prefix them.
func (h *DBHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	merged := make(map[string]any, len(h.preAttrs)+len(attrs))
	for k, v := range h.preAttrs {
		merged[k] = v
	}
	for _, a := range attrs {
		resolveAttr(merged, h.preGroup, a)
	}
	return &DBHandler{
		pool:     h.pool,
		minLevel: h.minLevel,
		ss:       h.ss,
		preAttrs: merged,
		preGroup: h.preGroup,
	}
}

// WithGroup implements slog.Handler.
func (h *DBHandler) WithGroup(name string) slog.Handler {
	prefix := name
	if h.preGroup != "" {
		prefix = h.preGroup + "." + name
	}
	return &DBHandler{
		pool:     h.pool,
		minLevel: h.minLevel,
		ss:       h.ss,
		// preAttrs already baked — not re-prefixed
		preAttrs: h.preAttrs,
		preGroup: prefix,
	}
}

// Stop signals the writer to stop and waits up to drainTimeout for the buffer
// to drain. Safe to call multiple times; only the first call has effect.
func (h *DBHandler) Stop() {
	h.ss.once.Do(func() {
		h.ss.stopped.Store(true)
		close(h.ss.ch)
	})
	select {
	case <-h.ss.done:
	case <-time.After(drainTimeout):
		// Drain timed out — remaining queued records are dropped.
		// The process is shutting down and stdout already captured everything.
	}
}

// run is the background writer goroutine. It drains h.ss.ch until closed.
func (h *DBHandler) run() {
	defer close(h.ss.done)
	for rec := range h.ss.ch {
		h.insert(rec)
	}
}

// insert writes a single record to the database.
func (h *DBHandler) insert(rec dbRecord) {
	if h.pool == nil {
		return
	}
	var attrsJSON []byte
	if len(rec.attrs) > 0 {
		var err error
		if attrsJSON, err = json.Marshal(rec.attrs); err != nil {
			attrsJSON = nil
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	//nolint:errcheck — failure drops silently; stdout is the authoritative log.
	_, _ = h.pool.Exec(ctx,
		`INSERT INTO app_logs (ts, level, msg, attrs) VALUES ($1, $2, $3, $4)`,
		rec.ts, rec.level, rec.msg, attrsJSON,
	)
}

// resolveAttr flattens a slog.Attr into m, resolving LogValuers and recursing
// into slog.Group values so they produce nested "group.key" entries.
func resolveAttr(m map[string]any, group string, a slog.Attr) {
	// Resolve LogValuer implementations (e.g. time.Time, custom types).
	a.Value = a.Value.Resolve()

	if a.Value.Kind() == slog.KindGroup {
		sub := a.Key
		if group != "" {
			sub = group + "." + a.Key
		}
		for _, ga := range a.Value.Group() {
			resolveAttr(m, sub, ga)
		}
		return
	}

	key := a.Key
	if group != "" {
		key = group + "." + a.Key
	}
	m[key] = a.Value.Any()
}
