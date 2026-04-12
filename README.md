# NATS Operator

A Kubernetes operator for running [NATS](https://nats.io) clusters and the
nats-box utility pod, modelled on the upstream
[nats-io/k8s helm chart](https://github.com/nats-io/k8s/tree/main/helm/charts/nats)
but reorganised for the operator pattern: typed CRDs, server-side apply,
controller-side defaults, owner-reference cascades, and zero free-form
escape hatches in the API surface.

## What it manages

The operator owns three custom resources:

| Kind | Group | What it does |
|---|---|---|
| `NatsCluster` | `nats.crewlet.cloud/v1alpha1` | A clustered NATS server deployment — StatefulSet, Services, ConfigMap, PDB, optional reloader / prom-exporter sidecars, optional websocket Ingress, JetStream / leaf-nodes / mqtt / gateway / monitor / profiling listeners. |
| `NatsBox` | `nats.crewlet.cloud/v1alpha1` | A long-running utility pod with the `nats` CLI and pre-configured contexts (URLs + creds + TLS). Useful for `kubectl exec` debugging. |

JetStream resource management (Streams, Consumers, KV, Object Store) is
delegated to [NACK](https://github.com/nats-io/nack) — install it alongside
the operator and write NACK CRs that target your `NatsCluster` via the URL
the operator publishes in `Status.Endpoints`.

## Quick start

### Install the operator

The operator ships as a Helm chart under [`charts/nats-operator/`](charts/nats-operator/):

```bash
helm install nats-operator ./charts/nats-operator
```

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

## API design

`NatsCluster` is a typed mirror of the upstream helm chart's
[`values.yaml`](https://github.com/nats-io/k8s/blob/main/helm/charts/nats/values.yaml),
with three deliberate departures:

1. **No `merge` / `patch` escape hatches.** Every field is typed. The chart
   uses raw YAML overlays to compensate for helm's limited templating —
   the operator owns rendering, so we can model new K8s fields properly
   when users need them. The one exception is `Config.Includes`, a typed
   list of `SecretKeySelector` / `ConfigMapKeySelector` references that
   the operator mounts and pulls into `nats.conf` via the native `include`
   directive. That covers JWT operator/account/user setups, custom
   resolvers, and any free-form NATS config that doesn't yet have a typed
   field.

2. **Single source of truth for derived state.** The chart exposes
   listener ports in three places (`config.<listener>.port`,
   `container.ports.<listener>`, `service.ports.<listener>`) and lets
   users set them inconsistently. Here, the listener port lives only in
   `config.<listener>.port`; container ports and service ports are
   derived. Replicas live at `spec.replicas` (a workload concern), not
   buried inside `config.cluster.replicas`. Cluster routing is enabled
   automatically when `replicas > 1` — there is no separate
   `cluster.enabled` toggle.

3. **CEL validation rules** catch common misconfigurations at admission
   time:
   - `reloader.enabled || podTemplate.configChecksumAnnotation` — at
     least one config-rollout strategy must be in effect, otherwise the
     operator literally cannot apply config changes.
   - `monitor.tlsEnabled` requires `monitor.enabled`.
   - `memoryStore.enabled` requires `maxSize`.
   - `webSocketIngress.enabled` requires non-empty `hosts`.
   - `pdb.minAvailable` and `pdb.maxUnavailable` are mutually exclusive.
   - `promExporter.podMonitor.enabled` requires `promExporter.enabled`.
   - `tlsCA.configMap` and `tlsCA.secret` are mutually exclusive.
   - `configInclude.secret` xor `configInclude.configMap`.

The result: the typical user spec is dramatically smaller than the helm
chart equivalent, but every helm chart capability is preserved.

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

## Reconcile model

The operator uses [server-side apply](https://kubernetes.io/docs/reference/using-api/server-side-apply/)
with `FieldManager: "nats-operator"`. Every reconcile:

1. Fetches the CR
2. Applies controller-side defaults (image versions, port numbers,
   defaulting `enabled: true` on optional blocks)
3. Renders `nats.conf` once and shares the bytes between the ConfigMap
   builder and the StatefulSet builder (so the optional checksum
   annotation matches the file the pod actually mounts)
4. Builds every owned resource from the defaulted spec
5. SSAs each with `client.ForceOwnership` so we own only the fields we
   set — user-supplied annotations, labels, and unrelated fields are
   preserved across reconciles
6. Patches `Status.Replicas` / `Status.ReadyReplicas` /
   `Status.Endpoints` / `Status.Conditions` from the live StatefulSet

Config changes propagate via the reloader sidecar by default (hot reload,
no pod restart). Users can opt into rolling restarts on every change
instead with `podTemplate.configChecksumAnnotation: true`. A CEL rule
rejects specs that disable both, since the operator would otherwise be
unable to apply config changes.

## Status

| | Status |
|---|---|
| `NatsCluster` API | ✅ v1alpha1 |
| `NatsCluster` controller | ✅ v1alpha1 (StatefulSet, Service, ConfigMap, PDB, ServiceAccount, PodMonitor, websocket Ingress) |
| `NatsBox` API | ✅ v1alpha1 |
| `NatsBox` controller | ✅ v1alpha1 (Deployment, contexts Secret) |
| Helm chart | ✅ |
| nats.conf renderer golden tests | ✅ |
| Envtest controller integration tests | ⚠️ scaffold only |
| End-to-end tests on a real cluster | ⏳ planned |
| JWT auth typed surface | ⏳ today via `Config.Resolver` + `Config.Includes`; typed convenience fields planned |
| NACK wrapper CRDs (NatsStream, etc.) | ❌ deferred — use NACK CRs directly with `Status.Endpoints.Client` |

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
Source layout:

```
api/v1alpha1/             CRD types (NatsCluster, NatsBox)
internal/controller/
  natscluster_controller.go    Reconcile loop for NatsCluster
  natsbox_controller.go        Reconcile loop for NatsBox
  defaults.go                  Controller-side defaulting
  labels.go                    Canonical labels and selector helpers
  naming.go                    Resource naming + mount path constants
  natsconf.go                  nats.conf renderer + HOCON-flavored serializer
  natsconf_test.go             Golden tests for the renderer
  build_*.go                   Pure builders for each managed K8s resource
  natsbox_*.go                 NatsBox-specific helpers and builders
config/                   Kubebuilder kustomize manifests (CRDs, RBAC, samples)
charts/nats-operator/     Helm chart (CRDs and manager-role auto-synced from config/)
```

The `nats.conf` renderer is a custom HOCON-flavored serializer rather
than `encoding/json`, because nats-server's `include` directive isn't
expressible as JSON. The renderer builds a `map[string]any` structurally
and emits keys deterministically (sorted, with includes always last) so
the same spec produces byte-identical output across reconciles —
critical because the rendered file lands in a ConfigMap and unstable
rendering would force needless rolling restarts.

## Contributing

Issues and pull requests welcome. Please run `make lint` and `make test`
before submitting.

## License

Copyright 2026 Crewlet contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
