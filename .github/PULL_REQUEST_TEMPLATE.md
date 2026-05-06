## Summary

<!-- Describe what this PR does and why -->

## Type of Change

- [ ] 🐛 Bug fix
- [ ] ✅ New/migrated test
- [ ] ♻️ Refactor (no functional change)
- [ ] 📦 Dependency update
- [ ] 🔧 CI / tooling
- [ ] 📖 Documentation

## Test Area(s) Affected

<!-- e.g. operator, triggers, ecosystem -->

## Polarion ID(s)

<!-- Comma-separated Polarion case IDs, or N/A -->

## How to Test

<!-- How should a reviewer validate this change? -->

```bash
# Example:
ginkgo run --label-filter='...' ./tests/<area>/
```

## Checklist

- [ ] CI passes (lint, build, compile-tests)
- [ ] Area label added to this PR
- [ ] PR title follows Conventional Commits (`feat:`, `fix:`, `chore:`, etc.)
- [ ] `go mod tidy` run if dependencies changed
- [ ] New tests have `Label()` set and compile correctly
- [ ] `BeforeEach`/`AfterEach` clean up any cluster resources created
