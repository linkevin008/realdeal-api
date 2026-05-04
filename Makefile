SIM_ID = 256EB576-7745-4D4C-A01E-97AA0B700BA6
IOS_PROJECT = ../realdeal-ios/RealDeal.xcodeproj

.PHONY: dev up down restart build test sim start help

## Start everything: Postgres, API server, and iOS simulator
start: up sim
	go run ./cmd/api

## Start Postgres and run the API server
dev: up
	go run ./cmd/api

## Start Postgres in the background (wait for healthy)
up:
	docker compose up -d --wait

## Stop Postgres
down:
	docker compose down

## Restart Postgres and the API server
restart: down dev

## Build the binary
build:
	go build -o bin/api ./cmd/api

## Run all tests
test:
	go test -count=1 -parallel 8 ./...

## Run all tests with coverage report
test-cover:
	go test -count=1 -parallel 8 -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

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

## Show available targets
help:
	@echo "Available targets:"
	@grep -E '^(##|[a-z]+:)' Makefile | awk '/^## /{desc=substr($$0,4)} /^[a-z]+:/{printf "  make %-12s %s\n", substr($$1,1,length($$1)-1), desc}'
