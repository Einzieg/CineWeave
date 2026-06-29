# Codex Execution Plan

The goal is to complete all tasks in `dev_docs/cineweave-technical-spec-codex-reviewed-v4.md`.

Current root decision:

- Use `D:\Code\CineWeave` as the repository root.
- Do not create a nested `cineweave/` directory.
- Use API envelope format from spec section 9.0.
- Move prompt schema work before provider call logging because provider logs reference prompt versions.

First task sequence:

1. Initialize monorepo structure.
2. Add local Docker Compose infrastructure.
3. Implement Go API skeleton with health checks and config loader.
4. Add PostgreSQL migrations for auth, tenants, projects, and RBAC.
5. Implement auth and RBAC.
6. Continue into Provider Gateway core.

