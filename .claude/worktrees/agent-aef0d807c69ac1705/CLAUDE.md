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
internal/i18n/         # Language selection and translations
charts/wish-operator/  # Helm chart
```

## Development Commands

```bash
# Generate deepcopy and CRD manifests (also syncs the chart's CRD copy)
make manifests generate

# Run tests (fetches the envtest control-plane binaries first)
make test

# Build
make build

# Lint
golangci-lint run

# Generate templ files (version comes from go.mod, matching the Containerfile
# and CI; a stale CLI on PATH restamps every generated file in the tree)
go run github.com/a-h/templ/cmd/templ@"$(go list -m -f '{{.Version}}' github.com/a-h/templ)" generate
```

## Breaking Changes

The release notes and the chart changelog are built from commit subjects, and `!` after the type or scope is the only marker they detect: `refactor(api)!: remove legacy fields`. A `BREAKING CHANGE:` footer in the body is invisible to them, since only `%s` is read.

Branches merge with squash, so the subject that reaches the release is the **PR title**. A breaking change whose commits carry the marker but whose PR title does not ships as an ordinary entry, with no pointer to the upgrade procedure. Put the marker in both.

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

Availability ignores reservations whose `expiresAt` has passed, so an expired reservation frees its items immediately rather than waiting for the controller to prune it from the status.

## Kubernetes Context

Use `homelab` context for all kubectl operations.
