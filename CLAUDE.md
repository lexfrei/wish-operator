# wish-operator

Kubernetes operator for managing wishlists.

## Project Overview

Users describe desired gifts as Kubernetes CRD resources (`Wish`). The operator serves a web page using HTMX + Templ where anonymous visitors can reserve gifts for 1-8 weeks.

## Technical Stack

- **Language**: Go 1.25
- **CRD API**: `wishlist.k8s.lex.la/v1alpha1`
- **Controller**: controller-runtime
- **Web**: HTMX + a-h/templ
- **CLI**: cobra + viper
- **Testing**: testify + envtest

## Project Structure

```
api/v1alpha1/          # CRD types (Wish)
cmd/operator/          # Entry point
internal/controller/   # Reconciler logic
internal/web/          # HTTP server + handlers
internal/templates/    # Templ files
charts/wish-operator/  # Helm chart
```

## Development Commands

```bash
# Generate deepcopy and CRD manifests
go generate ./...

# Run tests
go test ./...

# Build
go build -o bin/operator ./cmd/operator

# Lint
golangci-lint run

# Generate templ files
templ generate
```

## TDD Workflow

This project follows strict TDD:
1. Write failing tests first
2. Implement minimum code to pass tests
3. Refactor while keeping tests green

## CRD: Wish

**Spec fields**:
- `title` (string, required): Item name
- `imageURL` (string): Product image
- `productURL` (string): Link to buy
- `msrp` (string): Price display
- `tags` ([]string): Category tags
- `contextTags` ([]string): "For:" section
- `description` (string): Why user wants this
- `priority` (int32, 1-5): Displayed as stars
- `ttl` (metav1.Duration): How long wish stays active

**Status fields**:
- `reserved` (bool): Is reserved
- `reservedAt` (*metav1.Time): When reserved
- `reservationExpires` (*metav1.Time): When reservation expires
- `active` (bool): Within TTL

## Kubernetes Context

Use `homelab` context for all kubectl operations.
