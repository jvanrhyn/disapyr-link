# Copilot Instructions — disapyr-link

## Branching Strategy

This project uses a three-tier branching model. Agents **must** follow these rules without deviation.

### Tiers

| Branch | Purpose |
|--------|---------|
| `feature/*` | All new work — one branch per feature or fix |
| `develop` | Integration branch — receives all feature merges |
| `main` | Release branch — clean, single-commit-per-release history |

### Rules

1. **Always branch from `develop`** when starting any new feature or fix:
   ```
   git checkout develop
   git checkout -b feature/{short-description}
   ```

2. **Merge feature → develop without squash** (preserves full commit history on develop):
   ```
   git checkout develop
   git merge feature/{short-description}
   ```

3. **Never merge directly to `main`** during normal development. `main` is only updated via an explicit squash merge from `develop`.

4. **Squash develop → main only when explicitly instructed** by the user. Never do this automatically. The trigger phrase is explicit (e.g. "merge to main", "release", "squash to main").

5. **When squashing to `main`**, use `--allow-unrelated-histories` (main is an orphan branch with no shared history) and write a commit message that describes the full feature/release purpose — not individual commit messages:
   ```
   git checkout main
   git merge --squash --allow-unrelated-histories develop
   git commit -m "feat: <meaningful description of what this release contains>"
   ```

6. **Delete feature branches after merging** to keep the branch list clean:
   ```
   git branch -d feature/{short-description}
   ```

### What agents must NOT do

- ❌ Squash when merging feature → develop
- ❌ Merge to `main` unless explicitly asked
- ❌ Commit directly to `develop` or `main` (always use a feature branch)
- ❌ Use a generic commit message like "squash merge" when releasing to `main`

### Build & test

- Language: Go 1.25+
- Build: `go build ./...`
- Test: `go test ./...`
- Run: `go run ./cmd/server`
