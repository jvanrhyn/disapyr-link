# slog Groups and Attributes Bug - Complete Summary

## Files Created

This analysis includes the following files:

### 1. **Reproduction Script** (Executable)
📁 `cmd/test_slog_groups/main.go`
- Runnable Go program demonstrating the bug
- Compares MockDBHandler output with standard slog.JSONHandler
- Shows 5 test cases, 3 of which expose the bug

**To run:**
```bash
go run ./cmd/test_slog_groups/main.go
```

**Output includes:**
- Mock handler output (buggy behavior)
- Standard JSONHandler output (correct behavior)
- Detailed analysis with visual traces

### 2. **Quick Start Guide**
�� `cmd/test_slog_groups/REPRODUCTION_GUIDE.md`
- How to run the reproduction script
- What to expect in the output
- Understanding each test case
- Code locations of the bug

### 3. **Detailed Bug Analysis** 
📁 `docs/SLOG_GROUPS_BUG_ANALYSIS.md`
- Executive summary
- Root cause analysis
- Visual traces of the bug
- 4 proposed solutions (A-D)
- Impact assessment
- Migration notes

### 4. **Corrected Implementation**
📁 `docs/SLOG_GROUPS_CORRECTED_IMPLEMENTATION.md`
- Option A (recommended solution) in detail
- Complete corrected code
- Visual trace showing correct behavior
- Performance considerations
- Migration strategy

---

## The Bug in 30 Seconds

When you write:
```go
log.With("outside", 1).WithGroup("group").With("inside", 2).Info("msg")
```

**Standard slog behavior (JSONHandler):**
```json
{"outside": 1, "group": {"inside": 2}}
```

**Current DBHandler behavior (BUGGY):**
```json
{"group.outside": 1, "group.inside": 2}  // ← "outside" incorrectly prefixed!
```

The problem: Attributes added BEFORE entering a group are incorrectly prefixed with that group's name.

---

## Root Cause

In `WithGroup()`:
```go
func (h *DBHandler) WithGroup(name string) slog.Handler {
    // ...
    return &DBHandler{
        preAttrs: h.preAttrs,  // ← BUG: Copies all previous attributes
        preGroup: name,        // ← Now they'll be prefixed with this group!
    }
}
```

In `Handle()`:
```go
for _, a := range h.preAttrs {
    // ← BUG: Applies preGroup prefix to ALL preAttrs
    attrs[attrKey(h.preGroup, a.Key)] = a.Value.Any()
}
```

**The fix:** Track which scope each attribute belongs to, not just a flat list.

---

## Affected Code

📁 `internal/logger/db_handler.go`
- `NewDBHandler()` - Line 45
- `WithAttrs()` - Line 96
- `WithGroup()` - Line 111 ← **Main bug location**
- `Handle()` - Line 63 ← **Where bug manifests**
- `attrKey()` - Line 165

---

## Test Results

When you run the reproduction script:

```
[TEST 3] With("k3", "v3").WithGroup("g2").Info("msg", "k4", "v4")
MockDBHandler:    {g2.k3: v3, g2.k4: v4}     ← BUGGY
JSONHandler:      {k3: v3, g2: {k4: v4}}     ← CORRECT
                   ^^^       ^^^^

[TEST 4] With("outside", 1).WithGroup("group").With("inside", 2).Info("msg")
MockDBHandler:    {group.outside: 1, group.inside: 2}  ← BUGGY
JSONHandler:      {outside: 1, group: {inside: 2}}     ← CORRECT
                   ^^^^^^              ^^^^^^^

[TEST 5] With("root", 1).WithGroup("g1").With("mid", 2).WithGroup("g2").With("deep", 3).Info("msg")
MockDBHandler:    {g1.g2.root: 1, g1.g2.mid: 2, g1.g2.deep: 3}  ← BUGGY
JSONHandler:      {root: 1, g1: {mid: 2, g2: {deep: 3}}}        ← CORRECT
                   ^^^^     ^^^^
```

All three test cases (3, 4, 5) show attributes incorrectly prefixed with groups entered after they were created.

---

## Recommended Solution: Option A

**Track attributes by scope** instead of using a flat list:

```go
type DBHandler struct {
    // OLD (buggy):
    preAttrs []slog.Attr
    preGroup string
    
    // NEW (correct):
    scopedAttrs map[string][]slog.Attr  // "root" -> attrs, "g1" -> attrs, etc.
    currentScope string
}
```

### Key Changes:

1. **WithAttrs** - Append to current scope:
   ```go
   scopedAttrs[h.currentScope] = append(scopedAttrs[h.currentScope], attrs...)
   ```

2. **WithGroup** - Change scope:
   ```go
   newScope := h.currentScope + "." + name
   // Keep old scopedAttrs intact, just change currentScope
   ```

3. **Handle** - Flatten with correct prefixes:
   ```go
   for scope, attrs := range h.scopedAttrs {
       for _, a := range attrs {
           key := h.attrKey(scope, a.Key)  // Apply scope prefix correctly
           attrs[key] = a.Value.Any()
       }
   }
   ```

---

## Why This Matters

### Data Integrity
- Attributes are stored with wrong keys in the database
- Queries expecting `"outside"` will fail to find `"group.outside"`
- The semantic meaning of scope is lost

### Scope Sensitivity
- Affects any code doing: `log.With(...).WithGroup(...).With(...)`
- Affects nested groups: `log.WithGroup("g1").WithGroup("g2").With(...)`
- Silent failures - no errors, just wrong data

### Impact
- **Severity:** HIGH - Correctness bug with silent failures
- **Scope:** Any service using this pattern
- **Detectability:** Low - looks like it works, but produces wrong output

---

## Quick Reference

| Item | Location |
|------|----------|
| Bug demo | `cmd/test_slog_groups/main.go` |
| Quick start | `cmd/test_slog_groups/REPRODUCTION_GUIDE.md` |
| Bug analysis | `docs/SLOG_GROUPS_BUG_ANALYSIS.md` |
| Fix details | `docs/SLOG_GROUPS_CORRECTED_IMPLEMENTATION.md` |
| Source code | `internal/logger/db_handler.go` |

---

## Next Steps

1. **Review** the reproduction script output
2. **Understand** the bug mechanism (see detailed analysis)
3. **Decide** on a fix approach (Option A recommended)
4. **Implement** the corrected handler
5. **Test** with the provided test cases
6. **Plan** for data migration (if needed)

---

## Testing the Reproduction

```bash
cd /Users/johanvanrhyn/Developer/Github/go/disapyr-link

# Run the reproduction script
go run ./cmd/test_slog_groups/main.go | head -100

# You'll see:
# - MockDBHandler output (BUGGY)
# - JSONHandler output (CORRECT)
# - Comparison showing the differences
```

All the information you need to understand and fix this bug is in the files above.
