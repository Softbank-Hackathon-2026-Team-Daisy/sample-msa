SERVICES   := backend frontend
PKG        := github.com/Softbank-Hackathon-2026-Team-Daisy/sample-msa/internal/buildinfo
VERSION    ?= $(shell sed -n 's/^[[:space:]]*Version[[:space:]]*= "\(.*\)"/\1/p' internal/buildinfo/buildinfo.go)
COMMIT     ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BASE_URL   ?= http://localhost:8080

LDFLAGS := -s -w -X $(PKG).Version=$(VERSION) -X $(PKG).Commit=$(COMMIT) -X $(PKG).BuildTime=$(BUILD_TIME)

.PHONY: run run-backend run-frontend build test fmt fmt-check vet check docker-build up down smoke clean

## run: run both services locally (backend :8081, frontend :8080)
run:
	$(MAKE) -j2 run-backend run-frontend

run-backend:
	PORT=8081 go run ./services/backend

run-frontend:
	BACKEND_URL=http://localhost:8081 go run ./services/frontend

## build: build static binaries into bin/
build:
	@for s in $(SERVICES); do \
		echo "building bin/$$s"; \
		CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$$s ./services/$$s || exit 1; \
	done

## test: run all tests
test:
	go test -count=1 ./...

## fmt: format all Go code
fmt:
	gofmt -w .

## fmt-check: fail if any Go file is not gofmt-clean
fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

## vet: run go vet
vet:
	go vet ./...

## check: local quality gate (format, vet, tests, build)
check: fmt-check vet test build

## docker-build: build both images tagged hellocalc-<service>:$(COMMIT) (commit hash; no latest)
docker-build:
	@for s in $(SERVICES); do \
		docker build -f services/$$s/Dockerfile \
			--build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) --build-arg BUILD_TIME=$(BUILD_TIME) \
			-t hellocalc-$$s:$(COMMIT) . || exit 1; \
	done

## up: build and start both services with Docker Compose (http://localhost:8080)
up:
	VERSION=$(VERSION) COMMIT=$(COMMIT) BUILD_TIME=$(BUILD_TIME) docker compose up --build -d --wait

## down: stop the Compose stack
down:
	docker compose down

## smoke: run the smoke test against $(BASE_URL)
smoke:
	BASE_URL=$(BASE_URL) ./scripts/smoke-test.sh

## clean: remove build output
clean:
	rm -rf bin
