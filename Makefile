.PHONY: help fmt check test race vet build lint security db-up db-down

help:
	@echo "Targets: fmt check test race vet build lint security db-up db-down"

fmt:
	gofmt -w cmd internal

check:
	@test -z "$$(gofmt -l cmd internal)" || (echo "Run make fmt"; exit 1)
	go vet ./...
	go test ./...
	go build ./...

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

build:
	go build -o bin/psst ./cmd/psst
	go build -o bin/psst-server ./cmd/psst-server

lint:
	go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...

security:
	go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...

db-up:
	docker compose up -d --wait postgres

db-down:
	docker compose down
