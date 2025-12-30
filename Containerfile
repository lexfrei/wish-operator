# Build stage
FROM docker.io/library/golang:1.25-alpine AS builder

ARG TARGETOS=linux
ARG TARGETARCH

WORKDIR /workspace

# Install templ
RUN go install github.com/a-h/templ/cmd/templ@latest

# Copy go mod files first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Generate templ files
RUN templ generate

# Build
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w" -o manager ./cmd/

# Runtime stage
FROM gcr.io/distroless/static:nonroot

LABEL org.opencontainers.image.source="https://github.com/lexfrei/wish-operator"
LABEL org.opencontainers.image.description="Kubernetes operator for managing wishlists"
LABEL org.opencontainers.image.licenses="BSD-3-Clause"

WORKDIR /
COPY --from=builder /workspace/manager .

USER 65532:65532

ENTRYPOINT ["/manager"]
