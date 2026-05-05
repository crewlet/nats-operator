package v1alpha1

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NatsClusterSpec defines the desired state of a NatsCluster.
//
// The shape borrows vocabulary from the upstream nats-io/k8s helm chart but is
// reorganized for an operator: the controller is the single source of truth
// for derived state, so port numbers, mount paths, container ports, route URLs
// etc. are computed from a small set of typed fields rather than mirrored in
// multiple places. Free-form NATS server config that the typed surface does
// not (yet) cover goes through Config.Includes.
// +kubebuilder:validation:XValidation:rule="(!has(self.reloader) || !has(self.reloader.enabled) || self.reloader.enabled) || (has(self.podTemplate) && self.podTemplate.configChecksumAnnotation)",message="at least one of reloader.enabled or podTemplate.configChecksumAnnotation must be true so the operator can apply config changes"
type NatsClusterSpec struct {
	// replicas is the number of nats pods. The operator wires this through to
	// the StatefulSet replica count and, when greater than 1, automatically
	// renders a NATS cluster routing block — there is no separate "enable
	// clustering" toggle.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// image is the nats server container image. The reloader and prometheus
	// exporter sidecars carry their own image fields under their respective
	// blocks since they ship from different repositories.
	// +optional
	Image ImageSpec `json:"image,omitzero"`

	// global applies cross-cutting image and label settings to every resource.
	// +optional
	Global GlobalSpec `json:"global,omitzero"`

	// tlsCA references a CA bundle that gets mounted into every TLS block.
	// +optional
	TLSCA TLSCASpec `json:"tlsCA,omitzero"`

	// config holds the NATS server configuration the operator renders into
	// nats.conf — listeners, jetstream, cluster routing, monitor, etc.
	// +optional
	Config NatsConfigSpec `json:"config,omitzero"`

	// container customizes per-container knobs (env, resources, probes,
	// security context) for the nats server container.
	// +optional
	Container ContainerSpec `json:"container,omitzero"`

	// reloader customizes the nats config reloader sidecar.
	// +optional
	Reloader ReloaderSpec `json:"reloader,omitzero"`

	// promExporter customizes the prometheus nats exporter sidecar.
	// +optional
	PromExporter PromExporterSpec `json:"promExporter,omitzero"`

	// service customizes the client-facing Service.
	// +optional
	Service ServiceSpec `json:"service,omitzero"`

	// statefulSet customizes the underlying StatefulSet.
	// +optional
	StatefulSet StatefulSetSpec `json:"statefulSet,omitzero"`

	// podTemplate customizes the StatefulSet pod template.
	// +optional
	PodTemplate PodTemplateSpec `json:"podTemplate,omitzero"`

	// headlessService customizes the headless Service used for pod DNS.
	// +optional
	HeadlessService HeadlessServiceSpec `json:"headlessService,omitzero"`

	// configMap customizes (or replaces) the generated nats config ConfigMap.
	// Set existingName to point at a ConfigMap you manage yourself; the operator
	// will mount it instead of generating one.
	// +optional
	ConfigMap ConfigMapSpec `json:"configMap,omitzero"`

	// podDisruptionBudget customizes the PDB.
	// +optional
	PodDisruptionBudget PodDisruptionBudgetSpec `json:"podDisruptionBudget,omitzero"`

	// serviceAccount customizes the ServiceAccount used by the StatefulSet pods.
	// +optional
	ServiceAccount ServiceAccountSpec `json:"serviceAccount,omitzero"`

	// auth configures the cluster's authentication. When unset, the cluster
	// runs without authentication (only suitable for fully-isolated workloads).
	// +optional
	Auth AuthSpec `json:"auth,omitzero"`
}

// GlobalSpec mirrors `global` from the upstream chart.
type GlobalSpec struct {
	// imagePullPolicy is the default image pull policy applied to every container.
	// +optional
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// imagePullSecrets are image pull secrets attached to every pod spec.
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// imageRegistry is the default registry prefix used for every image.
	// +optional
	ImageRegistry string `json:"imageRegistry,omitempty"`

	// labels are added to every managed resource.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
}

// TLSCASpec references a CA bundle that gets mounted into every TLS block.
// Exactly one of configMap or secret must be set.
// +kubebuilder:validation:XValidation:rule="(has(self.configMap) ? 1 : 0) + (has(self.secret) ? 1 : 0) <= 1",message="at most one of configMap or secret may be set"
type TLSCASpec struct {
	// configMap selects a key in a ConfigMap holding the CA bundle.
	// +optional
	ConfigMap *corev1.ConfigMapKeySelector `json:"configMap,omitempty"`
	// secret selects a key in a Secret holding the CA bundle.
	// +optional
	Secret *corev1.SecretKeySelector `json:"secret,omitempty"`
}

// NatsConfigSpec is the typed representation of the NATS server config the
// operator renders into nats.conf.
type NatsConfigSpec struct {
	// +optional
	Cluster ClusterConfig `json:"cluster,omitzero"`
	// +optional
	JetStream JetStreamConfig `json:"jetstream,omitzero"`
	// +optional
	Nats NatsListenerConfig `json:"nats,omitzero"`
	// +optional
	LeafNodes ListenerConfig `json:"leafnodes,omitzero"`
	// +optional
	WebSocket WebSocketConfig `json:"websocket,omitzero"`
	// +optional
	MQTT ListenerConfig `json:"mqtt,omitzero"`
	// +optional
	Gateway ListenerConfig `json:"gateway,omitzero"`
	// +optional
	Monitor MonitorConfig `json:"monitor,omitzero"`
	// +optional
	Profiling SimpleListenerConfig `json:"profiling,omitzero"`

	// serverNamePrefix is prepended to each pod's server name. Helpful for
	// keeping server names unique across a super-cluster.
	// +optional
	ServerNamePrefix string `json:"serverNamePrefix,omitempty"`

	// includes references user-managed Secrets or ConfigMaps whose contents
	// are mounted into the nats container and pulled into nats.conf via the
	// native `include` directive. Use this for free-form server config the
	// typed spec does not (yet) cover — JWT operator/account/user blocks,
	// custom resolvers, complex permission trees, etc.
	//
	// Each entry produces a single included file. The mount path is fixed at
	// /etc/nats-extra/<name> and the rendered nats.conf gets a corresponding
	// `include "/etc/nats-extra/<name>";` line in slice order.
	// +optional
	// +listType=map
	// +listMapKey=name
	Includes []ConfigInclude `json:"includes,omitempty"`
}

// ConfigInclude references a user-managed Secret or ConfigMap key whose
// content is included verbatim into nats.conf via the native `include`
// directive. Exactly one of secret or configMap must be set.
// +kubebuilder:validation:XValidation:rule="has(self.secret) != has(self.configMap)",message="exactly one of secret or configMap must be set"
type ConfigInclude struct {
	// name is the include filename. Must be unique within the includes list
	// and is used both as the file name under /etc/nats-extra/ and as the
	// VolumeMount name. Conventionally ends in .conf.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9._-]+$`
	Name string `json:"name"`

	// secret selects a key in a Secret in the same namespace.
	// +optional
	Secret *corev1.SecretKeySelector `json:"secret,omitempty"`

	// configMap selects a key in a ConfigMap in the same namespace.
	// +optional
	ConfigMap *corev1.ConfigMapKeySelector `json:"configMap,omitempty"`
}

// ClusterConfig describes how the NATS cluster routing block is rendered when
// the cluster is operating in multi-replica mode. There is no `enabled` field:
// clustering is automatically enabled iff Spec.Replicas > 1.
type ClusterConfig struct {
	// port is the cluster route listener port. Defaults to 6222.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=6222
	// +optional
	Port int32 `json:"port,omitempty"`
	// noAdvertise hides cluster route addresses from clients. Defaults to true.
	// +kubebuilder:default=true
	// +optional
	NoAdvertise *bool `json:"noAdvertise,omitempty"`
	// +optional
	RouteURLs RouteURLsConfig `json:"routeURLs,omitzero"`
	// +optional
	TLS TLSBlock `json:"tls,omitzero"`
}

// RouteURLsConfig controls how the cluster route URLs are constructed.
// Route hosts are always emitted as fully-qualified
// `<pod>.<headless>.<ns>.svc.<cluster-domain>` names to keep resolution
// deterministic across glibc / musl / Go-net resolvers.
type RouteURLsConfig struct {
	// authSecretRef references a Secret holding the cluster route user/password.
	// The Secret must contain `user` and `password` keys. When set, the operator
	// adds the credentials to route URLs and the cluster authorization block.
	// +optional
	AuthSecretRef *corev1.LocalObjectReference `json:"authSecretRef,omitempty"`
	// k8sClusterDomain overrides the cluster DNS suffix appended to every
	// route URL. Defaults to cluster.local, which covers every stock
	// Kubernetes install — only change it when the cluster was brought up
	// with a custom --cluster-domain.
	// +kubebuilder:default=cluster.local
	// +optional
	K8sClusterDomain string `json:"k8sClusterDomain,omitempty"`
}

// JetStreamConfig mirrors `config.jetstream`.
type JetStreamConfig struct {
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// +optional
	FileStore FileStoreConfig `json:"fileStore,omitzero"`
	// +optional
	MemoryStore MemoryStoreConfig `json:"memoryStore,omitzero"`
}

// FileStoreConfig describes the JetStream file store. The on-disk path is
// fixed at /data and not user-configurable.
type FileStoreConfig struct {
	// enabled defaults to true when JetStream is enabled.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
	// pvc controls the JetStream volume claim template.
	// +optional
	PVC PVCConfig `json:"pvc,omitzero"`
	// maxSize bounds the file store. Defaults to the PVC size.
	// +optional
	MaxSize *resource.Quantity `json:"maxSize,omitempty"`
}

// MemoryStoreConfig describes the JetStream in-memory store.
// +kubebuilder:validation:XValidation:rule="!self.enabled || has(self.maxSize)",message="maxSize is required when memory store is enabled"
type MemoryStoreConfig struct {
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// maxSize must fit within the container memory limit.
	// +optional
	MaxSize *resource.Quantity `json:"maxSize,omitempty"`
}

// PVCConfig describes a volume claim template for jetstream / resolver storage.
// The standard corev1.PersistentVolumeClaimSpec is embedded — set storage size
// via spec.resources.requests.storage like a regular PVC.
type PVCConfig struct {
	// enabled, when explicitly set to false, falls back to an emptyDir volume.
	// Defaults to true.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
	// PersistentVolumeClaimSpec is the standard PVC spec. Use the resources,
	// storageClassName, accessModes, dataSource(Ref), volumeMode, etc. fields
	// just as you would on a free-standing PVC.
	// +optional
	corev1.PersistentVolumeClaimSpec `json:",inline"`
}

// NatsListenerConfig mirrors `config.nats` — the client listener.
type NatsListenerConfig struct {
	// port is the client listener port. Defaults to 4222.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=4222
	// +optional
	Port int32 `json:"port,omitempty"`
	// +optional
	TLS TLSBlock `json:"tls,omitzero"`
}

// SimpleListenerConfig is a minimal enabled+port block (e.g. profiling).
type SimpleListenerConfig struct {
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// +optional
	// +kubebuilder:validation:Minimum=1
	Port int32 `json:"port,omitempty"`
}

// ListenerConfig is the standard listener block (enabled, port, tls)
// shared by leafnodes / mqtt / gateway. Free-form per-listener config goes
// through Config.Includes.
type ListenerConfig struct {
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// +optional
	// +kubebuilder:validation:Minimum=1
	Port int32 `json:"port,omitempty"`
	// +optional
	TLS TLSBlock `json:"tls,omitzero"`
}

// WebSocketConfig mirrors `config.websocket` (listener + ingress).
type WebSocketConfig struct {
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// port is the websocket listener port. Defaults to 8080.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=8080
	// +optional
	Port int32 `json:"port,omitempty"`
	// +optional
	TLS TLSBlock `json:"tls,omitzero"`
	// +optional
	Ingress WebSocketIngress `json:"ingress,omitzero"`
}

// +kubebuilder:validation:XValidation:rule="!self.enabled || size(self.hosts) > 0",message="hosts must be non-empty when ingress is enabled"
type WebSocketIngress struct {
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// hosts must contain at least one entry to actually create the Ingress.
	// +optional
	Hosts []string `json:"hosts,omitempty"`
	// +optional
	Path string `json:"path,omitempty"`
	// +optional
	PathType string `json:"pathType,omitempty"`
	// +optional
	ClassName string `json:"className,omitempty"`
	// tlsSecretName enables TLS for every host on the Ingress.
	// +optional
	TLSSecretName string `json:"tlsSecretName,omitempty"`
	// annotations are added to the generated Ingress.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// MonitorConfig mirrors `config.monitor`. Defaults to enabled on port 8222.
// +kubebuilder:validation:XValidation:rule="!self.tlsEnabled || (has(self.enabled) && self.enabled)",message="tlsEnabled requires monitor to be enabled"
type MonitorConfig struct {
	// enabled defaults to true.
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
	// port is the monitor listener port. Defaults to 8222.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=8222
	// +optional
	Port int32 `json:"port,omitempty"`
	// tlsEnabled switches the monitor port to HTTPS using the nats listener TLS.
	// Requires Config.Nats.TLS.Enabled to be true. When set together with
	// PromExporter.Enabled, PromExporter.MonitorDomain must be set to a
	// CN/SAN of the nats TLS certificate.
	// +optional
	TLSEnabled bool `json:"tlsEnabled,omitempty"`
}

// TLSBlock is the standard tls config block reused throughout the listener
// types. The mount path is picked by the operator; users only supply the
// secret name and (optionally) the key names if they differ from the
// kubernetes.io/tls defaults.
type TLSBlock struct {
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// secretName mounts an existing TLS secret.
	// +optional
	SecretName string `json:"secretName,omitempty"`
	// cert is the certificate file name within the secret. Defaults to tls.crt.
	// +kubebuilder:default=tls.crt
	// +optional
	Cert string `json:"cert,omitempty"`
	// key is the private key file name within the secret. Defaults to tls.key.
	// +kubebuilder:default=tls.key
	// +optional
	Key string `json:"key,omitempty"`
	// verify enables mutual TLS — clients must present a certificate.
	// +optional
	Verify *bool `json:"verify,omitempty"`
	// timeout is the TLS handshake timeout in seconds.
	// +optional
	Timeout *int32 `json:"timeout,omitempty"`
}

// ContainerSpec describes the per-nats-container knobs. The image lives at
// the spec top level since almost every user sets it.
type ContainerSpec struct {
	// env is the list of environment variables for the nats container.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`
	// envFrom is the standard list of envFrom sources.
	// +optional
	EnvFrom []corev1.EnvFromSource `json:"envFrom,omitempty"`
	// resources sets the nats container resource requests/limits.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitzero"`
	// securityContext sets the nats container security context.
	// +optional
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`
	// livenessProbe overrides the default liveness probe.
	// +optional
	LivenessProbe *corev1.Probe `json:"livenessProbe,omitempty"`
	// readinessProbe overrides the default readiness probe.
	// +optional
	ReadinessProbe *corev1.Probe `json:"readinessProbe,omitempty"`
	// startupProbe overrides the default startup probe.
	// +optional
	StartupProbe *corev1.Probe `json:"startupProbe,omitempty"`
}

// ImageSpec describes a container image. Repository accepts a full image
// path including registry and (optionally) digest — for example
// "registry.example.com/library/nats" or "nats@sha256:...". The chart's
// separate registry / digest / fullImageName fields are not modeled here
// because they are alternate spellings of the same value.
type ImageSpec struct {
	// +optional
	Repository string `json:"repository,omitempty"`
	// +optional
	Tag string `json:"tag,omitempty"`
	// +optional
	PullPolicy corev1.PullPolicy `json:"pullPolicy,omitempty"`
}

// ReloaderSpec describes the nats config reloader sidecar container. The
// volume mounts forwarded into the sidecar are computed by the operator.
type ReloaderSpec struct {
	// enabled defaults to true.
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
	// +optional
	Image ImageSpec `json:"image,omitzero"`
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`
	// resources sets the reloader container resource requests/limits.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitzero"`
	// securityContext sets the reloader container security context.
	// +optional
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`
}

// PromExporterSpec describes the prometheus nats exporter sidecar container.
// +kubebuilder:validation:XValidation:rule="!self.podMonitor.enabled || self.enabled",message="promExporter must be enabled when podMonitor is enabled"
type PromExporterSpec struct {
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// +optional
	Image ImageSpec `json:"image,omitzero"`
	// port is the exporter listener port. Defaults to 7777.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=7777
	// +optional
	Port int32 `json:"port,omitempty"`
	// monitorDomain must match a CN/SAN on the nats TLS cert when monitor TLS
	// is enabled.
	// +optional
	MonitorDomain string `json:"monitorDomain,omitempty"`
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`
	// resources sets the exporter container resource requests/limits.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitzero"`
	// securityContext sets the exporter container security context.
	// +optional
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`
	// +optional
	PodMonitor PodMonitorSpec `json:"podMonitor,omitzero"`
}

// PodMonitorSpec describes the prometheus PodMonitor for the exporter.
type PodMonitorSpec struct {
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// labels are added to the generated PodMonitor.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// interval is the prometheus scrape interval.
	// +optional
	Interval string `json:"interval,omitempty"`
	// scrapeTimeout is the prometheus scrape timeout.
	// +optional
	ScrapeTimeout string `json:"scrapeTimeout,omitempty"`
}

// ServiceSpec describes the client-facing Service. The operator decides which
// listener ports to publish: nats is always exposed, leafnodes/websocket/mqtt/
// gateway are exposed iff enabled in Config, and cluster/monitor/profiling are
// kept off the client Service (cluster is internal-only via the headless
// Service; monitor and profiling are scraped via PodMonitor or the headless
// Service). Set NodePorts to assign stable NodePort numbers.
type ServiceSpec struct {
	// enabled defaults to true.
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
	// nodePorts assigns a stable NodePort number to a listener, keyed by
	// listener name (nats, leafnodes, websocket, mqtt, gateway). Only
	// meaningful when Type is NodePort or LoadBalancer. Listeners not present
	// in this map get a NodePort allocated by the apiserver.
	// +optional
	NodePorts map[string]int32 `json:"nodePorts,omitempty"`
	// type is the Service type. Defaults to ClusterIP.
	// +optional
	Type corev1.ServiceType `json:"type,omitempty"`
	// loadBalancerClass is the LoadBalancer class for type=LoadBalancer.
	// +optional
	LoadBalancerClass *string `json:"loadBalancerClass,omitempty"`
	// externalTrafficPolicy is the externalTrafficPolicy for type=LoadBalancer/NodePort.
	// +optional
	ExternalTrafficPolicy corev1.ServiceExternalTrafficPolicy `json:"externalTrafficPolicy,omitempty"`
	// annotations are added to the generated Service.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// labels are added to the generated Service.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
}

// StatefulSetSpec describes the underlying StatefulSet.
type StatefulSetSpec struct {
	// annotations are added to the generated StatefulSet.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// labels are added to the generated StatefulSet.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// podManagementPolicy overrides the default Parallel pod management policy.
	// +optional
	PodManagementPolicy appsv1.PodManagementPolicyType `json:"podManagementPolicy,omitempty"`
	// minReadySeconds is the standard StatefulSet minReadySeconds field.
	// +optional
	MinReadySeconds *int32 `json:"minReadySeconds,omitempty"`
}

// PodTemplateSpec describes the StatefulSet pod template.
type PodTemplateSpec struct {
	// configChecksumAnnotation rolls the StatefulSet on config changes by
	// stamping a hash on the pod spec instead of relying on the reloader.
	// +optional
	ConfigChecksumAnnotation bool `json:"configChecksumAnnotation,omitempty"`

	// annotations are added to the rendered pod template.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// labels are added to the rendered pod template.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// nodeSelector is the standard pod nodeSelector.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// tolerations is the standard pod tolerations list.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// affinity is the standard pod affinity rules.
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// topologySpreadConstraints is the standard pod topologySpreadConstraints
	// list. The labelSelector field is overwritten by the operator at reconcile
	// time to match the StatefulSet pods, so any user-supplied selector is
	// ignored — set the rest of the constraint and leave labelSelector nil.
	// +optional
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`

	// priorityClassName is the standard pod priorityClassName.
	// +optional
	PriorityClassName string `json:"priorityClassName,omitempty"`
	// runtimeClassName is the standard pod runtimeClassName.
	// +optional
	RuntimeClassName *string `json:"runtimeClassName,omitempty"`
	// terminationGracePeriodSeconds overrides the default termination grace period.
	// +optional
	TerminationGracePeriodSeconds *int64 `json:"terminationGracePeriodSeconds,omitempty"`
	// dnsPolicy is the standard pod dnsPolicy.
	// +optional
	DNSPolicy corev1.DNSPolicy `json:"dnsPolicy,omitempty"`
	// dnsConfig is the standard pod dnsConfig.
	// +optional
	DNSConfig *corev1.PodDNSConfig `json:"dnsConfig,omitempty"`
	// hostAliases is the standard pod hostAliases list.
	// +optional
	HostAliases []corev1.HostAlias `json:"hostAliases,omitempty"`
	// securityContext sets the pod-level security context.
	// +optional
	SecurityContext *corev1.PodSecurityContext `json:"securityContext,omitempty"`
	// imagePullSecrets is added on top of global.imagePullSecrets.
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
}

// HeadlessServiceSpec describes the headless Service used for pod DNS.
type HeadlessServiceSpec struct {
	// annotations are added to the generated headless Service.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// labels are added to the generated headless Service.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
}

// ConfigMapSpec describes the generated nats config ConfigMap, with an
// existingName escape hatch for users who want to manage it themselves.
type ConfigMapSpec struct {
	// existingName, when set, tells the operator to skip generating a config
	// ConfigMap and mount the named one instead. The operator still validates
	// that it exists in the same namespace.
	// +optional
	ExistingName string `json:"existingName,omitempty"`
	// annotations are added to the generated ConfigMap.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// labels are added to the generated ConfigMap.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
}

// PodDisruptionBudgetSpec describes the generated PDB. The standard
// policyv1 spec fields (minAvailable, maxUnavailable, unhealthyPodEvictionPolicy)
// are inlined; the selector field is overwritten by the operator at reconcile
// time to match the StatefulSet pods, so any user-supplied selector is ignored.
// +kubebuilder:validation:XValidation:rule="!(has(self.minAvailable) && has(self.maxUnavailable))",message="minAvailable and maxUnavailable are mutually exclusive"
type PodDisruptionBudgetSpec struct {
	// enabled defaults to true.
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
	// annotations are added to the generated PDB.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// labels are added to the generated PDB.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// PodDisruptionBudgetSpec is the standard policyv1 PDB spec. The selector
	// field is overwritten by the operator and any user-supplied value is ignored.
	// +optional
	policyv1.PodDisruptionBudgetSpec `json:",inline"`
}

// AuthSpec configures how clients authenticate to the NATS cluster.
// In v1alpha1 only the JWT (decentralized) path is modeled — other auth
// modes (token, user/password, NKey) can be added later as peer fields.
type AuthSpec struct {
	// jwt enables NATS decentralized authentication. When set, the operator
	// renders the `operator:`, `system_account:`, `resolver:` and
	// `resolver_preload:` directives into nats.conf from the typed fields
	// below, so users do not have to hand-write them into a Secret and
	// reference them via Config.Includes.
	// +optional
	JWT *JWTAuthSpec `json:"jwt,omitempty"`
}

// JWTAuthSpec describes the decentralized auth tree the server trusts.
// The operator mounts the referenced operator JWT and account JWTs into
// the nats container via a managed Secret and emits an `include` directive
// in nats.conf pointing at the rendered auth fragment.
// +kubebuilder:validation:XValidation:rule="self.accounts.exists(a, a.publicKey == self.systemAccount)",message="systemAccount must match one of accounts[].publicKey"
type JWTAuthSpec struct {
	// operator references a Secret key containing the operator JWT — the
	// root of trust for this cluster. Typically generated with `nsc` and
	// rotated out-of-band. Required.
	// +required
	Operator corev1.SecretKeySelector `json:"operator"`

	// systemAccount is the public key of the account with cluster-admin
	// privileges. Must match one of accounts[].publicKey. NATS account
	// public keys are 56-char base32 strings, so 64 is a comfortable cap
	// that also keeps the CEL cross-check rule's estimated cost bounded.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +required
	SystemAccount string `json:"systemAccount"`

	// accounts is the set of preloaded accounts. Each entry supplies the
	// account's public key, a reference to a Secret containing the signed
	// account JWT, and (optionally) a reference to a user creds Secret
	// that the operator uses to create a NACK `jetstream.nats.io/v1beta2`
	// Account CR for this account.
	//
	// MaxItems caps the list at 64 because apiserver CEL rule cost is
	// estimated quadratically against unbounded lists and the
	// systemAccount cross-check rule below otherwise exceeds the budget.
	// 64 is well above any realistic number of preloaded accounts — the
	// full resolver mode is the right answer once you have more.
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=64
	// +required
	Accounts []JWTAccount `json:"accounts"`

	// resolver configures how nats-server stores and looks up account JWTs
	// at runtime.
	// +optional
	Resolver JWTResolverSpec `json:"resolver,omitzero"`
}

// JWTAccount is a single preloaded account entry.
type JWTAccount struct {
	// name is a human-readable handle for this account. Used as the NACK
	// Account CR name suffix (`<natscluster-name>-<name>`). Must be a
	// DNS label.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`

	// publicKey is the account's public key (`nsc` account identifier).
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	PublicKey string `json:"publicKey"`

	// jwt references a Secret key containing the account JWT signed by the
	// operator. Required.
	// +required
	JWT corev1.SecretKeySelector `json:"jwt"`

	// userCreds, when set, tells the operator to create a NACK
	// `jetstream.nats.io/v1beta2` Account CR for this account pointing at
	// the referenced user credentials Secret. That Account CR can then be
	// referenced by NACK Stream / Consumer / KV / ObjectStore CRs via
	// their `account: <natscluster-name>-<name>` field, so users do not
	// have to repeat URLs or credentials on every JetStream resource.
	//
	// The operator does not generate the user creds — they come from
	// `nsc generate creds` and live in a user-managed Secret.
	// +optional
	UserCreds *corev1.SecretKeySelector `json:"userCreds,omitempty"`
}

// JWTResolverType selects how nats-server stores account JWTs at runtime.
// +kubebuilder:validation:Enum=memory;full
type JWTResolverType string

const (
	// JWTResolverMemory serves only the preloaded accounts. Accounts are
	// static for the lifetime of each pod — changes require a config
	// rewrite (which the operator handles on NatsCluster edits).
	JWTResolverMemory JWTResolverType = "memory"

	// JWTResolverFull backs accounts with on-disk storage and allows the
	// system account to push new/updated accounts at runtime without a
	// config rewrite. Requires a PVC template via `storage`.
	JWTResolverFull JWTResolverType = "full"
)

// JWTResolverSpec configures the runtime resolver behavior.
// +kubebuilder:validation:XValidation:rule="self.type != 'full' || has(self.storage)",message="storage is required when resolver.type is 'full'"
type JWTResolverSpec struct {
	// type selects the resolver mode. Defaults to "memory".
	// +kubebuilder:default=memory
	// +optional
	Type JWTResolverType `json:"type,omitempty"`

	// storage is the PVC template used when type=full. The operator
	// mounts the volume at /data/resolver. Ignored for type=memory.
	// +optional
	Storage *corev1.PersistentVolumeClaimSpec `json:"storage,omitempty"`

	// allowDelete enables runtime account deletion via the system account.
	// Only honored when type=full. Defaults to false.
	// +optional
	AllowDelete *bool `json:"allowDelete,omitempty"`

	// interval is how often a `full` resolver checks for account updates.
	// Parseable by NATS server (e.g. "2m"). Only honored when type=full.
	// +optional
	Interval string `json:"interval,omitempty"`
}

// ServiceAccountSpec describes the generated ServiceAccount.
type ServiceAccountSpec struct {
	// enabled defaults to false. When false the StatefulSet uses the namespace
	// default ServiceAccount.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
	// annotations are added to the generated ServiceAccount.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// imagePullSecrets is the standard ServiceAccount imagePullSecrets list.
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
}

// NatsClusterEndpoints exposes the canonical connection URLs the operator
// generates for a NatsCluster. Consumers (NACK wrapper CRs, external apps)
// read these instead of reconstructing them from the Service name pattern.
type NatsClusterEndpoints struct {
	// client is the URL of the client-facing Service.
	// +optional
	Client string `json:"client,omitempty"`
	// headless is the URL of the headless Service used for pod DNS / cluster
	// routing. Useful when callers need to bypass the client Service.
	// +optional
	Headless string `json:"headless,omitempty"`
}

// NatsClusterStatus defines the observed state of NatsCluster.
type NatsClusterStatus struct {
	// observedGeneration is the .metadata.generation last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// replicas is the total number of nats pods belonging to the StatefulSet.
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// readyReplicas is the number of nats pods reported ready by the StatefulSet.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// configMapName is the ConfigMap currently mounted as /etc/nats-config.
	// +optional
	ConfigMapName string `json:"configMapName,omitempty"`

	// endpoints exposes the canonical connection URLs the operator generates
	// for this NatsCluster. NACK wrapper CRs and external clients use these
	// instead of guessing the Service name pattern.
	// +optional
	Endpoints NatsClusterEndpoints `json:"endpoints,omitzero"`

	// conditions represent the current state of the NatsCluster resource.
	//
	// Standard condition types include:
	// - "Available": the cluster is fully functional
	// - "Progressing": the cluster is being created or updated
	// - "Degraded": the cluster failed to reach or maintain its desired state
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=nc
// +kubebuilder:printcolumn:name="Replicas",type=integer,JSONPath=`.status.replicas`
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=`.status.readyReplicas`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// NatsCluster is the Schema for the natsclusters API
type NatsCluster struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of NatsCluster
	// +required
	Spec NatsClusterSpec `json:"spec"`

	// status defines the observed state of NatsCluster
	// +optional
	Status NatsClusterStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// NatsClusterList contains a list of NatsCluster
type NatsClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []NatsCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NatsCluster{}, &NatsClusterList{})
}
