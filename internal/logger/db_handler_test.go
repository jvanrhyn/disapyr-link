package logger

import (
"context"
"log/slog"
"os"
"testing"
"time"
)

// ---- MultiHandler tests ----

func TestMultiHandlerFanout(t *testing.T) {
h1 := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
h2 := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn})
multi := NewMultiHandler(h1, h2)
log := slog.New(multi)

ctx := context.Background()
if !multi.Enabled(ctx, slog.LevelDebug) {
t.Fatal("should be enabled at Debug (h1 accepts it)")
}
if !multi.Enabled(ctx, slog.LevelWarn) {
t.Fatal("should be enabled at Warn")
}
log.Info("info message", "key", "value")
log.Warn("warn message", "key", "value2")
log.With("request_id", "abc123").Warn("with attrs")
log.WithGroup("http").Warn("grouped")
}

// ---- DBHandler tests ----

func TestDBHandlerNilPool(t *testing.T) {
h := NewDBHandler(nil, slog.LevelWarn)
defer h.Stop()
log := slog.New(h)
log.Warn("warn", "safe", true)
log.Error("error", "safe", true)
// Give async goroutine time to process
time.Sleep(50 * time.Millisecond)
}

func TestDBHandlerNoSendOnClosedChannel(t *testing.T) {
// Stop() must not cause a panic on subsequent Handle() calls.
h := NewDBHandler(nil, slog.LevelDebug)
h.Stop()
// After Stop, Handle must be a no-op (not panic).
log := slog.New(h)
log.Warn("after stop — must not panic")
}

func TestDBHandlerStopIdempotent(t *testing.T) {
h := NewDBHandler(nil, slog.LevelWarn)
h.Stop()
h.Stop() // second call must not panic (double-close guard)
}

func TestDBHandlerChildHandlerStopNoPanic(t *testing.T) {
// Stop() on a child handler derived via WithAttrs must not double-close.
h := NewDBHandler(nil, slog.LevelWarn)
child := h.WithAttrs([]slog.Attr{slog.String("k", "v")}).(*DBHandler)
child.Stop() // closes the shared channel
h.Stop()     // second close — must not panic
}

func TestDBHandlerWithAttrsGroupScoping(t *testing.T) {
// Attrs added before WithGroup must NOT receive the group prefix.
h := NewDBHandler(nil, slog.LevelDebug)
defer h.Stop()

var captured map[string]any
// Instrument by directly calling Handle.
r := slog.NewRecord(time.Now(), slog.LevelWarn, "test", 0)

// Simulate: log.With("a", 1).WithGroup("g").Warn("msg", "b", 2)
h2 := h.WithAttrs([]slog.Attr{slog.String("a", "1")})
h3 := h2.WithGroup("g")
h3.(*DBHandler).Handle(context.Background(), r)

// Give async goroutine time to queue
time.Sleep(10 * time.Millisecond)

// We can't inspect the DB without a live connection, but we can verify
// Handle didn't panic and the handler's preAttrs key is "a" (not "g.a").
d := h3.(*DBHandler)
if _, ok := d.preAttrs["g.a"]; ok {
t.Error("group prefix was incorrectly applied to pre-group attr 'a'")
}
if _, ok := d.preAttrs["a"]; !ok {
t.Error("pre-group attr 'a' should be stored without group prefix")
}
_ = captured
}

func TestDBHandlerGroupAttrInRecord(t *testing.T) {
// slog.Group values in the record should be flattened to "group.key".
m := make(map[string]any)
resolveAttr(m, "", slog.Group("req",
slog.String("method", "GET"),
slog.Int("status", 200),
))
if m["req.method"] != "GET" {
t.Errorf("expected req.method=GET, got %v", m["req.method"])
}
if m["req.status"] != int64(200) {
t.Errorf("expected req.status=200, got %v", m["req.status"])
}
}
