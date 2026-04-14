# nats-operator

A Helm chart for the [nats-operator](https://github.com/crewlet/nats-operator) — a Kubernetes operator that manages NATS clusters and the nats-box utility pod via the `NatsCluster` and `NatsBox` CRDs.

## TL;DR

```bash
helm install nats-operator \
  oci://ghcr.io/crewlet/nats-operator/charts/nats-operator
```

Pin a specific chart version with `--version <x.y.z>`. The operator image
published alongside the chart lives at `ghcr.io/crewlet/nats-operator`.

Or from source:

```bash
helm install nats-operator ./charts/nats-operator
```

Then create your first cluster:

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
  config:
    jetstream:
      enabled: true
      fileStore:
        pvc:
          resources:
            requests:
              storage: 10Gi
```

## CRDs

The chart bundles the `NatsCluster` and `NatsBox` CRDs under `crds/`. Helm installs them on first install but does **not** upgrade them on `helm upgrade`. When you bump the operator version, apply the new CRDs explicitly:

```bash
kubectl apply -f charts/nats-operator/crds/
```

To skip CRD installation entirely (e.g. when you manage CRDs out-of-band via GitOps), set `crds.install=false`.

## Multi-replica HA

The operator supports leader election so multiple replicas provide HA without duplicate reconciles:

```yaml
replicaCount: 2
podDisruptionBudget:
  enabled: true
  minAvailable: 1
topologySpreadConstraints:
  - maxSkew: 1
    topologyKey: kubernetes.io/hostname
    whenUnsatisfiable: DoNotSchedule
```

The chart fills in the `labelSelector` on each topology constraint so it actually targets the operator pods.

## Prometheus integration

If you have prometheus-operator installed, enable the bundled `ServiceMonitor`:

```yaml
metrics:
  enabled: true
  serviceMonitor:
    enabled: true
    interval: 30s
    labels:
      release: kube-prometheus-stack   # match your Prometheus's serviceMonitorSelector
```

Metrics are served over HTTPS by default with token-review authentication. Disable HTTPS via `metrics.secure=false` if you front the operator with your own auth proxy.

## Values

| Key | Type | Default | Description |
|---|---|---|---|
| `replicaCount` | int | `1` | Number of operator replicas. Use leader election for HA. |
| `image.repository` | string | `ghcr.io/crewlet/nats-operator` | Operator image (full path including registry). |
| `image.tag` | string | `""` | Operator image tag. Defaults to `.Chart.AppVersion`. |
| `image.digest` | string | `""` | Image digest. When set, overrides tag for reproducible installs. |
| `image.pullPolicy` | string | `IfNotPresent` | Image pull policy. |
| `imagePullSecrets` | list | `[]` | Image pull secrets. |
| `nameOverride` | string | `""` | Override the chart name. |
| `fullnameOverride` | string | `""` | Override the fully-qualified release name. |
| `namespaceOverride` | string | `""` | Override the namespace the chart installs into. |
| `commonLabels` | map | `{}` | Labels stamped on every rendered resource. |
| `crds.install` | bool | `true` | Whether the chart bundles the CRDs at install time. |
| `serviceAccount.create` | bool | `true` | Whether to create a ServiceAccount. |
| `serviceAccount.automount` | bool | `true` | Automount the SA token. |
| `serviceAccount.annotations` | map | `{}` | SA annotations (e.g. for IRSA / Workload Identity). |
| `serviceAccount.name` | string | `""` | Override the generated SA name. |
| `podAnnotations` | map | `{kubectl.kubernetes.io/default-container: manager}` | Annotations on the operator pod. |
| `podLabels` | map | `{}` | Labels on the operator pod. |
| `podSecurityContext` | object | `{runAsNonRoot: true, seccompProfile: {type: RuntimeDefault}}` | Pod-level security context. |
| `securityContext` | object | hardened defaults | Manager container security context. |
| `resources` | object | small requests / 256Mi limit | Manager container resources. |
| `livenessProbe` | object | HTTP `/healthz` on `:8081` | Manager liveness probe. |
| `readinessProbe` | object | HTTP `/readyz` on `:8081` | Manager readiness probe. |
| `volumes` | list | `[]` | Extra pod volumes. |
| `volumeMounts` | list | `[]` | Extra container volume mounts. |
| `nodeSelector` | map | `{}` | Pod nodeSelector. |
| `tolerations` | list | `[]` | Pod tolerations. |
| `affinity` | object | `{}` | Pod affinity rules. |
| `topologySpreadConstraints` | list | `[]` | Pod topology spread constraints (the chart fills in `labelSelector`). |
| `priorityClassName` | string | `""` | Pod priorityClassName. |
| `runtimeClassName` | string | `""` | Pod runtimeClassName. |
| `terminationGracePeriodSeconds` | int | `10` | Pod terminationGracePeriodSeconds. |
| `leaderElection.enabled` | bool | `true` | Enable manager leader election. |
| `healthProbeBindAddress` | string | `:8081` | Manager health probe bind address. |
| `metrics.enabled` | bool | `true` | Whether the manager serves metrics. |
| `metrics.bindAddress` | string | `:8443` | Metrics bind address passed via `--metrics-bind-address`. |
| `metrics.port` | int | `8443` | Container port for metrics. |
| `metrics.secure` | bool | `true` | Serve metrics over HTTPS with token-review auth. |
| `metrics.service.create` | bool | `true` | Create a Service in front of the metrics port. |
| `metrics.service.type` | string | `ClusterIP` | Metrics Service type. |
| `metrics.service.annotations` | map | `{}` | Metrics Service annotations. |
| `metrics.serviceMonitor.enabled` | bool | `false` | Create a prometheus-operator ServiceMonitor. |
| `metrics.serviceMonitor.labels` | map | `{}` | Extra labels on the ServiceMonitor. |
| `metrics.serviceMonitor.interval` | string | `30s` | Prometheus scrape interval. |
| `metrics.serviceMonitor.scrapeTimeout` | string | `10s` | Prometheus scrape timeout. |
| `rbac.create` | bool | `true` | Create the operator's ClusterRole / RoleBindings. |
| `extraArgs` | list | `[]` | Extra args appended to the manager command line. |
| `podDisruptionBudget.enabled` | bool | `false` | Create a PDB for the operator pod. |
| `podDisruptionBudget.minAvailable` | int / string | `1` | PDB minAvailable. Mutually exclusive with `maxUnavailable`. |
| `podDisruptionBudget.maxUnavailable` | int / string | unset | PDB maxUnavailable. |
| `networkPolicy.enabled` | bool | `false` | Create a NetworkPolicy for the operator pod. |
| `networkPolicy.extraIngress` | list | `[]` | Extra ingress rules merged into the policy. |
| `networkPolicy.extraEgress` | list | `[]` | Extra egress rules merged into the policy. |

## Maintenance

The chart's RBAC and CRDs are generated from the kubebuilder manifests in `config/`. After editing `// +kubebuilder:rbac:` markers in the operator code, run:

```bash
make helm-sync   # syncs CRDs and the manager ClusterRole into the chart
make helm-lint   # helm lint
make helm-template  # template smoke tests with default + feature-enabled values
```

These targets run automatically as part of `make helm-package`.
