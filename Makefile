SIM_ID = F341C58B-1EEE-471D-97A7-630050D3FB6F
IOS_PROJECT = ../realdeal-ios/RealDeal.xcodeproj

.PHONY: dev up down restart build test sim start

# Start everything: Postgres, API server, and iOS simulator
start: up sim
	go run ./cmd/api

# Start Postgres and run the API server
dev: up
	go run ./cmd/api

# Start Postgres in the background (wait for healthy)
up:
	docker compose up -d --wait

# Stop Postgres
down:
	docker compose down

# Restart Postgres and the API server
restart: down dev

# Build the binary
build:
	go build -o bin/api ./cmd/api

# Run all tests
test:
	go test ./...

# Boot simulator, build and launch the iOS app
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
