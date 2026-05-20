.PHONY: web build test test-go test-web lint clean

# Build the front-end SPA into internal/webui/dist so go:embed picks it up.
web:
	cd internal/webui && npm ci && npm run build

# Full server build depends on a fresh web bundle.
build: web
	go build -o bin/server ./cmd/server

test: test-go test-web

test-go:
	go test ./...

test-web:
	cd internal/webui && npm test -- --run

lint:
	go vet ./...
	cd internal/webui && npm run lint

clean:
	rm -rf bin/ internal/webui/dist/assets internal/webui/dist/index.html
