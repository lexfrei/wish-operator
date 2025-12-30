# wish-operator

Kubernetes operator for managing wishlists. Create wishes as Kubernetes resources, share them via web UI, and let others reserve gifts.

## Features

- **Wish CRD** — define wishes with title, description, price, images, priority (1-5 stars), and tags
- **Web UI** — HTMX-powered interface for viewing and reserving wishes
- **Reservations** — reserve wishes for 1-8 weeks with automatic expiration
- **TTL** — wishes can auto-expire after a defined duration
- **Rate limiting** — per-IP rate limiting to prevent abuse
- **Gateway API** — HTTPRoute support for ingress via Gateway API

## Installation

### Helm (recommended)

```bash
helm install wish-operator oci://ghcr.io/lexfrei/charts/wish-operator \
  --namespace wish-operator \
  --create-namespace
```

### With HTTPRoute

```bash
helm install wish-operator oci://ghcr.io/lexfrei/charts/wish-operator \
  --namespace wish-operator \
  --create-namespace \
  --set httpRoute.enabled=true \
  --set 'httpRoute.parentRefs[0].name=my-gateway' \
  --set 'httpRoute.hostnames[0]=wishes.example.com'
```

## Usage

### Create a Wish

```yaml
apiVersion: wishlist.k8s.lex.la/v1alpha1
kind: Wish
metadata:
  name: mechanical-keyboard
  namespace: wish-operator
spec:
  title: "Mechanical Keyboard"
  description: "Cherry MX Brown switches"
  msrp: "$150"
  productURL: "https://example.com/keyboard"
  imageURL: "https://example.com/keyboard.jpg"
  priority: 4
  tags:
    - electronics
    - office
  contextTags:
    - birthday
  ttl: 720h  # 30 days
```

### Wish Spec Fields

| Field | Type | Description |
|-------|------|-------------|
| `title` | string | Name of the desired item (required) |
| `description` | string | Why you want this item |
| `msrp` | string | Price display (e.g., "$150", "€99") |
| `productURL` | string | Link to purchase |
| `imageURL` | string | Product image URL |
| `priority` | int32 | Importance 1-5 (displayed as stars) |
| `tags` | []string | Category labels |
| `contextTags` | []string | Occasions (birthday, christmas) |
| `ttl` | duration | Auto-expire after this duration |

### Wish Status

| Field | Description |
|-------|-------------|
| `active` | Whether wish is within TTL |
| `reserved` | Whether someone reserved it |
| `reservedAt` | When it was reserved |
| `reservationExpires` | When reservation expires |

## Configuration

### Helm Values

| Parameter | Default | Description |
|-----------|---------|-------------|
| `replicaCount` | 1 | Number of replicas |
| `image.repository` | ghcr.io/lexfrei/wish-operator | Image repository |
| `image.tag` | "" | Image tag (defaults to chart appVersion) |
| `operator.namespace` | default | Namespace to watch for Wishes |
| `operator.rateLimit` | 30 | Requests per second per IP |
| `operator.rateBurst` | 10 | Burst size for rate limiting |
| `httpRoute.enabled` | false | Create HTTPRoute resource |
| `httpRoute.hostnames` | [] | Hostnames for the route |
| `httpRoute.parentRefs` | [] | Gateway references |

## Development

### Prerequisites

- Go 1.23+
- kubectl
- Helm 3
- [helm-unittest](https://github.com/helm-unittest/helm-unittest)

### Build

```bash
make build
```

### Test

```bash
# Go tests
go test ./...

# Helm tests
helm unittest charts/wish-operator
```

### Lint

```bash
golangci-lint run
```

### Run locally

```bash
make install    # Install CRDs
make run        # Run controller locally
```

## License

BSD-3-Clause. See [LICENSE](LICENSE).
