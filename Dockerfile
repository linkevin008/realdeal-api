# One Dockerfile builds every service in this monorepo.
# Pass the service name as a build arg; it must match a cmd/<SERVICE> directory:
#   docker build --build-arg SERVICE=core -t realdeal-core .
#   docker build --build-arg SERVICE=lookup -t realdeal-lookup .
# The same image runs locally (docker compose) and on ECS Fargate.

FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG SERVICE=core
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /bin/service ./cmd/${SERVICE}

FROM gcr.io/distroless/static-debian12

COPY --from=builder /bin/service /service

EXPOSE 8080

ENTRYPOINT ["/service"]
