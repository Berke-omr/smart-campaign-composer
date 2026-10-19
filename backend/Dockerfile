# syntax=docker/dockerfile:1

# One image, five binaries (api, worker, mockprovider, migrate, seed).
# Stages: deps -> test (used by `make test`) / build -> runtime.

FROM golang:1.27.2 AS deps
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# Debian-based Go image: the race detector needs cgo, which alpine lacks.
FROM deps AS test
COPY . .
ENV GOFLAGS=-mod=readonly
CMD ["go", "test", "-race", "-count=1", "./..."]

FROM deps AS build
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/...

FROM alpine:3.24 AS runtime
# ca-certificates: HTTPS to the Claude API (P3). tzdata: correct local times in logs.
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -H -u 10001 app
COPY --from=build /out/ /app/
USER app
