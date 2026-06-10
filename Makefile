SIM_ID = 256EB576-7745-4D4C-A01E-97AA0B700BA6
IOS_PROJECT = ../realdeal-ios/RealDeal.xcodeproj

.PHONY: dev infra up down restart build docker-build test test-cover test-integration smoke sim start help

## Fast iteration: infra containers + core via go run
dev: infra
	go run ./cmd/core

## Start infra containers only (postgres + localstack)
infra:
	docker compose up -d --wait postgres localstack

## Full containerized stack — gateway on :8080, same images that ship to ECS
up:
	docker compose --profile full up -d --build
	@echo "Waiting for gateway health..."
	@for i in $$(seq 1 60); do \
		curl -sf http://localhost:8080/health >/dev/null 2>&1 && echo "Stack is up: http://localhost:8080" && exit 0; \
		sleep 1; \
	done; echo "ERROR: gateway did not become healthy"; docker compose --profile full logs --tail 20; exit 1

## Stop everything (all profiles)
down:
	docker compose --profile full down

## Restart the full stack
restart: down up

## Build all service binaries
build:
	go build -o bin/core ./cmd/core
	go build -o bin/lookup ./cmd/lookup

## Build all service images (one per cmd/<service>)
docker-build:
	docker build --build-arg SERVICE=core -t realdeal-core .
	docker build --build-arg SERVICE=lookup -t realdeal-lookup .

## Run all unit tests
test:
	go test -count=1 -parallel 8 ./...

## Run all unit tests with coverage report
test-cover:
	go test -count=1 -parallel 8 -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

## Bring up the full stack and run HTTP integration tests through the gateway
test-integration: up
	@go test -count=1 -tags=integration ./tests/integration/...; status=$$?; \
	docker compose --profile full down; \
	exit $$status

## Run integration tests against an already-running deployment (set API_BASE_URL)
smoke:
	go test -count=1 -tags=integration ./tests/integration/...

## Boot simulator, build and launch the iOS app
sim:
	xcrun simctl boot $(SIM_ID) 2>/dev/null || true
	open -a Simulator
	xcodebuild -project $(IOS_PROJECT) \
		-scheme RealDeal \
		-destination 'platform=iOS Simulator,id=$(SIM_ID)' \
		-configuration Debug \
		-derivedDataPath /tmp/realdeal-build \
		build
	xcrun simctl install $(SIM_ID) /tmp/realdeal-build/Build/Products/Debug-iphonesimulator/RealDeal.app
	xcrun simctl launch $(SIM_ID) com.kevil.RealDeal

## Start everything: full stack + iOS simulator
start: up sim

## Show available targets
help:
	@echo "Available targets:"
	@grep -E '^(##|[a-z-]+:)' Makefile | awk '/^## /{desc=substr($$0,4)} /^[a-z-]+:/{printf "  make %-18s %s\n", substr($$1,1,length($$1)-1), desc}'
