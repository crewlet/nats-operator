# NATS Operator

A Kubernetes operator for [NATS](https://nats.io), modelled on the upstream
[nats-io/k8s Helm chart](https://github.com/nats-io/k8s/tree/main/helm/charts/nats)
but reorganised as typed CRDs with server-side apply and no free-form escape
hatches.

## Custom resources

| Kind | Purpose |
|---|---|
| `NatsCluster` | A clustered NATS server: StatefulSet, Services, ConfigMap, PDB, reloader/exporter sidecars, optional websocket Ingress, JetStream, leaf nodes, MQTT, gateways, decentralized JWT auth. |
| `NatsBox` | Utility pod with the `nats` CLI pre-wired to a cluster — handy for `kubectl exec` debugging. |

JetStream resources (Streams, Consumers, KV, Object Store) are delegated to
[NACK](https://github.com/nats-io/nack). See [NACK integration](#nack-integration) below.

## Install

```bash
helm install nats-operator \
  oci://ghcr.io/crewlet/nats-operator/charts/nats-operator
```

The chart bundles CRDs, RBAC, and the manager Deployment. Values are
documented in the [chart README](charts/nats-operator/README.md).

## Create a cluster

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
              storage: 100Gi
```

Connection URLs are published in `status.endpoints`.

## Create a nats-box

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

A `default` CLI context is generated for the referenced cluster. Add more
via `spec.contexts`.

## Decentralized JWT auth

Generate operator/account/user credentials with
[`nsc`](https://docs.nats.io/using-nats/nats-tools/nsc), store the JWTs
and creds in Secrets, and reference them from `spec.auth.jwt`:

```yaml
spec:
  auth:
    jwt:
      operator:
        name: nats-operator-jwt
        key: operator.jwt
      systemAccount: AASYSTEM_ACCOUNT_PUBKEY
      accounts:
        - name: system
          publicKey: AASYSTEM_ACCOUNT_PUBKEY
          jwt:       {name: nats-system-account-jwt, key: account.jwt}
          userCreds: {name: nats-system-user-creds,  key: nats.creds}
        - name: app
          publicKey: BBAPP_ACCOUNT_PUBKEY
          jwt:       {name: nats-app-account-jwt, key: account.jwt}
          userCreds: {name: nats-app-user-creds,  key: nats.creds}
      resolver:
        type: memory  # or "full" for runtime account pushes
```

JWT material is staged into an operator-managed Secret mounted at
`/etc/nats-auth/` — never into a ConfigMap. Rotating a JWT Secret triggers
a hot reload via the reloader sidecar; no pod restart.

## NACK integration

For each account with `userCreds`, the operator creates a NACK `Account`
CR named `<cluster>-<account.name>`, wired to the cluster endpoints and
creds Secret. NACK Streams then reference the account by name only:

```yaml
apiVersion: jetstream.nats.io/v1beta2
kind: Stream
metadata:
  name: orders
spec:
  account: my-nats-app   # auto-created from auth.jwt.accounts[]
  name: ORDERS
  subjects: [orders.*]
```

If NACK isn't installed, the `NackIntegrationAvailable` condition goes
`False` and the integration lights up automatically once NACK arrives.
The NatsCluster itself is unaffected.

## Development

```bash
make generate manifests   # deepcopy + CRDs
make build                # manager binary
make test                 # unit + golden tests
make lint
make helm-sync            # sync chart CRDs from config/
make helm-lint helm-template helm-package
```

Built with [kubebuilder](https://book.kubebuilder.io/) and
[controller-runtime](https://github.com/kubernetes-sigs/controller-runtime).

## Contributing
<<<<<<< HEAD
// TODO(user): Add detailed information on how you would like others to contribute to this project

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

=======

Issues and PRs welcome. Run `make lint test` before submitting.
>>>>>>> tmp-original-05-05-26-03-23
