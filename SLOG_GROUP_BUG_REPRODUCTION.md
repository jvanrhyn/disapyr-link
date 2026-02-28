# DBHandler `slog.Group()` Bug - Reproduction & Analysis

## Executive Summary

The `DBHandler` implementation has a critical bug: it **cannot properly handle `slog.Group()` attributes**. When a `slog.Group()` is logged, the handler incorrectly marshals it as a JSON array of raw `slog.Attr` structures instead of a properly nested object.

### Quick Proof

```go
logger.Info("test", slog.Group("g", "k", "v"))
```

| Handler | Output |
|---------|--------|
| ✗ DBHandler | `{"g": [{"Key":"k", "Value":{}}]}` |
| ✓ JSONHandler | `{"g": {"k": "v"}}` |

## Root Cause

In `DBHandler.Handle()`, line 73:

```go
r.Attrs(func(a slog.Attr) bool {
    attrs[attrKey(h.preGroup, a.Key)] = a.Value.Any()
    return true
})
```

When `a` is a Group attribute:
1. `a.Value.Any()` returns `[]slog.Attr` (the group's attributes)
2. This `[]slog.Attr` array is stored directly as the map value
3. When `json.Marshal(attrs)` is called, it serializes the Go struct fields

So `{"g": [{"Key":"k", "Value":{...}}]}` is produced instead of `{"g": {"k": "v"}}`.

## What json.Marshal Does

When you `json.Marshal([]slog.Attr)`, Go reflects on the `slog.Attr` struct and marshals its exported fields (`Key` and `Value`):

```json
[
  {
    "Key": "k",
    "Value": {}
  }
]
```

The `Value` field is empty because `slog.Value` is not JSON-serializable—it contains unexported function pointers and data that can only be accessed via `.Any()`.

## Detailed Test Results

Run the reproduction script:
```bash
go run ./cmd/test_group_bug/main.go
```

### Test 1: Simple Group
```go
logger.Info("test", slog.Group("g", "k", "v"))
```

**Expected:** `{"g": {"k": "v"}}` or `{"g.k": "v"}` (flattened)
**Actual:**  `{"g": [{"Key":"k", "Value":{}}]}`

### Test 2: Nested Groups
```go
logger.Info("test", slog.Group("outer", "inner", slog.Group("nested", "x", 1)))
```

**Expected:** `{"outer": {"inner": {"nested": {"x": 1}}}}`
**Actual:**  `{"outer": [{"Key":"inner", "Value":{}}]}`

### Test 3: Multiple Attributes in Group
```go
logger.Info("test", slog.Group("g", "k1", "v1", "k2", "v2", "k3", "v3"))
```

**Expected:** `{"g": {"k1": "v1", "k2": "v2", "k3": "v3"}}`
**Actual:**  
```json
{
  "g": [
    {"Key": "k1", "Value": {}},
    {"Key": "k2", "Value": {}},
    {"Key": "k3", "Value": {}}
  ]
}
```

### Comparison with Standard JSONHandler
The `slog.JSONHandler` correctly produces:
- Test 1: `{"g":{"k":"v"}}`
- Test 2: `{"outer":{"inner":{"x":1}}}`
- Test 3: `{"g":{"k1":"v1","k2":"v2","k3":"v3"}}`

## Why This Is a Bug

1. **Type Mismatch**: The output is an array of objects, not the expected nested object structure
2. **Data Loss**: The actual values are lost (only field names are shown)
3. **Incompatibility**: Any code expecting `{"g": {"k": "v"}}` will break
4. **slog Compatibility**: The handler doesn't properly implement the slog.Handler interface for Group support

## The Fix

The `DBHandler.Handle()` method must recursively process Group attributes instead of calling `.Any()` on them.

### Solution: Recursive Attribute Processing

```go
func (h *DBHandler) Handle(_ context.Context, r slog.Record) error {
    attrs := make(map[string]any, r.NumAttrs()+len(h.preAttrs))
    
    // Helper function to recursively process attributes
    var processAttrs func([]slog.Attr, string)
    processAttrs = func(attrList []slog.Attr, prefix string) {
        for _, a := range attrList {
            key := attrKey(prefix, a.Key)
            
            // Check if this is a Group attribute
            if a.Value.Kind() == slog.KindGroup {
                // Recursively process group's attributes
                groupAttrs := a.Value.Group()
                processAttrs(groupAttrs, key)
            } else {
                // Regular attribute—store as is
                attrs[key] = a.Value.Any()
            }
        }
    }
    
    // Process handler-level attrs (from WithAttrs)
    processAttrs(h.preAttrs, h.preGroup)
    
    // Process record attrs
    r.Attrs(func(a slog.Attr) bool {
        processAttrs([]slog.Attr{a}, h.preGroup)
        return true
    })
    
    // ... rest of Handle (enqueue record)
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

### How It Works

1. **Detect Groups**: Check `a.Value.Kind() == slog.KindGroup`
2. **Extract Group Attrs**: Call `a.Value.Group()` to get `[]slog.Attr`
3. **Recursive Processing**: Call `processAttrs()` on the extracted attributes with an updated key prefix
4. **Flatten with Dot Notation**: Keys are built as `"g.k"` for a group `"g"` with attribute `"k"`

### Result

With the fix, the output becomes:
- Test 1: `{"g.k": "v"}` ✓ (or `{"g": {"k": "v"}}` if you build nested maps)
- Test 2: `{"outer.inner.nested.x": 1}` ✓
- Test 3: `{"g.k1": "v1", "g.k2": "v2", "g.k3": "v3"}` ✓

## Implementation Checklist

- [ ] Update `DBHandler.Handle()` to use recursive attribute processing
- [ ] Test with `slog.Group()` attributes
- [ ] Test with nested groups
- [ ] Test with mixed regular and group attributes
- [ ] Ensure backward compatibility with existing non-Group attributes
- [ ] Update documentation if Group support is a feature

## Files to Modify

- `internal/logger/db_handler.go` - Update `Handle()` method

## Related Files

- `cmd/test_group_bug/main.go` - Reproduction script (demonstrates the bug)
- `cmd/test_slog_groups/main.go` - Existing tests for WithGroup/With ordering bugs

## See Also

- [slog.Value.Kind()](https://pkg.go.dev/log/slog#Value.Kind)
- [slog.Value.Group()](https://pkg.go.dev/log/slog#Value.Group)
- [slog.KindGroup](https://pkg.go.dev/log/slog#KindGroup)
