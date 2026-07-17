set shell := ["bash", "-uc"]

# Build the proxy binary
build:
    go build -o bin/proxy ./cmd/proxy

# Run the proxy (secrets injected from 1Password)
run: build
    op run --env-file op.env --no-masking -- ./bin/proxy -config config.yaml

# Run with example config
run-example: build
    ./bin/proxy -config config.yaml.example

# Start the full dev stack (Go proxy + Vite frontend) via the Aspire AppHost.
# op run resolves secrets once; Aspire child processes inherit the environment.
# Dashboard URL is printed on startup.
up:
    op run --env-file op.env --no-masking -- dotnet run --project apphost

# Run backend tests
test:
    go test -v ./...

# Run backend tests with coverage report
test-coverage:
    go test -v -coverprofile=coverage.out ./...
    go tool cover -html=coverage.out -o coverage.html

# Format Go code
fmt:
    go fmt ./...

# Lint Go code (requires golangci-lint)
lint:
    golangci-lint run

# Tidy Go module dependencies
tidy:
    go mod tidy

# Clean build artifacts (does NOT touch proxy.db — it holds real usage data)
clean:
    rm -rf bin/
    rm -f coverage.out coverage.html

# Install frontend dependencies
frontend-install:
    cd frontend && npm install

# Start only the Vite dev server (prefer `just up` for the full stack)
frontend-dev: frontend-install
    cd frontend && npm run dev

# Production build of the frontend
frontend-build: frontend-install
    cd frontend && npm run build

# Run frontend unit tests
frontend-test: frontend-install
    cd frontend && npm test

# Run all tests (backend + frontend)
test-all: test frontend-test

# Cross-compile release binaries
build-all:
    GOOS=linux GOARCH=amd64 go build -o bin/proxy-linux-amd64 ./cmd/proxy
    GOOS=linux GOARCH=arm64 go build -o bin/proxy-linux-arm64 ./cmd/proxy
    GOOS=darwin GOARCH=amd64 go build -o bin/proxy-darwin-amd64 ./cmd/proxy
    GOOS=darwin GOARCH=arm64 go build -o bin/proxy-darwin-arm64 ./cmd/proxy
    GOOS=windows GOARCH=amd64 go build -o bin/proxy-windows-amd64.exe ./cmd/proxy
