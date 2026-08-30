# wish-operator

Kubernetes operator for managing wishlists.

## Project Overview

Users describe desired gifts as Kubernetes CRD resources (`Wish`). The operator serves a web page using HTMX + Templ where anonymous visitors can reserve gifts for 1-8 weeks.

## Technical Stack

- **Language**: Go 1.26
- **CRD API**: `wishlist.k8s.lex.la/v1alpha1`
- **Controller**: controller-runtime
- **Web**: HTMX + a-h/templ
- **CLI**: stdlib `flag`
- **Testing**: testify + envtest

## Project Structure

```
api/v1alpha1/          # CRD types (Wish)
cmd/                   # Entry point (main.go)
internal/controller/   # Reconciler logic
internal/web/          # HTTP server + handlers
internal/templates/    # Templ files
charts/wish-operator/  # Helm chart
```

## Development Commands

```bash
# Generate deepcopy and CRD manifests
make manifests generate

# Run tests
go test ./...

# Build
make build

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
- `officialURL` (string): Manufacturer or product page
- `purchaseURLs` ([]string): Places to buy it
- `msrp` (string): Price display
- `tags` ([]string): Category tags
- `contextTags` ([]string): "For:" section
- `description` (string): Why user wants this
- `priority` (int32, 0-5): Displayed as stars; 0 means unset and renders none
- `quantity` (int32, default 1): How many are wanted; 0 means unlimited
- `ttl` (*metav1.Duration): How long wish stays active

**Status fields**:
- `reservations` ([]Reservation): Active reservations, each with `quantity`, `createdAt`, `expiresAt`
- `active` (bool): Within TTL
- `conditions` ([]metav1.Condition): Declared on the type; the controller does not populate it yet
- Deprecated, kept for migration off the single-reservation model: `reserved` (bool), `reservedAt` (*metav1.Time), `reservationExpires` (*metav1.Time)

## Kubernetes Context

Use `homelab` context for all kubectl operations.
