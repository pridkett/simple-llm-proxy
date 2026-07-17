// Simple LLM Proxy — Aspire AppHost (ADR 009)
//
// Entry point for the full development stack:
//   just up            (wraps: op run --env-file op.env -- dotnet run --project apphost)
//
// Brings up:
//   1. proxy    — the Go proxy via `go run`, listening on :8080 (fixed port from config.yaml)
//   2. frontend — the Vite dev server on :5173, started only after the proxy is healthy
//
// Ports are fixed and unproxied (isProxied: false) because config.yaml, the CORS
// allowlist, the OIDC redirect_url, and vite.config.js proxy targets all assume
// 8080/5173 — see ADR 009 Decision 2.
//
// Secrets: none are handled here. `just up` wraps this process in `op run`, and
// both child processes inherit the resolved environment (PROXY_MASTER_KEY,
// provider keys, OIDC credentials) — see ADR 009 Decision 3.

var builder = DistributedApplication.CreateBuilder(args);

var proxy = builder.AddExecutable("proxy", "go", "..", "run", "./cmd/proxy", "-config", "config.yaml")
    .WithHttpEndpoint(port: 8080, isProxied: false)
    .WithHttpHealthCheck("/health");

builder.AddViteApp("frontend", "../frontend")
    .WithHttpEndpoint(port: 5173, isProxied: false)
    // Vite proxies /admin, /v1, /health to :8080 — don't start until the backend is up.
    .WaitFor(proxy);

builder.Build().Run();
