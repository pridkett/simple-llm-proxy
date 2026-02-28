.PHONY: build run test clean frontend-install frontend-dev frontend-build frontend-test

# Build the proxy binary
build:
	go build -o bin/proxy ./cmd/proxy

# Run the proxy
run: build
	./bin/proxy -config config.yaml

# Run with example config
run-example: build
	./bin/proxy -config config.yaml.example

# Run tests
test:
	go test -v ./...

# Run tests with coverage
test-coverage:
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Clean build artifacts
clean:
	rm -rf bin/
	rm -f coverage.out coverage.html
	rm -f proxy.db

# Format code
fmt:
	go fmt ./...

# Lint code (requires golangci-lint)
lint:
	golangci-lint run

# Tidy dependencies
tidy:
	go mod tidy

# Frontend targets
frontend-install:
	cd frontend && npm install

frontend-dev: frontend-install
	cd frontend && npm run dev

frontend-build: frontend-install
	cd frontend && npm run build

frontend-test: frontend-install
	cd frontend && npm test

# Build for multiple platforms
build-all:
	GOOS=linux GOARCH=amd64 go build -o bin/proxy-linux-amd64 ./cmd/proxy
	GOOS=linux GOARCH=arm64 go build -o bin/proxy-linux-arm64 ./cmd/proxy
	GOOS=darwin GOARCH=amd64 go build -o bin/proxy-darwin-amd64 ./cmd/proxy
	GOOS=darwin GOARCH=arm64 go build -o bin/proxy-darwin-arm64 ./cmd/proxy
	GOOS=windows GOARCH=amd64 go build -o bin/proxy-windows-amd64.exe ./cmd/proxy
