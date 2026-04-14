# NATS Operator

A Kubernetes operator for running [NATS](https://nats.io) clusters and the
nats-box utility pod, modelled on the upstream
[nats-io/k8s helm chart](https://github.com/nats-io/k8s/tree/main/helm/charts/nats)
but reorganised for the operator pattern: typed CRDs, server-side apply,
controller-side defaults, owner-reference cascades, and zero free-form
escape hatches in the API surface.

## What it manages

The operator owns two custom resources:

| Kind | Group | What it does |
|---|---|---|
| `NatsCluster` | `nats.crewlet.cloud/v1alpha1` | A clustered NATS server deployment — StatefulSet, Services, ConfigMap, PDB, optional reloader / prom-exporter sidecars, optional websocket Ingress, JetStream / leaf-nodes / mqtt / gateway / monitor / profiling listeners, and typed decentralized JWT authentication. |
| `NatsBox` | `nats.crewlet.cloud/v1alpha1` | A long-running utility pod with the `nats` CLI and pre-configured contexts (URLs + creds + TLS). Useful for `kubectl exec` debugging. |

JetStream resource management (Streams, Consumers, KV, Object Store) is
delegated to [NACK](https://github.com/nats-io/nack). The operator
**auto-wires NACK's `Account` CRD** from each account declared in
`spec.auth.jwt.accounts[]` that has a `userCreds` reference set, so NACK
Stream / Consumer / KV / ObjectStore CRs only need to reference
`account: <natscluster-name>-<account.name>` — no URLs, no credentials
repeated per resource. If NACK isn't installed the operator degrades
gracefully and surfaces a `NackIntegrationAvailable=False` status
condition; the integration lights up automatically when NACK arrives.

## Quick start

### Install the operator

The operator ships as a Helm chart under [`charts/nats-operator/`](charts/nats-operator/)
and is published to GitHub Container Registry as an OCI artifact:

```bash
helm install nats-operator \
  oci://ghcr.io/crewlet/nats-operator/charts/nats-operator
```

The operator image is published alongside the chart at
`ghcr.io/crewlet/nats-operator` (tagged with semver release tags and `latest`).

The chart bundles the CRDs, manager Deployment, RBAC, and (optionally) a
PodMonitor / NetworkPolicy / PodDisruptionBudget for the operator pod itself.
See the [chart README](charts/nats-operator/README.md) for the full values
table.

### Create a NatsCluster

```yaml
apiVersion: nats.crewlet.cloud/v1alpha1
kind: NatsCluster
metadata:
  name: my-nats
spec:
  replicas: 3
  image:
    repository: nats
    tag: 2.12.6-alpine
  container:
    resources:
      requests: {cpu: 500m, memory: 2Gi}
  config:
    jetstream:
      enabled: true
      fileStore:
        pvc:
          resources:
            requests:
              storage: 100Gi
```

```bash
kubectl apply -f cluster.yaml
kubectl get natscluster my-nats -w
```

The operator will:
- Render `nats.conf` and store it in a ConfigMap mounted at `/etc/nats-config`
- Create a headless Service for pod DNS and a client Service publishing the
  enabled listeners (nats / leafnodes / websocket / mqtt / gateway)
- Create a StatefulSet with three pods, the nats container, the config
  reloader sidecar (so config edits hot-reload without rolling), and a
  JetStream PVC per pod
- Create a PodDisruptionBudget (defaults to `maxUnavailable: 1`)
- Publish the connection URLs in `status.endpoints` so other tooling can
  consume them without guessing the Service name pattern

### Create a NatsBox

```yaml
apiVersion: nats.crewlet.cloud/v1alpha1
kind: NatsBox
metadata:
  name: my-box
spec:
  clusterRef:
    name: my-nats
```

```bash
kubectl exec -it deploy/my-box -- nats stream ls
```

The operator generates a `default` nats CLI context pointing at the
referenced cluster's headless Service. To add more contexts (e.g. for a
remote NATS), set `spec.contexts`.

## Authentication (decentralized JWT) + NACK auto-wiring

The operator supports NATS decentralized auth as a typed surface under
`spec.auth.jwt`. You generate an operator, accounts, and user credentials
out-of-band with [`nsc`](https://docs.nats.io/using-nats/nats-tools/nsc),
drop the resulting JWTs and creds files into Kubernetes Secrets, and
reference them from the cluster spec. The operator renders the
`operator:` / `system_account:` / `resolver:` / `resolver_preload:`
directives into `nats.conf` automatically and — if `userCreds` is set on
an account — also creates a NACK `Account` CR pointing at the cluster so
NACK-managed JetStream resources can use the account by name.

```yaml
apiVersion: nats.crewlet.cloud/v1alpha1
kind: NatsCluster
metadata:
  name: my-nats
spec:
  replicas: 3
  image: {repository: nats, tag: 2.12.6-alpine}
  config:
    jetstream:
      enabled: true
      fileStore:
        pvc:
          resources: {requests: {storage: 10Gi}}
  auth:
    jwt:
      # The operator JWT — root of trust. Generated with `nsc` and
      # stored in a Secret the user manages.
      operator:
        name: nats-operator-jwt
        key: operator.jwt

      # Public key of the account with cluster-admin privileges.
      # Must match one of the accounts[] entries below.
      systemAccount: AASYSTEM_ACCOUNT_PUBKEY

      accounts:
        - name: system
          publicKey: AASYSTEM_ACCOUNT_PUBKEY
          jwt:
            name: nats-system-account-jwt
            key: account.jwt
          # When set, the operator creates a NACK `Account` CR named
          # "my-nats-system" pointing at this creds secret, so NACK CRs
          # can reference `account: my-nats-system`.
          userCreds:
            name: nats-system-user-creds
            key: nats.creds

        - name: app
          publicKey: BBAPP_ACCOUNT_PUBKEY
          jwt:
            name: nats-app-account-jwt
            key: account.jwt
          userCreds:
            name: nats-app-user-creds
            key: nats.creds

      resolver:
        # "memory" serves only the preloaded accounts above (static —
        # additions require a spec edit). "full" uses on-disk storage
        # and lets the system account push new accounts at runtime.
        type: memory
```

With this in place, a NACK stream is a three-line CR — no URLs, no
credential repetition, cluster lifecycle propagates via owner references:

```yaml
apiVersion: jetstream.nats.io/v1beta2
kind: Stream
metadata:
  name: orders
spec:
  account: my-nats-app     # auto-created by the operator from auth.jwt.accounts[1]
  name: ORDERS
  subjects: [orders.*]
```

**What the operator does behind the scenes:**

1. Reads the operator JWT and each account JWT from the user's Secrets.
2. Builds an operator-managed `my-nats-auth` Secret containing
   `operator.jwt` (copied byte-for-byte), `resolver_preload.conf` (the
   `resolver_preload { ... }` block with account JWTs inlined), and
   `auth.conf` (the top-level fragment).
3. Mounts that Secret at `/etc/nats-auth/` in the nats container and
   emits `include "/etc/nats-auth/auth.conf";` in the rendered
   `nats.conf`. The JWT material never lands in a ConfigMap.
4. For each account with `userCreds`, SSA-applies a NACK
   `jetstream.nats.io/v1beta2 Account` CR named
   `<cluster>-<account.name>` with `servers` filled from the cluster's
   `Status.Endpoints.Client` and `creds` pointing at the user-supplied
   Secret. Owner-referenced back to the NatsCluster so deletion cascades.
5. JWT rotation: update your account JWT Secret → next reconcile
   rebuilds the managed auth Secret → the reloader sidecar detects the
   mount change and signals nats-server. No pod restart for
   hot-reloadable changes.

**When NACK isn't installed:** the NatsCluster itself is unaffected. The
operator sets `NackIntegrationAvailable=False` with a clear reason and
requeues every 60 seconds. As soon as NACK is installed the condition
flips to `True` and the Account CRs get created on the next reconcile.

## Multi-tenancy

Two `NatsCluster` resources in the same namespace are guaranteed not to
interfere:

- All managed resources are named `<natscluster-name>-<suffix>`. The
  user-overridable name fields the helm chart exposes are deliberately
  not modeled, so two clusters can never produce a colliding resource
  name.
- Pod selectors use a canonical key
  (`nats.crewlet.cloud/cluster: <natscluster-name>`) that is unique per
  cluster. Service / PDB / PodMonitor / StatefulSet selectors all match
  on this key.
- The NATS `cluster.name` is hardcoded to the CR name, so route packets
  cannot leak between clusters even if a user accidentally peers two
  StatefulSets via DNS.
- Each pod's `server_name` is set from the downward API
  (`$SERVER_NAME = $POD_NAME`), so JetStream sees globally unique
  server names.
- All managed resources carry an owner reference back to the
  `NatsCluster`, so deleting the CR cascades cleanly without GC of
  resources owned by other clusters.

## Development

```bash
# Code generation (deepcopy + CRDs)
make generate manifests

# Build the manager binary
make build

# Run tests (includes the nats.conf renderer golden tests)
make test

# Lint
make lint

# Sync the helm chart's CRDs and manager-role from config/
make helm-sync

# Helm lint + template smoke tests
make helm-lint helm-template

# Package the helm chart
make helm-package
```

The operator is built with [kubebuilder](https://book.kubebuilder.io/) and
uses [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime).

## Contributing

Issues and pull requests welcome. Please run `make lint` and `make test`
before submitting.
