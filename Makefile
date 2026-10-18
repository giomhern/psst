.PHONY: help fmt check test race vet build lint security db-up db-down contacts contacts-helper contacts-helper-test

help:
	@echo "Targets: fmt check test race vet build lint security db-up db-down contacts"

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

# Optional macOS bridge. Ordinary Go builds keep working without Swift.
contacts-helper:
	mkdir -p bin
	xcrun swiftc -parse-as-library -O -target "$$(uname -m)-apple-macosx13.0" -framework Contacts -Xlinker -sectcreate -Xlinker __TEXT -Xlinker __info_plist -Xlinker cmd/psst-contacts/Info.plist cmd/psst-contacts/main.swift cmd/psst-contacts/ContactNames.swift -o bin/psst-contacts
	codesign --force --sign - --identifier io.github.giomhern.psst.contacts bin/psst-contacts

contacts: contacts-helper
	install -d -m 700 "$(HOME)/Library/Caches/psst"
	install -m 700 bin/psst-contacts "$(HOME)/Library/Caches/psst/psst-contacts"

contacts-helper-test:
	mkdir -p bin
	xcrun swiftc -parse-as-library -O cmd/psst-contacts/ContactNames.swift tests/contacts-helper/main.swift -o bin/psst-contacts-test
	./bin/psst-contacts-test
