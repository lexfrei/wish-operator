# wish-operator

Kubernetes operator for managing wishlists. Create wishes as Kubernetes resources, share them via web UI, and let others reserve gifts.

## Features

- **Wish CRD** — define wishes with title, description, price, images, priority (0-5 stars), and tags
- **Quantity support** — specify multiple items per wish, reserve partially
- **Web UI** — HTMX-powered interface for viewing and reserving wishes
- **Reservations** — multiple anonymous reservations per wish, 1-8 weeks with automatic expiration
- **TTL** — wishes can auto-expire after a defined duration
- **Rate limiting** — per-IP rate limiting to prevent abuse, see [Client addresses behind a proxy](#client-addresses-behind-a-proxy)
- **Multi-language UI** — language from the `?lang=` parameter, then `Accept-Language`, falling back to English
- **Gateway API** — HTTPRoute support for ingress via Gateway API

## Installation

### Helm (recommended)

```bash
helm install wish-operator oci://ghcr.io/lexfrei/charts/wish-operator \
  --namespace wish-operator \
  --create-namespace
```

Helm installs the CRDs from the chart's `crds/` directory only on the first install and never touches them on upgrade. After upgrading to a release that changes the CRD, pull the chart and apply the updated definitions once:

```bash
helm pull oci://ghcr.io/lexfrei/charts/wish-operator --version <CHART_VERSION> --untar
kubectl apply --filename wish-operator/crds/
```

Breaking change: `replicaCount` above 1 now requires `operator.leaderElection: true`, and the chart fails to render otherwise. Earlier versions accepted that combination and ran every replica as an active reconciler, which raced on the same objects. If your release sets `replicaCount` above 1, add `--set operator.leaderElection=true` to the upgrade.

### With HTTPRoute

```bash
helm install wish-operator oci://ghcr.io/lexfrei/charts/wish-operator \
  --namespace wish-operator \
  --create-namespace \
  --set httpRoute.enabled=true \
  --set 'httpRoute.parentRefs[0].name=my-gateway' \
  --set 'httpRoute.hostnames[0]=wishes.example.com'
```

### Upgrading

Reservations used to live in three single-reservation status fields (`reserved`, `reservedAt`, `reservationExpires`). Releases v0.3.0 through v0.4.1 carried a reconcile-time migration that rewrote them into the `reservations` list; those fields and the migration are gone from later releases.

If you are still on v0.0.1 or v0.0.2, go through v0.4.1 first. Jumping straight to a later release skips the migration and drops any reservation still stored in the old format. `helm upgrade` never installs CRDs, only `helm install` does, so every hop below applies the CRD by hand. Which side of the operator it goes on differs, and getting it backwards loses data:

1. Record what is reserved today, before touching anything, expiry included. Keep the output.

    ```bash
    kubectl get wishes --all-namespaces --output json \
      | jq -r '.items[] | select(.status.reserved == true) | [.metadata.namespace, .metadata.name, .status.reservationExpires] | @tsv'
    ```

2. Apply the v0.4.1 CRD **before** upgrading the operator to v0.4.1. The v0.0.x schema has no `reservations` property, and an API server prunes what the schema does not declare on every write. The v0.4.1 operator writes the migrated list and the cleared legacy fields in a single status update, so under the old schema the new list is pruned away while the clearing sticks, and the reservation is gone.

    ```bash
    kubectl apply --filename https://raw.githubusercontent.com/lexfrei/wish-operator/refs/tags/v0.4.1/charts/wish-operator/crds/wishlist.k8s.lex.la_wishes.yaml
    ```

3. Upgrade to v0.4.1. Starting it reconciles every Wish once. Check the names from step 1 whose recorded expiry is still in the future — those must now carry a non-empty `reservations` list:

    ```bash
    kubectl get wish NAME --namespace NAMESPACE --output jsonpath='{.status.reservations}'
    ```

    Two things that look like failure but are not. An entry whose expiry had already passed is migrated and pruned within the same reconcile, so its list is legitimately empty — that is why step 1 records the expiry, and why only the still-valid ones are worth checking. And do not use the absence of `status.reserved` as the signal either: it goes away whether the reservation migrated or was pruned into nothing.

4. Only once step 3 checks out, upgrade to this release and apply its CRD **after** the operator, substituting the tag you are installing.

    ```bash
    kubectl apply --filename https://raw.githubusercontent.com/lexfrei/wish-operator/refs/tags/vX.Y.Z/charts/wish-operator/crds/wishlist.k8s.lex.la_wishes.yaml
    ```

The order flips between step 2 and step 4 because the two schemas differ in kind. The v0.4.1 one adds a property, and the operator needs it in place to have somewhere to write. This release's one removes properties, so it has to wait behind the step 3 check that says the data has moved. Applying it is not destructive by itself; the legacy fields go on the next status write to each object, which may not happen for a while — the controller only writes when something actually changed, so a Wish sitting in steady state keeps them in etcd, inert, until it next needs updating.

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
| `quantity` | int32 | Number of items available (default: 1); 0 means unlimited |

### Wish Status

| Field | Description |
|-------|-------------|
| `active` | Whether wish is within TTL |
| `reservations` | List of active reservations (quantity, createdAt, expiresAt) |
| `conditions` | Declared on the type; the controller does not populate it yet |

### Reservation limits

Reserving is anonymous, so two limits are fixed in the operator rather than exposed as configuration. A wish holds at most 100 live reservations; past that it answers 409 until some expire, and the page stops offering the form. A single reservation claims at most 100 items on a wish with unlimited quantity, where nothing else bounds the request. A wish with a declared quantity is bounded by that quantity instead, however large it is.

These limits keep a wish repairable, not unabusable. What they prevent is the terminal case: an object that grows past what the API server accepts, after which nothing can update it again, including the controller pass that prunes expired reservations. What they do not prevent is one client taking all 100 slots and holding them, because a reservation runs for up to eight weeks and carries no identity at all. The wish then shows "cannot take any more reservations" until they expire, and nothing distinguishes the client that did it from a hundred genuine visitors. Treat the reservation limits as a bound on the damage, not as access control: if a wishlist is public and contested, put it behind something that authenticates.

## Configuration

### Helm Values

| Parameter | Default | Description |
|-----------|---------|-------------|
| `replicaCount` | 1 | Number of replicas; above 1 requires `operator.leaderElection` |
| `operator.leaderElection` | false | Elect a single active reconciler; required for multiple replicas |
| `image.repository` | ghcr.io/lexfrei/wish-operator | Image repository |
| `image.tag` | "" | Image tag (defaults to chart appVersion) |
| `operator.namespace` | default | Namespace the web UI serves Wishes from; the controller reconciles all namespaces |
| `operator.rateLimit` | 30 | Requests per second per IP, counted per replica |
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
- envtest binaries for the controller suite, installed by `make setup-envtest`

### Build

```bash
make build
```

### Test

The controller suite runs against envtest, so it needs the control-plane binaries: run `make setup-envtest` once and the suite finds them under `bin/k8s` on its own. Use `make test` rather than invoking the Go test command across all packages directly, which also pulls in `test/e2e` and fails there for want of a Kind cluster.

```bash
# Go tests
make test

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
