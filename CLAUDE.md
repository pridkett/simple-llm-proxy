# IMPORTANT STUFF FIRST

You're working as an engineer with a codebase in GitHub. This means that when
you begin work on a task, it MUST HAVE AN ISSUE IN GITHUB. You must work on a
branch. Then you must file a PR with a conventional commit message that
connects it to the issue in GitHub. Keep your issues compact and don't try to
do too much on one commit.

# PROJECT STUFF

## Project Overview

Simple LLM Proxy is a lightweight Go-based LLM proxy server that provides OpenAI-compatible endpoints with multi-provider support (OpenAI, Anthropic). It uses LiteLLM-compatible YAML configuration.

## Build & Test Commands

```bash
# Build
make build                 # Build binary to bin/proxy
go build ./...             # Verify compilation

# Test
make test                  # Run all tests
go test ./... -v           # Verbose test output

# Run
make run                   # Build and run with config.yaml
./bin/proxy -config config.yaml
```

## Project Structure

```
cmd/proxy/main.go              # Entry point, server setup, graceful shutdown
internal/
  api/
    handler/                   # HTTP handlers (chat.go, models.go, health.go, embeddings.go)
    middleware/                # auth.go, logging.go, recovery.go
    router.go                  # Chi router setup, route registration
  config/
    config.go                  # Config structs (ModelConfig, RouterSettings, GeneralSettings)
    loader.go                  # YAML parsing, os.environ/ expansion
  model/
    request.go                 # OpenAI-compatible request types
    response.go                # OpenAI-compatible response types
    error.go                   # Error types and WriteError helper
  provider/
    provider.go                # Provider interface, Stream interface, Deployment struct
    registry.go                # Provider factory registry
    openai/openai.go           # OpenAI implementation
    anthropic/anthropic.go     # Anthropic implementation with message translation
  router/
    router.go                  # Load balancing router, deployment management
    strategy.go                # Strategy interface
    shuffle.go                 # Random selection strategy
    roundrobin.go              # Round-robin strategy
    cooldown.go                # Failure tracking and cooldown management
  storage/
    storage.go                 # Storage interface
    sqlite/                    # SQLite implementation with migrations
```

## Key Patterns

### Provider Interface
All LLM providers implement `provider.Provider`:
```go
type Provider interface {
    Name() string
    ChatCompletion(ctx, req) (*ChatCompletionResponse, error)
    ChatCompletionStream(ctx, req) (Stream, error)
    Embeddings(ctx, req) (*EmbeddingsResponse, error)
    SupportsEmbeddings() bool
}
```

### Provider Registration
Providers self-register via `init()`:
```go
func init() {
    provider.Register("openai", New)
}
```

### Config Environment Expansion
Config values like `os.environ/VAR_NAME` are expanded to environment variable values in `config/loader.go`.

### Request Flow
1. Request hits handler in `api/handler/`
2. Handler calls `router.GetDeploymentWithRetry()` to get a healthy deployment
3. Handler calls `deployment.Provider.ChatCompletion()` or `ChatCompletionStream()`
4. Router tracks success/failure via `ReportSuccess()`/`ReportFailure()`
5. Cooldown manager takes deployments offline after repeated failures

### Anthropic Translation
`provider/anthropic/anthropic.go` translates:
- OpenAI messages → Anthropic format (extracts system messages)
- Anthropic responses → OpenAI format
- Tool calls between formats
- Stop reasons (`end_turn` → `stop`, `max_tokens` → `length`)

### Streaming
Both providers return `provider.Stream` interface for SSE streaming. Handlers write `data: {json}\n\n` format and flush after each chunk.

## Configuration Format

```yaml
model_list:
  - model_name: gpt-4              # User-facing name
    litellm_params:
      model: openai/gpt-4          # provider/actual-model
      api_key: os.environ/OPENAI_API_KEY
    rpm: 100

router_settings:
  routing_strategy: simple-shuffle  # or round-robin
  num_retries: 2
  allowed_fails: 3
  cooldown_time: 30s

general_settings:
  master_key: os.environ/PROXY_MASTER_KEY
  database_url: ./proxy.db
  port: 8080
```

## Dependencies

- `github.com/go-chi/chi/v5` - HTTP router
- `gopkg.in/yaml.v3` - YAML parsing
- `modernc.org/sqlite` - Pure Go SQLite (no CGO)

## Adding a New Provider

1. Create `internal/provider/newprovider/newprovider.go`
2. Implement `provider.Provider` interface
3. Add `init()` function to register: `provider.Register("newprovider", New)`
4. Import in `cmd/proxy/main.go`: `_ "github.com/pwagstro/simple-llm-proxy/internal/provider/newprovider"`

## Architecture Designs

Before embarking on large tasks, create an issue in GitHub for the ADR for that
task and write a complete ADR (architectural decision record) and commit it to
the `adr` folder. That ADR, which should be written in Markdown, is considered
the deliverable for that issue. The implementation of the issue should have its
own issue. Before embarking on a major task, you MUST draft an ADR and commit it.

## Git and GitHub Usage

All issues and bugs MUST be in pridkett/simple-llm-proxy. Do not file or search for
bugs on any other project. Even for code that resides in another repository,
issuses must be placed in pridkett/simple-llm-proxy for all submodules of this project.

### Code Commits

After completing a task, please commit your code using a single commit with a
commit message in the form of a conventional commit. Make sure to be as
descriptive as possible and write a multi-line commit message explaining the
changes.

#### Conventional Commit Component Names

The component (scope) in a conventional commit MUST be a meaningful English
word or short phrase describing what area of the codebase changed. NEVER use
phase numbers, plan numbers, or sequence identifiers as the component.

Good examples:
- `feat(storage):` — changes to the storage/database layer
- `feat(spend-handler):` — changes to the spend API handler
- `feat(cost-view):` — changes to the CostView frontend component
- `fix(auth):` — fixes to authentication logic
- `test(storage):` — tests for the storage layer

Bad examples (NEVER DO THIS):
- `feat(03-02):` — meaningless phase number
- `docs(phase-03):` — meaningless phase reference
- `test(03-00):` — meaningless plan number

#### Co-Authorship — NON-NEGOTIABLE

Every commit MUST end with the following co-authorship trailer so GitHub
attributes the commit correctly:

```
Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
```

Use the actual model name from the session (Sonnet 4.6, Opus 4.6, Haiku 4.5,
etc.). This line must be separated from the commit body by a blank line.

If you are working on an issue from the issue tracker, you must include a line
that indicates what issue you are working on. It should look like this:

contributes to pridkett/simple-llm-proxy#123

All subrepositories for this project will use the same issue tracker, so even
if you're in a sub-project that commits to a different repository, reference
the issues this way.

### Working on Branches

When starting on a new task - always make sure you being your work off the
`main` branch of the code. If you do use a branch for code, make a clear note
that you're working off a branch. Branches should be clearly named in the form of
    [TYPE]/[NUM]-dashed-word-description
For example:
    feature/12-add-in-server-messaging
    fix/13-correct-missing-headers-in-cicd
If you are working off a branch and it seems to be directed for a different branch,
you are probably doing something wrong. If your current git state appears to be
in a detached state, you are almost certainly doing something wrong.

Unless you are explicitly told to do so, NEVER start a new task off any branch
other than `main`. Doing so is an amateur move and will get most developers
fired. ALWAYS make sure that `main` is updated against the main GitHub repo before
creating a new branch.

The branch number should refer to the GitHub issue you're working on and not
the GSD phase or GSD identifier. If you don't have a corresponding GitHub issue
you're working on, make one. It's important that we keep our documentation
buttoned up.

When completing a task you MUST file a pull request as described in the pull
request section below. Please make sure that the pull requests build off each
other if you're doing multiple pull requests so that way there are fewer merge
commits and conflicts.

### Working with Issues

When creating an issue, always make sure to apply the appropriate tags to the
issue. This helps the humans understand what you're doing.

When starting work on an issue, always make sure to assign yourself to the
issue. You must have a GitHub issue for the work that you're doing in addition to
the GSD phase.

When closing an issue, always make sure to add a detailed writeup of the work
that was done to implement that issue in code.

When you create an issue on GitHub - you must add the label `AI Generated` to the 
issue.

### Working with Pull Requests

All code changes MUST be done on a branch, never directly on `main`. Each
wave of a GSD phase execution must have its own branch and pull request. Do
not merge wave N's branch until its PR is created (merging is the human's
responsibility).

When you are incrementally working on a task and pushing to a branch, create a
pull request for that task. Link it to the original work item using notation
such as "this PR implements pridkett/simple-llm-proxy#2". Provide detailed
comments about the implementation, including a link to the ADR.

When you are working on a task and it has a PR, make sure that all of the items
in the PR are checked off as being complete before marking it for review.

When you create a pull request on GitHub, you must add the label `AI Generated`
to the pull request. You also should apply labels in use within the project as
appropriate. For example, `frontend`, `backend`, `adr`, `enhancement`, etc.

