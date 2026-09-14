# Build stage
FROM golang:1.27.1-alpine3.23 AS build

WORKDIR /src

# Manifests are copied before the source so the module download lands in its own
# layer: editing a .go file then rebuilds without re-fetching dependencies.
COPY go.mod go.sum* ./
RUN go mod download

COPY . .

ARG VERSION=dev

# CGO_ENABLED=0 for fully static binary
# -trimpath keeps build paths out of the binary, and -s -w
# drop the symbol table and DWARF data.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/api ./cmd/api

# Runtime stage
FROM scratch

# Root certificates, needed for any outbound TLS.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/api /api

USER 65532:65532

EXPOSE 8080

ENV APP_ENV=production \
    APP_PORT=8080

# No HEALTHCHECK: scratch has no shell or curl to run one. Point the
# orchestrator's probes at the endpoints instead — GET /healthz for liveness and
# GET /readyz for readiness.
ENTRYPOINT ["/api"]