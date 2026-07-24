# wish-operator

Kubernetes operator for managing wishlists. Create wishes as Kubernetes resources, share them via web UI, and let others reserve gifts.

## Features

- **Wish CRD** — define wishes with title, description, price, images, priority (0-5 stars), and tags
- **Quantity support** — specify multiple items per wish, reserve partially
- **Web UI** — HTMX-powered interface for viewing and reserving wishes
- **Reservations** — multiple anonymous reservations per wish, 1-8 weeks with automatic expiration
- **TTL** — wishes can auto-expire after a defined duration
- **Rate limiting** — per-IP rate limiting to prevent abuse, see [Client addresses behind a proxy](#client-addresses-behind-a-proxy)
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
  officialURL: "https://example.com/keyboard"
  purchaseURLs:
    - "https://amazon.com/keyboard"
    - "https://newegg.com/keyboard"
  imageURL: "https://example.com/keyboard.jpg"
  priority: 4
  tags:
    - electronics
    - office
  contextTags:
    - birthday
  ttl: 720h    # 30 days
  quantity: 1  # default, can be omitted
```

### Wish Spec Fields

| Field | Type | Description |
|-------|------|-------------|
| `title` | string | Name of the desired item (required) |
| `description` | string | Why you want this item |
| `msrp` | string | Price display (e.g., "$150", "€99") |
| `officialURL` | string | Official product page |
| `purchaseURLs` | []string | Links where to buy |
| `imageURL` | string | Product image URL |
| `priority` | int32 | Importance 0-5 (displayed as stars; 0 renders none) |
| `tags` | []string | Category labels |
| `contextTags` | []string | Occasions (birthday, christmas) |
| `ttl` | duration | Auto-expire after this duration |
| `quantity` | int32 | Number of items available (default: 1) |

### Wish Status

| Field | Description |
|-------|-------------|
| `active` | Whether wish is within TTL |
| `reservations` | List of active reservations (quantity, createdAt, expiresAt) |

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
| `operator.trustedProxyHops` | 0 | Proxies in front that append to `X-Forwarded-For` (0 ignores the header) |
| `httpRoute.enabled` | false | Create HTTPRoute resource |
| `httpRoute.hostnames` | [] | Hostnames for the route |
| `httpRoute.parentRefs` | [] | Gateway references |

> **Upgrading from an earlier release behind a proxy:** set `operator.trustedProxyHops` as part of the upgrade. Earlier versions read the leftmost `X-Forwarded-For` entry, which a caller could set to anything; the header is now ignored unless you say what sits in front. Leaving it at the default puts every visitor arriving through your gateway into one shared bucket, because they all reach the operator from the same address. See [Client addresses behind a proxy](#client-addresses-behind-a-proxy).

### Client addresses behind a proxy

Rate limiting buckets requests by client address, and how that address is read depends on what sits in front of the operator. With the default `operator.trustedProxyHops: 0` the connection address is used and `X-Forwarded-For` is ignored, because a directly reachable server has no way to tell a real header from one the caller made up.

Set `operator.trustedProxyHops` to the number of proxies that append to `X-Forwarded-For` on the way in — one for a single gateway, two when a CDN or tunnel fronts that gateway. The operator then counts that many entries from the right of the header, which is where the outermost proxy wrote the address it saw. Counting from the left instead would read whatever the caller sent, so a caller could rotate values and never hit the limit.

IPv6 clients are bucketed by `/64` rather than by address, because a single subscriber is normally handed a whole `/64` and would otherwise get a fresh allowance for every address in it. IPv4 clients are bucketed per address.

A hop count above zero also assumes the operator is reachable *only* through that chain. A ClusterIP Service is reachable directly by any other pod in the cluster, and such a caller picks its own `X-Forwarded-For` outright, so restrict traffic to the gateway with a NetworkPolicy if in-cluster callers are a concern.

Set it too low and every visitor collapses onto the address of a proxy in the chain, sharing one bucket. Set it higher than the real chain and a caller can supply the entry that gets used. `X-Real-IP` is never read: proxies that append to `X-Forwarded-For` pass it through untouched. A proxy configured to set only `X-Real-IP`, as an nginx with a lone `proxy_set_header X-Real-IP` does, leaves nothing to count, so configure it to append to `X-Forwarded-For` as well.

## Development

### Prerequisites

- Go 1.26+
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
