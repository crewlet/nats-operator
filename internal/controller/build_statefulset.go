/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	natsv1alpha1 "github.com/crewlet/nats-operator/api/v1alpha1"
)

// buildStatefulSet returns the StatefulSet that runs the nats pods. The
// rendered config bytes are passed in so the builder can stamp a checksum
// annotation on the pod template when the user opts into that rollout
// strategy (rolling restart on every config change instead of relying on
// the reloader sidecar for hot reload).
func buildStatefulSet(cr *natsv1alpha1.NatsCluster, spec *natsv1alpha1.NatsClusterSpec, rendered []byte) *appsv1.StatefulSet {
	podLabels := mergeUserLabels(commonLabels(cr), spec.PodTemplate.Labels)
	podAnnotations := spec.PodTemplate.Annotations
	if spec.PodTemplate.ConfigChecksumAnnotation {
		sum := sha256.Sum256(rendered)
		podAnnotations = mergeAnnotations(podAnnotations, map[string]string{
			"nats.crewlet.cloud/config-hash": hex.EncodeToString(sum[:]),
		})
	}

	pmp := spec.StatefulSet.PodManagementPolicy
	if pmp == "" {
		pmp = appsv1.ParallelPodManagement
	}

	return &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "StatefulSet"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        statefulSetName(cr),
			Namespace:   cr.Namespace,
			Labels:      mergeUserLabels(commonLabels(cr), spec.StatefulSet.Labels),
			Annotations: spec.StatefulSet.Annotations,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:            spec.Replicas,
			ServiceName:         headlessServiceName(cr),
			PodManagementPolicy: pmp,
			MinReadySeconds:     ptrDeref(spec.StatefulSet.MinReadySeconds),
			Selector:            &metav1.LabelSelector{MatchLabels: selectorLabels(cr)},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      podLabels,
					Annotations: podAnnotations,
				},
				Spec: buildPodSpec(cr, spec),
			},
			VolumeClaimTemplates: buildVolumeClaimTemplates(cr, spec),
		},
	}
}

func buildPodSpec(cr *natsv1alpha1.NatsCluster, spec *natsv1alpha1.NatsClusterSpec) corev1.PodSpec {
	volumes := buildVolumes(cr, spec)
	natsMounts := natsContainerMounts(spec)

	containers := []corev1.Container{
		buildNatsContainer(spec, natsMounts),
	}
	if isTrue(spec.Reloader.Enabled) {
		containers = append(containers, buildReloaderContainer(spec, natsMounts))
	}
	if spec.PromExporter.Enabled {
		containers = append(containers, buildExporterContainer(spec))
	}

	tsc := overrideTopologySelector(cr, spec.PodTemplate.TopologySpreadConstraints)

	return corev1.PodSpec{
		ServiceAccountName:            podServiceAccountName(cr, spec),
		ImagePullSecrets:              mergePullSecrets(spec),
		NodeSelector:                  spec.PodTemplate.NodeSelector,
		Tolerations:                   spec.PodTemplate.Tolerations,
		Affinity:                      spec.PodTemplate.Affinity,
		TopologySpreadConstraints:     tsc,
		PriorityClassName:             spec.PodTemplate.PriorityClassName,
		RuntimeClassName:              spec.PodTemplate.RuntimeClassName,
		TerminationGracePeriodSeconds: spec.PodTemplate.TerminationGracePeriodSeconds,
		DNSPolicy:                     spec.PodTemplate.DNSPolicy,
		DNSConfig:                     spec.PodTemplate.DNSConfig,
		HostAliases:                   spec.PodTemplate.HostAliases,
		SecurityContext:               spec.PodTemplate.SecurityContext,
		Volumes:                       volumes,
		Containers:                    containers,
	}
}

// ----- containers -----

func buildNatsContainer(spec *natsv1alpha1.NatsClusterSpec, mounts []corev1.VolumeMount) corev1.Container {
	c := corev1.Container{
		Name:            "nats",
		Image:           imageRef(spec.Image),
		ImagePullPolicy: spec.Image.PullPolicy,
		Args:            []string{"--config", mountPathConfig + "/" + natsConfFileName},
		Ports:           natsContainerPorts(spec),
		Env:             natsContainerEnv(spec),
		EnvFrom:         spec.Container.EnvFrom,
		Resources:       spec.Container.Resources,
		SecurityContext: spec.Container.SecurityContext,
		VolumeMounts:    mounts,
		LivenessProbe:   spec.Container.LivenessProbe,
		ReadinessProbe:  spec.Container.ReadinessProbe,
		StartupProbe:    spec.Container.StartupProbe,
	}
	// Apply default health probes only when the user hasn't supplied their own.
	//
	// Liveness uses js-enabled-only — the lightest /healthz variant that
	// just confirms the server process is responding and JetStream is
	// enabled. Crucially, it does NOT depend on cluster meta leadership,
	// so a transient loss of quorum won't cause kubelet to restart every
	// pod and turn a blip into an outage.
	//
	// Readiness uses js-server-only — checks that JetStream is running on
	// THIS pod. Slightly stricter than liveness so we stop routing traffic
	// to a pod whose JetStream subsystem has died, but still doesn't gate
	// on cluster-wide state.
	//
	// Startup uses js-enabled-only with a generous failureThreshold. Full
	// bootstrap (routes established → meta leader elected) can take 10–30s
	// on a cold boot, and some of that time is spent on DNS propagation
	// that we have no control over. A 5s*30 = 150s grace window makes the
	// container robust to that startup tail without letting genuinely
	// broken pods hang forever.
	if c.LivenessProbe == nil {
		c.LivenessProbe = defaultHealthProbe(spec, "/healthz?js-enabled-only=true", 30, 3)
	}
	if c.ReadinessProbe == nil {
		c.ReadinessProbe = defaultHealthProbe(spec, "/healthz?js-server-only=true", 5, 3)
	}
	if c.StartupProbe == nil {
		c.StartupProbe = defaultHealthProbe(spec, "/healthz?js-enabled-only=true", 5, 30)
	}
	return c
}

func buildReloaderContainer(spec *natsv1alpha1.NatsClusterSpec, natsMounts []corev1.VolumeMount) corev1.Container {
	// The reloader watches the same config and TLS files the nats container
	// reads, so we forward the same mounts under /etc.
	mounts := make([]corev1.VolumeMount, 0, len(natsMounts))
	for _, m := range natsMounts {
		if len(m.MountPath) >= 5 && m.MountPath[:5] == "/etc/" {
			mounts = append(mounts, m)
		}
	}
	return corev1.Container{
		Name:            "reloader",
		Image:           imageRef(spec.Reloader.Image),
		ImagePullPolicy: spec.Reloader.Image.PullPolicy,
		Args: []string{
			"-pid", "/var/run/nats/nats.pid",
			"-config", mountPathConfig + "/" + natsConfFileName,
		},
		Env:             spec.Reloader.Env,
		Resources:       spec.Reloader.Resources,
		SecurityContext: spec.Reloader.SecurityContext,
		VolumeMounts:    mounts,
	}
}

func buildExporterContainer(spec *natsv1alpha1.NatsClusterSpec) corev1.Container {
	scheme := "http"
	if spec.Config.Monitor.TLSEnabled {
		scheme = "https"
	}
	monitorURL := fmt.Sprintf("%s://localhost:%d/", scheme, spec.Config.Monitor.Port)
	return corev1.Container{
		Name:            "metrics",
		Image:           imageRef(spec.PromExporter.Image),
		ImagePullPolicy: spec.PromExporter.Image.PullPolicy,
		Args: []string{
			"-port", fmt.Sprintf("%d", spec.PromExporter.Port),
			"-connz", "-routez", "-subz", "-varz", "-prefix", "nats",
			"-use_internal_server_id",
			monitorURL,
		},
		Ports: []corev1.ContainerPort{
			{Name: "metrics", ContainerPort: spec.PromExporter.Port, Protocol: corev1.ProtocolTCP},
		},
		Env:             spec.PromExporter.Env,
		Resources:       spec.PromExporter.Resources,
		SecurityContext: spec.PromExporter.SecurityContext,
	}
}

// ----- container ports / env / probes -----

func natsContainerPorts(spec *natsv1alpha1.NatsClusterSpec) []corev1.ContainerPort {
	ports := []corev1.ContainerPort{
		{Name: "nats", ContainerPort: spec.Config.Nats.Port, Protocol: corev1.ProtocolTCP},
	}
	if spec.Replicas != nil && *spec.Replicas > 1 {
		ports = append(ports, corev1.ContainerPort{
			Name: "cluster", ContainerPort: spec.Config.Cluster.Port, Protocol: corev1.ProtocolTCP,
		})
	}
	if isTrue(spec.Config.Monitor.Enabled) {
		ports = append(ports, corev1.ContainerPort{
			Name: "monitor", ContainerPort: spec.Config.Monitor.Port, Protocol: corev1.ProtocolTCP,
		})
	}
	if spec.Config.LeafNodes.Enabled {
		ports = append(ports, corev1.ContainerPort{
			Name: "leafnodes", ContainerPort: spec.Config.LeafNodes.Port, Protocol: corev1.ProtocolTCP,
		})
	}
	if spec.Config.WebSocket.Enabled {
		ports = append(ports, corev1.ContainerPort{
			Name: "websocket", ContainerPort: spec.Config.WebSocket.Port, Protocol: corev1.ProtocolTCP,
		})
	}
	if spec.Config.MQTT.Enabled {
		ports = append(ports, corev1.ContainerPort{
			Name: "mqtt", ContainerPort: spec.Config.MQTT.Port, Protocol: corev1.ProtocolTCP,
		})
	}
	if spec.Config.Gateway.Enabled {
		ports = append(ports, corev1.ContainerPort{
			Name: "gateway", ContainerPort: spec.Config.Gateway.Port, Protocol: corev1.ProtocolTCP,
		})
	}
	if spec.Config.Profiling.Enabled {
		ports = append(ports, corev1.ContainerPort{
			Name: "profiling", ContainerPort: spec.Config.Profiling.Port, Protocol: corev1.ProtocolTCP,
		})
	}
	return ports
}

func natsContainerEnv(spec *natsv1alpha1.NatsClusterSpec) []corev1.EnvVar {
	env := []corev1.EnvVar{
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
			},
		},
		{Name: "SERVER_NAME", Value: "$(POD_NAME)"},
	}
	// Project routes auth secret into env vars referenced by the rendered
	// nats.conf cluster authorization block.
	if ref := spec.Config.Cluster.RouteURLs.AuthSecretRef; ref != nil {
		env = append(env,
			corev1.EnvVar{
				Name: "NATS_ROUTES_USER",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: *ref,
						Key:                  "user",
					},
				},
			},
			corev1.EnvVar{
				Name: "NATS_ROUTES_PASSWORD",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: *ref,
						Key:                  "password",
					},
				},
			},
		)
	}
	env = append(env, spec.Container.Env...)
	return env
}

func defaultHealthProbe(spec *natsv1alpha1.NatsClusterSpec, path string, period, failureThreshold int32) *corev1.Probe {
	if !isTrue(spec.Config.Monitor.Enabled) {
		// Fall back to TCP probe on the client port when the monitor is off.
		return &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt32(spec.Config.Nats.Port),
				},
			},
			PeriodSeconds:    period,
			FailureThreshold: failureThreshold,
		}
	}
	scheme := corev1.URISchemeHTTP
	if spec.Config.Monitor.TLSEnabled {
		scheme = corev1.URISchemeHTTPS
	}
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path:   path,
				Port:   intstr.FromInt32(spec.Config.Monitor.Port),
				Scheme: scheme,
			},
		},
		PeriodSeconds:    period,
		FailureThreshold: failureThreshold,
	}
}

// ----- volumes / mounts / PVC templates -----

func buildVolumes(cr *natsv1alpha1.NatsCluster, spec *natsv1alpha1.NatsClusterSpec) []corev1.Volume {
	vols := []corev1.Volume{
		{
			Name: volumeNameConfig,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: configMapName(cr)},
				},
			},
		},
		{
			Name:         "pid",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		},
	}

	// One volume per Config.Includes entry — Secret or ConfigMap projection.
	for _, inc := range spec.Config.Includes {
		v := corev1.Volume{Name: includeVolumeName(inc.Name)}
		switch {
		case inc.Secret != nil:
			v.VolumeSource = corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: inc.Secret.Name,
					Items: []corev1.KeyToPath{
						{Key: includeKeyOrName(inc.Secret.Key, inc.Name), Path: inc.Name},
					},
				},
			}
		case inc.ConfigMap != nil:
			v.VolumeSource = corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: inc.ConfigMap.Name},
					Items: []corev1.KeyToPath{
						{Key: includeKeyOrName(inc.ConfigMap.Key, inc.Name), Path: inc.Name},
					},
				},
			}
		}
		vols = append(vols, v)
	}

	// Per-listener TLS secret volumes.
	addTLS := func(listener string, tls natsv1alpha1.TLSBlock) {
		if !tls.Enabled || tls.SecretName == "" {
			return
		}
		vols = append(vols, corev1.Volume{
			Name: tlsVolumeName(listener),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{SecretName: tls.SecretName},
			},
		})
	}
	addTLS("nats", spec.Config.Nats.TLS)
	addTLS("cluster", spec.Config.Cluster.TLS)
	addTLS("leafnodes", spec.Config.LeafNodes.TLS)
	addTLS("websocket", spec.Config.WebSocket.TLS)
	addTLS("mqtt", spec.Config.MQTT.TLS)
	addTLS("gateway", spec.Config.Gateway.TLS)

	// Operator-managed auth Secret — mounted at /etc/nats-auth when JWT
	// auth is configured. Contains the rendered auth.conf, operator JWT,
	// and resolver_preload fragment.
	if spec.Auth.JWT != nil {
		vols = append(vols, corev1.Volume{
			Name: volumeNameAuth,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{SecretName: authSecretName(cr)},
			},
		})
	}

	// Global CA bundle (one Secret or ConfigMap projection).
	if spec.TLSCA.Secret != nil {
		vols = append(vols, corev1.Volume{
			Name: volumeNameCA,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{SecretName: spec.TLSCA.Secret.Name},
			},
		})
	} else if spec.TLSCA.ConfigMap != nil {
		vols = append(vols, corev1.Volume{
			Name: volumeNameCA,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: spec.TLSCA.ConfigMap.Name},
				},
			},
		})
	}

	return vols
}

func natsContainerMounts(spec *natsv1alpha1.NatsClusterSpec) []corev1.VolumeMount {
	mounts := []corev1.VolumeMount{
		{Name: volumeNameConfig, MountPath: mountPathConfig},
		{Name: "pid", MountPath: "/var/run/nats"},
	}
	for _, inc := range spec.Config.Includes {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      includeVolumeName(inc.Name),
			MountPath: includeMountPath(inc.Name),
			SubPath:   inc.Name,
			ReadOnly:  true,
		})
	}
	addMount := func(listener string, tls natsv1alpha1.TLSBlock) {
		if !tls.Enabled || tls.SecretName == "" {
			return
		}
		mounts = append(mounts, corev1.VolumeMount{
			Name:      tlsVolumeName(listener),
			MountPath: tlsMountPath(listener),
			ReadOnly:  true,
		})
	}
	addMount("nats", spec.Config.Nats.TLS)
	addMount("cluster", spec.Config.Cluster.TLS)
	addMount("leafnodes", spec.Config.LeafNodes.TLS)
	addMount("websocket", spec.Config.WebSocket.TLS)
	addMount("mqtt", spec.Config.MQTT.TLS)
	addMount("gateway", spec.Config.Gateway.TLS)
	if spec.TLSCA.Secret != nil || spec.TLSCA.ConfigMap != nil {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      volumeNameCA,
			MountPath: mountPathCA,
			ReadOnly:  true,
		})
	}
	if spec.Config.JetStream.Enabled && jetstreamUsesPVC(spec) {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      pvcVolumeNameJetStream,
			MountPath: mountPathData,
		})
	}
	if jwtResolverUsesPVC(spec) {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      pvcVolumeNameResolver,
			MountPath: mountPathResolver,
		})
	}
	if spec.Auth.JWT != nil {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      volumeNameAuth,
			MountPath: mountPathAuth,
			ReadOnly:  true,
		})
	}
	return mounts
}

func buildVolumeClaimTemplates(cr *natsv1alpha1.NatsCluster, spec *natsv1alpha1.NatsClusterSpec) []corev1.PersistentVolumeClaim {
	var pvcs []corev1.PersistentVolumeClaim
	if spec.Config.JetStream.Enabled && jetstreamUsesPVC(spec) {
		pvcs = append(pvcs, pvcTemplate(cr, pvcVolumeNameJetStream, spec.Config.JetStream.FileStore.PVC))
	}
	if jwtResolverUsesPVC(spec) {
		// auth.jwt.resolver.type=full stores accounts on disk; the PVC
		// template comes from auth.jwt.resolver.storage. Same labeling
		// rationale as the JetStream template — managed-by label so
		// cleanup selectors find these volumes.
		pvcs = append(pvcs, corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:   pvcVolumeNameResolver,
				Labels: commonLabels(cr),
			},
			Spec: *spec.Auth.JWT.Resolver.Storage,
		})
	}
	return pvcs
}

func pvcTemplate(cr *natsv1alpha1.NatsCluster, name string, pvc natsv1alpha1.PVCConfig) corev1.PersistentVolumeClaim {
	// Set the canonical labels on the PVC template so the PVCs the
	// StatefulSet creates from it carry our managed-by label. Kubernetes
	// doesn't auto-populate labels on volumeClaimTemplates, and without
	// them any cleanup relying on `-l app.kubernetes.io/managed-by=...`
	// misses the jetstream data volumes — which causes stale JetStream
	// metadata to survive across NatsCluster recreations and prevents
	// the fresh cluster from electing a meta leader.
	return corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: commonLabels(cr),
		},
		Spec: pvc.PersistentVolumeClaimSpec,
	}
}

func jetstreamUsesPVC(spec *natsv1alpha1.NatsClusterSpec) bool {
	return spec.Config.JetStream.FileStore.PVC.Enabled == nil || *spec.Config.JetStream.FileStore.PVC.Enabled
}

// jwtResolverUsesPVC returns true when the JWT auth path is enabled with
// resolver.type=full, which requires on-disk storage for the dynamic
// account JWT store.
func jwtResolverUsesPVC(spec *natsv1alpha1.NatsClusterSpec) bool {
	return spec.Auth.JWT != nil &&
		spec.Auth.JWT.Resolver.Type == natsv1alpha1.JWTResolverFull &&
		spec.Auth.JWT.Resolver.Storage != nil
}

// ----- helpers -----

func imageRef(img natsv1alpha1.ImageSpec) string {
	if img.Tag == "" {
		return img.Repository
	}
	return img.Repository + ":" + img.Tag
}

func tlsVolumeName(listener string) string { return "tls-" + listener }
func includeVolumeName(name string) string { return "include-" + sanitizeVolumeName(name) }
func includeKeyOrName(key, fallback string) string {
	if key != "" {
		return key
	}
	return fallback
}

// sanitizeVolumeName replaces characters not allowed in K8s volume names
// (which must be DNS labels) with '-'.
func sanitizeVolumeName(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-':
			out = append(out, c)
		case c >= 'A' && c <= 'Z':
			out = append(out, c+('a'-'A'))
		default:
			out = append(out, '-')
		}
	}
	return string(out)
}

func mergePullSecrets(spec *natsv1alpha1.NatsClusterSpec) []corev1.LocalObjectReference {
	all := append([]corev1.LocalObjectReference{}, spec.Global.ImagePullSecrets...)
	all = append(all, spec.PodTemplate.ImagePullSecrets...)
	return all
}

// overrideTopologySelector returns a copy of the user-supplied
// TopologySpreadConstraints with each entry's labelSelector overwritten by
// the canonical pod selector. The selector match is the only correctness-
// critical field — getting it wrong silently breaks spread, so the operator
// always wins here.
func overrideTopologySelector(cr *natsv1alpha1.NatsCluster, in []corev1.TopologySpreadConstraint) []corev1.TopologySpreadConstraint {
	if len(in) == 0 {
		return nil
	}
	out := make([]corev1.TopologySpreadConstraint, len(in))
	for i, c := range in {
		c.LabelSelector = &metav1.LabelSelector{MatchLabels: selectorLabels(cr)}
		out[i] = c
	}
	return out
}

func ptrDeref(p *int32) int32 {
	if p == nil {
		return 0
	}
	return *p
}

// podServiceAccountName returns the ServiceAccount name to set on the pod
// template. When ServiceAccount.Enabled is true the operator manages an SA
// named <cr>-sa; otherwise the StatefulSet inherits the namespace default.
func podServiceAccountName(cr *natsv1alpha1.NatsCluster, spec *natsv1alpha1.NatsClusterSpec) string {
	if isTrue(spec.ServiceAccount.Enabled) {
		return serviceAccountName(cr)
	}
	return ""
}
