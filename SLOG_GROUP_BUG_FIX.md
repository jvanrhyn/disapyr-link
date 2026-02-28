# DBHandler slog.Group() Fix - Implementation Guide

## Problem Summary

The current `DBHandler.Handle()` method calls `a.Value.Any()` on all attributes, including Group attributes. When `a` is a Group, `Any()` returns `[]slog.Attr`, which gets marshaled as a JSON array of raw struct fields instead of a nested object.

**Current behavior:**
```
logger.Info("test", slog.Group("g", "k", "v"))
↓
{"g": [{"Key":"k", "Value":{}}]}  ← WRONG! Raw slog.Attr structs
```

**Expected behavior:**
```
logger.Info("test", slog.Group("g", "k", "v"))
↓
{"g.k": "v"}  ← CORRECT! Flattened with dot notation
```

---

## Current Implementation (Buggy)

**File: `internal/logger/db_handler.go` (lines 62-93)**

```go
// Handle implements slog.Handler. It marshals the record attrs to JSON and
// enqueues it for async insertion. Returns immediately.
func (h *DBHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := make(map[string]any, r.NumAttrs()+len(h.preAttrs))

	// Merge handler-level attrs (from WithAttrs).
	for _, a := range h.preAttrs {
		attrs[attrKey(h.preGroup, a.Key)] = a.Value.Any()  // BUG: Doesn't handle Groups
	}

	// Merge record attrs.
	r.Attrs(func(a slog.Attr) bool {
		attrs[attrKey(h.preGroup, a.Key)] = a.Value.Any()  // BUG: Doesn't handle Groups
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
		// Buffer full — drop. stdout handler still has the record.
	}
	return nil
}
```

### Problems
1. Line 68: Calls `a.Value.Any()` without checking if it's a Group
2. Line 73: Same issue in the record attrs loop
3. When `a.Value.Any()` returns `[]slog.Attr`, it's stored directly
4. `json.Marshal` then serializes the Go struct, producing `[{"Key":"k","Value":{}}]`

---

## Fixed Implementation

```go
// Handle implements slog.Handler. It marshals the record attrs to JSON and
// enqueues it for async insertion. Returns immediately.
func (h *DBHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := make(map[string]any, r.NumAttrs()+len(h.preAttrs))

	// Helper function to recursively process attributes, handling Groups specially.
	var processAttrs func([]slog.Attr, string)
	processAttrs = func(attrList []slog.Attr, prefix string) {
		for _, a := range attrList {
			key := attrKey(prefix, a.Key)

			// Check if this attribute is a Group
			if a.Value.Kind() == slog.KindGroup {
				// Recursively process the group's attributes with the group key as prefix
				groupAttrs := a.Value.Group()
				processAttrs(groupAttrs, key)
			} else {
				// Regular attribute—store its value directly
				attrs[key] = a.Value.Any()
			}
		}
	}

	// Process handler-level attrs (from WithAttrs) with the current group prefix
	processAttrs(h.preAttrs, h.preGroup)

	// Process record attrs with the current group prefix
	r.Attrs(func(a slog.Attr) bool {
		processAttrs([]slog.Attr{a}, h.preGroup)
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
		// Buffer full — drop. stdout handler still has the record.
	}
	return nil
}
```

### Key Changes

1. **Added recursive helper function** (lines 65-77):
   - Takes `[]slog.Attr` and a key prefix
   - For each attribute, builds the full key with the prefix
   - **Checks `a.Value.Kind() == slog.KindGroup`**
   - If it's a Group, recursively processes its attributes
   - Otherwise, stores the value using `.Any()`

2. **Updated preAttrs processing** (line 79):
   - Calls `processAttrs()` instead of directly iterating

3. **Updated record attrs processing** (lines 81-84):
   - Calls `processAttrs()` for each attribute

---

## Step-by-Step Trace

### Example: `logger.Info("test", slog.Group("g", "k", "v"))`

#### Before Fix (Buggy)
```
r.Attrs() delivers:
  Attr{Key:"g", Value:<Group>}

Handle() calls:
  a.Value.Any() → []slog.Attr{Attr{Key:"k", Value:"v"}}
  attrs["g"] = []slog.Attr{Attr{Key:"k", Value:"v"}}

json.Marshal(attrs) sees:
  {"g": [{"Key":"k", "Value":{}}]}  ← Raw slog.Attr structs!
```

#### After Fix
```
r.Attrs() delivers:
  Attr{Key:"g", Value:<Group>}

Handle() calls:
  processAttrs([Attr{Key:"g", Value:<Group>}], "")
    ↓
    a.Value.Kind() == slog.KindGroup? YES
    ↓
    groupAttrs = a.Value.Group() → [Attr{Key:"k", Value:"v"}]
    ↓
    processAttrs([Attr{Key:"k", Value:"v"}], "g")
      ↓
      a.Value.Kind() == slog.KindGroup? NO
      ↓
      attrs["g.k"] = "v"

json.Marshal(attrs) sees:
  {"g.k": "v"}  ✓ Correct!
```

---

## Test Cases

After applying the fix, test with the reproduction script:

```bash
go run ./cmd/test_group_bug/main.go
```

### Expected Results

| Test Case | Output |
|-----------|--------|
| `slog.Group("g", "k", "v")` | `{"g.k": "v"}` |
| `slog.Group("outer", "inner", slog.Group("nested", "x", 1))` | `{"outer.inner.nested.x": 1}` |
| `slog.Group("g", "k1", "v1", "k2", "v2", "k3", "v3")` | `{"g.k1": "v1", "g.k2": "v2", "g.k3": "v3"}` |
| Regular attr mixed with group | `{"regular": 123, "g.k": "v"}` |

---

## Backward Compatibility

✓ **Fully backward compatible**

- Non-Group attributes work exactly as before
- `.WithGroup()` and `.With()` behavior unchanged
- Only adds support for `slog.Group()` which previously didn't work

---

## Performance Impact

✓ **Minimal performance impact**

- Only adds recursive function calls for Group attributes
- Regular attributes follow the same code path as before
- No additional memory allocation for non-Group cases

---

## Implementation Checklist

- [ ] Replace `Handle()` method in `internal/logger/db_handler.go`
- [ ] Test with simple Groups
- [ ] Test with nested Groups
- [ ] Test with mixed regular and Group attributes
- [ ] Run existing tests to ensure no regressions
- [ ] Run `cmd/test_group_bug/main.go` to verify the fix
- [ ] Update documentation if needed

---

## Related Code

**To understand `slog` attributes better:**

```go
// Check attribute kind
if attr.Value.Kind() == slog.KindGroup {
    // Get the group's attributes
    groupAttrs := attr.Value.Group()
}

// Get the actual value of a non-Group attribute
value := attr.Value.Any()
```

**Available Value kinds:**
- `slog.KindGroup` - A group of attributes
- `slog.KindInt64`, `slog.KindUint64`, etc. - Primitive types
- `slog.KindString`, `slog.KindBool`, `slog.KindFloat64`
- `slog.KindTime`, `slog.KindDuration`, `slog.KindAny`

---

## References

- [slog.Value documentation](https://pkg.go.dev/log/slog#Value)
- [slog.Value.Kind()](https://pkg.go.dev/log/slog#Value.Kind)
- [slog.Value.Group()](https://pkg.go.dev/log/slog#Value.Group)
- [Go 1.21 structured logging](https://pkg.go.dev/log/slog)
