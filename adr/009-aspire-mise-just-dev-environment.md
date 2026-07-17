# ADR 009: Dev Environment Orchestration with Aspire, mise, and just

**Status:** Accepted
**Date:** 2026-07-16
**ADR Issue:** pridkett/simple-llm-proxy#70
**Implementation Issue:** pridkett/simple-llm-proxy#71

---

## Context

Local development currently requires manually coordinating two long-lived processes — the Go proxy (`:8080`) and the Vite dev server (`:5173`) — plus 1Password secret injection. The existing tooling is a `Makefile` with a `dev` target that shells out to `scripts/dev.sh`, which backgrounds both processes with interleaved, prefix-tagged output and no health checking, no restart, and no observability beyond raw stdout.

The coventyr project has validated a better pattern for polyglot local stacks:

- **[Aspire](https://aspire.dev) AppHost** (a small .NET project) declares the stack's resources — processes, containers, health checks, dependency ordering — and provides a live dashboard with per-resource logs, traces, and restart controls.
- **[mise](https://mise.jdx.dev)** pins toolchain versions in `mise.toml` so every machine (and future CI) resolves identical tool versions with one `mise install`.
- **[just](https://github.com/casey/just)** replaces make as the task runner — simpler syntax, no phony-target ceremony, better argument handling.

The developer already runs this stack for coventyr (dotnet 10 SDK, Aspire 13.4.3, mise, just all installed), so there is no new tool acquisition cost.

## Decision

### 1. Aspire AppHost orchestrates the dev stack

A new `apphost/` directory contains a minimal .NET project (`Aspire.AppHost.Sdk` 13.4.3, `net10.0`) declaring two resources:

- **`proxy`** — the Go binary, modeled with the built-in `AddExecutable("proxy", "go", ..., "run", "./cmd/proxy", "-config", "config.yaml")`. We deliberately do **not** take a dependency on `CommunityToolkit.Aspire.Hosting.Golang`: the community toolkit versions independently of Aspire core and pinning it against 13.4.3 adds compatibility risk for zero functional gain — `go run` as an executable resource gives us the same process lifecycle, health check, and log capture. An HTTP health check polls the proxy's existing `/health` endpoint.
- **`frontend`** — the Vite dev server via `AddNpmApp("frontend", "../frontend", "dev")` from `Aspire.Hosting.NodeJs` 13.4.3, with `WaitFor(proxy)` so the UI only starts once the backend is healthy (the Vite proxy config forwards `/admin`, `/v1`, `/health` to `:8080`).

### 2. Ports stay fixed; endpoints are unproxied

Aspire normally assigns random ports and fronts resources with its own reverse proxy. We opt out (`isProxied: false`, explicit ports 8080/5173) because four existing contracts assume fixed ports: `config.yaml` (`port: 8080`), the CORS allowlist (`localhost:5173`/`5174`), the OIDC `redirect_url` (`http://localhost:5173/auth/callback`), and `frontend/vite.config.js` proxy targets. Randomizing ports would break login and CORS for no benefit in a single-developer stack.

### 3. Secrets stay with `op run`, outside Aspire

Aspire is not the secret store. `just up` wraps the AppHost launch in `op run --env-file op.env --no-masking -- dotnet run --project apphost`, exactly as `scripts/dev.sh` did — child processes inherit the resolved environment (`PROXY_MASTER_KEY`, provider API keys, OIDC credentials). This keeps 1Password as the single source of secret truth and keeps `apphost/Program.cs` free of secret handling.

### 4. mise pins the toolchain

`mise.toml` pins: `go` (1.25, matching `go.mod`), `node` (22, current dev version), `dotnet` (10, required by Aspire.AppHost.Sdk 13.4.3), and `just`. `mise install` is the one-command bootstrap. golangci-lint is left unpinned (installed via brew today; can move into mise later if CI adopts it).

### 5. just replaces make; Makefile and dev.sh are removed

All Makefile targets port 1:1 to a `justfile` (`build`, `run`, `run-example`, `test`, `test-coverage`, `fmt`, `lint`, `tidy`, `clean`, `frontend-*`, `build-all`) plus a new **`up`** recipe that replaces `make dev`/`scripts/dev.sh` with the Aspire launch. The Makefile and `scripts/dev.sh` are deleted in the same change so there is exactly one way to run the stack. `CLAUDE.md` build/test documentation is updated accordingly.

## Consequences

**Positive**
- One-command stack (`just up`) with a dashboard: per-resource logs, health status, restarts, and OTEL trace collection if we later point the proxy's (planned, issue #30) OTEL exporter at the Aspire dashboard's OTLP endpoint.
- Deterministic toolchains across machines via `mise install`.
- Startup ordering: the frontend waits for a healthy backend instead of racing it.
- Alignment with coventyr — one mental model across the developer's projects.

**Negative / accepted trade-offs**
- A .NET project lives in a Go/Vue repo (~40 lines of C# + csproj); dotnet SDK becomes a dev-only dependency (already installed; production builds remain pure `go build` + `npm run build`).
- `apphost/bin` and `apphost/obj` need gitignoring.
- Contributors without mise can still run `go build ./...` and `npm run dev` directly — nothing at runtime depends on Aspire; it is orchestration only.

**Out of scope**
- CI does not adopt Aspire (tests run directly via `go test` / `vitest`).
- No containers are introduced; SQLite remains a local file. If a future phase adds Postgres or Redis, `AddContainer` in the AppHost is the natural extension point.
- Production deployment is unchanged.
