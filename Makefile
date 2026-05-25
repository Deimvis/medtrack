.PHONY: run test build run-docker

run:
	go run ./cmd/server

test:
	go test ./...

build:
	go build -o medtrack ./cmd/server

run-docker:
	docker compose up --build medtrack
