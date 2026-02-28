# Corrected slog Groups and Attributes Implementation

## Overview

This document shows Option A (recommended): tracking attributes by their scope level rather than in a flat list.

## The Problem with Current Implementation

```go
// Current (BUGGY) approach
type DBHandler struct {
    preAttrs []slog.Attr  // Flat list - loses scope info
    preGroup string       // Current group - but preAttrs don't know their scope!
}
```

When `WithGroup` is called, all `preAttrs` are copied over and then later prefixed with the new group. This causes attributes added BEFORE the group to be incorrectly prefixed.

## The Solution: Track Scope Per Attribute

```go
// Corrected approach
type DBHandler struct {
    pool      *pgxpool.Pool
    minLevel  slog.Level
    ch        chan dbRecord
    stopOnce  sync.Once
    done      chan struct{}
    
    // Track attributes at each scope level
    scopedAttrs map[string][]slog.Attr  // key: "root", "g1", "g1.g2", etc.
    currentScope string                 // Current group like "g1" or "g1.g2"
}
```

## Detailed Implementation

### NewDBHandler

```go
func NewDBHandler(pool *pgxpool.Pool, minLevel slog.Level) *DBHandler {
    h := &DBHandler{
        pool:     pool,
        minLevel: minLevel,
        ch:       make(chan dbRecord, bufferSize),
        done:     make(chan struct{}),
        
        scopedAttrs:  make(map[string][]slog.Attr), // Empty: no attrs yet
        currentScope: "root",                        // Start at root
    }
    go h.run()
    return h
}
```

### WithAttrs - Append to Current Scope

```go
func (h *DBHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
    // Clone the scoped attrs from parent
    scopedAttrs := h.cloneScopedAttrs()
    
    // Append new attrs to the current scope
    scopedAttrs[h.currentScope] = append(
        scopedAttrs[h.currentScope],
        attrs...,
    )
    
    return &DBHandler{
        pool:        h.pool,
        minLevel:    h.minLevel,
        ch:          h.ch,
        done:        h.done,
        scopedAttrs: scopedAttrs,
        currentScope: h.currentScope,  // Stay in same scope
    }
}
```

**Key Point:** New attributes are added to `h.currentScope`, not blindly copied.

### WithGroup - Enter a New Scope

```go
func (h *DBHandler) WithGroup(name string) slog.Handler {
    // Build the new scope name
    newScope := name
    if h.currentScope != "root" {
        newScope = h.currentScope + "." + name
    }
    
    // Clone the scoped attrs from parent
    scopedAttrs := h.cloneScopedAttrs()
    // Don't add any attrs here - they'll be added via WithAttrs at the new scope
    
    return &DBHandler{
        pool:        h.pool,
        minLevel:    h.minLevel,
        ch:          h.ch,
        done:        h.done,
        scopedAttrs: scopedAttrs,
        currentScope: newScope,  // Move to new scope
    }
}
```

**Key Point:** Entering a group changes `currentScope`. New attributes added after this will go to the new scope. Old attributes from parent scopes are preserved.

### Handle - Flatten Correctly

```go
func (h *DBHandler) Handle(_ context.Context, r slog.Record) error {
    attrs := make(map[string]any)
    
    // 1. Add all previously-scoped attributes with their proper prefixes
    for scope, scopeAttrs := range h.scopedAttrs {
        for _, a := range scopeAttrs {
            // Apply prefix only if not at root
            key := h.attrKey(scope, a.Key)
            attrs[key] = a.Value.Any()
        }
    }
    
    // 2. Add record attributes at current scope
    r.Attrs(func(a slog.Attr) bool {
        key := h.attrKey(h.currentScope, a.Key)
        attrs[key] = a.Value.Any()
        return true
    })
    
    rec := dbRecord{
        ts:    r.Time,
        level: r.Level.String(),
        msg:   r.Message,
        attrs: attrs,
    }
    if rec.ts.IsZero() {
        rec.ts = time.Now()
    }
    
    select {
    case h.ch <- rec:
    default:
        // Buffer full — drop
    }
    return nil
}
```

**Key Points:**
1. Each scope's attributes are prefixed with that scope's name (or nothing for "root")
2. Record attributes go into the current scope
3. No attribute is incorrectly re-prefixed

### Helper Methods

```go
// cloneScopedAttrs creates a shallow copy of the scoped attributes
func (h *DBHandler) cloneScopedAttrs() map[string][]slog.Attr {
    result := make(map[string][]slog.Attr, len(h.scopedAttrs))
    for scope, attrs := range h.scopedAttrs {
        // Shallow copy of the slice (attrs themselves are value types)
        result[scope] = append([]slog.Attr{}, attrs...)
    }
    return result
}

// attrKey returns "scope.key" for non-root scopes, otherwise "key"
func (h *DBHandler) attrKey(scope, key string) string {
    if scope == "root" || scope == "" {
        return key
    }
    return scope + "." + key
}
```

## How It Works - Visual Trace

### Example: `With("root", 1).WithGroup("g1").With("mid", 2).WithGroup("g2").With("deep", 3).Info("msg")`

```
Step 1: NewDBHandler()
  scopedAttrs: {"root": []}
  currentScope: "root"

Step 2: With("root", 1)
  scopedAttrs: {"root": [{Key:"root", Value:1}]}
  currentScope: "root"
  ✓ Attribute added to root scope

Step 3: WithGroup("g1")
  scopedAttrs: {"root": [{Key:"root", Value:1}]}  (unchanged)
  currentScope: "g1"
  ✓ Scope changed; attribute "root" stays in root scope

Step 4: With("mid", 2)
  scopedAttrs: {
    "root": [{Key:"root", Value:1}],
    "g1": [{Key:"mid", Value:2}]
  }
  currentScope: "g1"
  ✓ New attribute added to g1 scope

Step 5: WithGroup("g2")
  scopedAttrs: {
    "root": [{Key:"root", Value:1}],
    "g1": [{Key:"mid", Value:2}]
  }  (unchanged)
  currentScope: "g1.g2"
  ✓ Scope changed to g1.g2

Step 6: With("deep", 3)
  scopedAttrs: {
    "root": [{Key:"root", Value:1}],
    "g1": [{Key:"mid", Value:2}],
    "g1.g2": [{Key:"deep", Value:3}]
  }
  currentScope: "g1.g2"
  ✓ New attribute added to g1.g2 scope

Step 7: Info("msg")
  Handle flattens to:
  {
    "root": 1,          (from root scope)
    "g1.mid": 2,        (from g1 scope)
    "g1.g2.deep": 3     (from g1.g2 scope)
  }
  ✓ All attributes correctly prefixed!
```

## Behavior Comparison

### Current (Buggy) Implementation

```
Input:  With("a", 1).WithGroup("g").With("b", 2).Info("msg")
Output: {"g.a": 1, "g.b": 2}
        ✗ "a" is incorrectly prefixed with "g"
```

### Corrected Implementation

```
Input:  With("a", 1).WithGroup("g").With("b", 2).Info("msg")
Output: {"a": 1, "g.b": 2}
        ✓ "a" has no prefix (added before group)
        ✓ "b" has g prefix (added after entering group)
```

## Migration Strategy

If you have existing logs in the database with the buggy format:

### Option 1: Accept Different Formats (Pragmatic)
- Old logs have flat dot notation
- New logs have corrected dot notation
- Queries need to handle both formats

### Option 2: Create a Migration Script
```sql
-- For records with nested groups, you'd need to:
-- 1. Parse the JSON attrs
-- 2. Reconstruct which attributes belong to which scope
-- 3. Flatten with correct prefixes

-- This is complex because you can't reliably recover scope info
-- from the buggy output without knowing the original source code
```

### Option 3: Accept Data Loss (Nuclear Option)
```sql
-- Delete all logs and start fresh
DELETE FROM app_logs WHERE ts < '2024-01-01';
```

**Recommendation:** Option 1 - Accept both formats. Your query layer can handle both, or you can accept the data as-is since it's already in the database.

## Testing the Fix

Your reproduction script in `cmd/test_slog_groups/main.go` can be updated to test the corrected implementation:

```go
// Create a corrected handler
correctedHandler := NewCorrectedDBHandler(nil, slog.LevelInfo)
logger := slog.New(correctedHandler)

// Run the same test cases
logger.With("outside", 1).WithGroup("group").With("inside", 2).Info("msg")

// Verify output matches JSONHandler
expectedAttrs := map[string]interface{}{
    "outside": 1,
    "group.inside": 2,
}
```

## Performance Considerations

The corrected implementation:

1. **Memory:** Slightly higher - we maintain a map of scopes instead of a flat list
   - Trade-off: Correctness > minor memory increase

2. **CPU:** Similar - still O(n) to flatten attributes at Handle time
   - The map lookup (scope -> attrs) is O(log n) in practice

3. **GC:** Slightly higher - we clone scoped attrs in WithAttrs/WithGroup
   - Trade-off: Immutability/safety > minor GC pressure

These are all acceptable for a structured logging implementation.

## Summary

The corrected implementation:

✅ Properly tracks which scope each attribute belongs to
✅ Correctly handles attributes added before, during, and after group creation
✅ Matches standard slog semantics (even if flattened)
✅ Preserves all attribute information
✅ Maintains backward-compatible flattened JSON format for database storage
✅ Allows correct querying and filtering by scope

The key insight: **Don't flatten at handler creation time; flatten only at log emission time, using proper scope information.**
