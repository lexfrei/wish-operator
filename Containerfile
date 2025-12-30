FROM docker.io/library/golang:1.25-alpine AS builder

ARG VERSION=development
ARG REVISION=development

# hadolint ignore=DL3018
RUN echo 'nobody:x:65534:65534:Nobody:/:' > /tmp/passwd && \
    apk add --no-cache upx ca-certificates

WORKDIR /build

# Install templ
RUN go install github.com/a-h/templ/cmd/templ@latest

# Copy go mod files first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source and generate templ
COPY . .
RUN templ generate

# Build and compress
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X main.Version=${VERSION} -X main.Gitsha=${REVISION}" -trimpath -o manager ./cmd/ && \
    upx --best --lzma manager

FROM scratch

LABEL org.opencontainers.image.source="https://github.com/lexfrei/wish-operator"
LABEL org.opencontainers.image.description="Kubernetes operator for managing wishlists"
LABEL org.opencontainers.image.licenses="BSD-3-Clause"

COPY --from=builder /tmp/passwd /etc/passwd
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder --chmod=555 /build/manager /manager

USER 65534
EXPOSE 8080/tcp 8081/tcp
ENTRYPOINT ["/manager"]
