# slog Groups and Attributes Bug Analysis

## Executive Summary

The current `DBHandler` implementation has a **critical bug** in how it handles attributes added before group creation. When you chain `.With()` calls before `.WithGroup()`, attributes are incorrectly prefixed with the group name even though they were added at a higher scope level.

## The Bug - Test Case 4

```go
log.With("outside", 1).WithGroup("group").With("inside", 2).Info("msg")
```

### Expected Behavior (Standard slog.JSONHandler)
```json
{
  "outside": 1,
  "group": {
    "inside": 2
  }
}
```

The `"outside"` key is at the root level because it was added **before** entering the group.

### Current Buggy Behavior (DBHandler)
```go
// Flattened representation
{
  "group.outside": 1,    // ← WRONG! Should be just "outside"
  "group.inside": 2      // ✓ Correct
}
```

The attribute `"outside"` is incorrectly prefixed with `"group"` because the implementation copies all `preAttrs` to the new handler when `WithGroup()` is called.

## Root Cause Analysis

### How the Bug Happens

1. **`log.With("outside", 1)`**
   - Creates handler with: `preAttrs=[{Key:"outside", Value:1}]`, `preGroup=""`

2. **`.WithGroup("group")`**
   ```go
   func (h *DBHandler) WithGroup(name string) slog.Handler {
       prefix := name
       if h.preGroup != "" {
           prefix = h.preGroup + "." + name
       }
       return &DBHandler{
           preAttrs: h.preAttrs,  // ← COPIES attributes from previous handler!
           preGroup: prefix,      // ← "group"
       }
   }
   ```
   - Creates new handler with: `preAttrs=[{Key:"outside", Value:1}]`, `preGroup="group"`
   - **BUG**: The "outside" attribute is now associated with group "group"!

3. **`.With("inside", 2)`**
   - Creates handler with: `preAttrs=[{Key:"outside", Value:1}, {Key:"inside", Value:2}]`, `preGroup="group"`

4. **`.Info("msg")`**
   ```go
   func (h *DBHandler) Handle(_ context.Context, r slog.Record) error {
       attrs := make(map[string]any)
       
       // ALL preAttrs get the current preGroup prefix!
       for _, a := range h.preAttrs {
           attrs[attrKey(h.preGroup, a.Key)] = a.Value.Any()
       }
       // ...
   }
   ```
   - Applies `preGroup` ("group") to ALL `preAttrs`:
     - "outside" → "group.outside" ✗ WRONG
     - "inside" → "group.inside" ✓ Correct

### Visual Trace

```
log.With("outside", 1)
  ↓ preAttrs=[outside=1], preGroup=""
.WithGroup("group")
  ↓ preAttrs=[outside=1] ← COPIED! (Problem here)
  ↓ preGroup="group"
.With("inside", 2)
  ↓ preAttrs=[outside=1, inside=2]
  ↓ preGroup="group"
.Info("msg")
  ↓ Handler prefixes ALL preAttrs with preGroup="group"
  ↓ Result: {"group.outside": 1, "group.inside": 2}  ← "outside" should NOT have prefix!
```

## Other Related Issues

### Test Case 3: Attributes Added Before WithGroup
```go
log.With("k3", "v3").WithGroup("g2").Info("msg", "k4", "v4")
```

**Expected (Standard slog):**
```json
{
  "k3": "v3",
  "g2": {
    "k4": "v4"
  }
}
```

**Current (DBHandler - BUGGY):**
```json
{
  "g2.k3": "v3",   // ← WRONG! Should be just "k3"
  "g2.k4": "v4"    // ✓ Correct
}
```

### Test Case 5: Multiple Nested Groups
```go
log.With("root", 1)
   .WithGroup("g1")
   .With("mid", 2)
   .WithGroup("g2")
   .With("deep", 3)
   .Info("msg")
```

**Expected (Standard slog):**
```json
{
  "root": 1,
  "g1": {
    "mid": 2,
    "g2": {
      "deep": 3
    }
  }
}
```

**Current (DBHandler - BUGGY):**
```json
{
  "g1.g2.root": 1,    // ← WRONG! Should be just "root"
  "g1.g2.mid": 2,     // ← WRONG! Should be "g1.mid"
  "g1.g2.deep": 3     // ✓ Correct
}
```

## Design Issues

### The Flattening Approach

The current implementation uses dot-notation to flatten the group hierarchy:
```
{
  "g1.g2.key": value
}
```

This approach has two problems:

1. **Incorrect Scope Tracking**: Attributes added before a group shouldn't have the group prefix, but the current design applies prefixes to all copied `preAttrs`.

2. **Information Loss**: The flattened representation loses the semantic distinction between:
   - An attribute added at the root level
   - An attribute added at group "g1" level
   - An attribute added at group "g1.g2" level

### How Standard slog Handles Groups

Standard slog maintains proper group scoping through nested handler wrapping:
- Attributes added at a level stay at that level
- When a group is entered, subsequent attributes are nested under that group
- The `JSONHandler` preserves this structure as nested JSON

The `DBHandler` tries to flatten this into a single map with dot notation, but loses track of scope in the process.

## Proposed Solutions

### Option A: Track Attributes by Group Scope ⭐ RECOMMENDED

Instead of a flat `preAttrs` list, track attributes at each group level:

```go
type DBHandler struct {
    pool      *pgxpool.Pool
    minLevel  slog.Level
    ch        chan dbRecord
    stopOnce  sync.Once
    done      chan struct{}
    
    // Track attributes at each scope
    attrsByScope map[string][]slog.Attr  // key: "root", "g1", "g1.g2", etc.
    preGroup     string                  // Current group prefix
}
```

Then when handling attributes:
- Only apply the current `preGroup` prefix to attributes added at the current level
- Attributes from parent scopes are prefixed with their own scope

**Pros:**
- Correctly preserves scope semantics
- Handles nested groups properly
- Can still flatten to dot notation for DB storage

**Cons:**
- Requires restructuring the handler
- More complex state management

### Option B: Use Nested Maps Instead of Flat Keys

Store attributes in a nested map structure that mirrors the group hierarchy:

```go
type DBHandler struct {
    // ...
    nestedAttrs map[string]interface{}  // Preserves hierarchy
}
```

When storing in DB, flatten only at insertion time.

**Pros:**
- More accurately reflects slog structure
- Clearer semantics
- Can produce properly nested JSON

**Cons:**
- Requires custom serialization logic
- Changes the `attrs` format in database

### Option C: Don't Copy preAttrs in WithGroup

Start fresh with empty attributes when entering a group:

```go
func (h *DBHandler) WithGroup(name string) slog.Handler {
    prefix := name
    if h.preGroup != "" {
        prefix = h.preGroup + "." + name
    }
    return &DBHandler{
        preAttrs: nil,  // ← Don't copy!
        preGroup: prefix,
    }
}
```

**Pros:**
- Simple fix

**Cons:**
- Loses attributes added before the group
- Doesn't match standard slog behavior

### Option D: Track Attribute "Group Depth"

Annotate each attribute with the group depth at which it was added:

```go
type AttrWithDepth struct {
    Attr  slog.Attr
    Depth int  // Which group nesting level this was added at
}
```

Only apply group prefix if current depth >= attribute's depth.

**Pros:**
- Preserves all attributes
- Correct scoping
- Works with multiple nested groups

**Cons:**
- Most complex solution
- Requires tracking depth throughout handler chain

## Recommended Fix: Option A

**Track attributes by their scope level**, not in a flat list.

### Implementation Sketch

```go
func (h *DBHandler) WithGroup(name string) slog.Handler {
    prefix := name
    if h.preGroup != "" {
        prefix = h.preGroup + "." + name
    }
    
    // New handler at group level has empty attrs
    // But inherits parent attrs through the handler chain
    return &DBHandler{
        pool:        h.pool,
        minLevel:    h.minLevel,
        ch:          h.ch,
        done:        h.done,
        attrsByScope: cloneScopes(h.attrsByScope), // Copy parent scopes
        currentScope: prefix,
    }
}

func (h *DBHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
    // Append to current scope's attributes
    newScopes := cloneScopes(h.attrsByScope)
    newScopes[h.preGroup] = append(
        newScopes[h.preGroup],
        attrs...,
    )
    
    return &DBHandler{
        pool:        h.pool,
        minLevel:    h.minLevel,
        ch:          h.ch,
        done:        h.done,
        attrsByScope: newScopes,
        currentScope: h.preGroup,
    }
}

func (h *DBHandler) Handle(ctx context.Context, r slog.Record) error {
    attrs := make(map[string]any)
    
    // Flatten all scopes with proper prefixes
    for scope, scopeAttrs := range h.attrsByScope {
        for _, a := range scopeAttrs {
            key := attrKey(scope, a.Key)
            attrs[key] = a.Value.Any()
        }
    }
    
    // Record attributes also at current scope
    r.Attrs(func(a slog.Attr) bool {
        attrs[attrKey(h.preGroup, a.Key)] = a.Value.Any()
        return true
    })
    
    // ... rest of Handle
}
```

## Testing

See `cmd/test_slog_groups/main.go` for a complete reproduction script that demonstrates:

1. ✅ Test 1: Simple group with record attributes (works correctly by accident)
2. ✅ Test 2: Group with added attributes (works correctly by accident)
3. ❌ Test 3: Attributes before group are incorrectly prefixed
4. ❌ Test 4: **THE BUG** - Attributes before group are incorrectly prefixed
5. ❌ Test 5: Multiple nested groups cause cascading incorrectness

Run with:
```bash
go run ./cmd/test_slog_groups/main.go
```

Compare the "MockDBHandler output" with the "JSON HANDLER BEHAVIOR" sections to see the differences.

## Impact

### Data Integrity
- Attributes are stored with incorrect keys in the database
- Queries for attributes at specific scope levels will fail
- The semantic meaning of where an attribute came from is lost

### Affected Use Cases
- Any code that does: `log.With(...).WithGroup(...).With(...)`
- Any code that nests multiple groups: `log.WithGroup("g1").WithGroup("g2").With(...)`

### Severity
**HIGH** - This is a correctness issue that silently produces wrong results.

## Migration Notes

If this bug is fixed, existing log records in the database will have inconsistent data. Depending on your schema design, you may need to:

1. Accept that old and new records have different structures
2. Write a migration script to re-interpret old records
3. Keep backward-compatible key names during transition

The JSON schema design for `attrs JSONB` should handle both formats gracefully, or migration code should be prepared.
