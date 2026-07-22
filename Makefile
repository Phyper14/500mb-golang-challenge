.PHONY: build test test-race test-cover lint vet fmt tidy docker-build compose-up compose-down smoke test-load clean

MODULE := github.com/Phyper14/500mb-golang-challenge
BINARY := bin/api
IMAGE  := 500mb-club-go:local

## build: compile the API binary for the host platform.
build:
	go build -trimpath -ldflags="-s -w" -o $(BINARY) ./cmd/api

## test: run the unit + integration test suite (miniredis, no Docker needed).
test:
	go test ./...

## test-race: same as test, with the race detector (requires cgo/gcc).
test-race:
	CGO_ENABLED=1 go test -race ./...

## test-cover: run tests with coverage and print the per-func breakdown.
test-cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

## lint: static analysis (go vet + staticcheck).
lint: vet
	staticcheck ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

tidy:
	go mod tidy

## docker-build: build the arm64/amd64-ready image locally for the host arch.
docker-build:
	docker build -t $(IMAGE) .

## compose-up: start the full stack (3 API + nginx LB + redis) using the
## locally built image, respecting the 2 CPU / 500 MB aggregate budget.
compose-up: docker-build
	sed 's|ghcr.io/pablo-martins/500mb-club-go:latest|$(IMAGE)|' docker-compose.yml > docker-compose.local.yml
	docker compose -f docker-compose.local.yml -p 500mb-local up -d

compose-down:
	-docker compose -f docker-compose.local.yml -p 500mb-local down -v
	rm -f docker-compose.local.yml

## smoke: run the official k6 smoke test against the local stack (compose-up first).
smoke:
	docker run --rm --network host \
		-v "$$(pwd)/test:/test" \
		-e BASE_URL=http://localhost:8080 \
		-i grafana/k6 run /test/smoke.js

## test-load: run the official k6 steady-state test.js against the local stack.
test-load:
	docker run --rm --network host \
		-v "$$(pwd)/test:/test" \
		-e BASE_URL=http://localhost:8080 \
		-i grafana/k6 run /test/test.js

clean:
	rm -rf bin coverage.out docker-compose.local.yml
