/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	corev1 "k8s.io/api/core/v1"

	natsv1alpha1 "github.com/crewlet/nats-operator/api/v1alpha1"
)

// Default container images. Bumping these in a release upgrades every
// NatsCluster on the next reconcile, so they live in code instead of
// +kubebuilder:default markers (which would bake the value into stored CRs).
const (
	defaultNatsImage     = "nats:2.12.6-alpine"
	defaultReloaderImage = "natsio/nats-server-config-reloader:0.21.1"
	defaultExporterImage = "natsio/prometheus-nats-exporter:0.18.0"
)

// Default listener ports — match the upstream nats-io/k8s helm chart.
const (
	defaultNatsPort      int32 = 4222
	defaultClusterPort   int32 = 6222
	defaultMonitorPort   int32 = 8222
	defaultWebSocketPort int32 = 8080
	defaultMQTTPort      int32 = 1883
	defaultLeafNodesPort int32 = 7422
	defaultGatewayPort   int32 = 7222
	defaultProfilingPort int32 = 65432
	defaultExporterPort  int32 = 7777
)

// Default file names within tls secrets — the kubernetes.io/tls convention.
const (
	defaultTLSCertFile = "tls.crt"
	defaultTLSKeyFile  = "tls.key"
	defaultCAKey       = "ca.crt"
)

const (
	defaultClusterDomain = "cluster.local"
)

// defaulted returns a copy of spec with controller-side defaults applied.
// Reconcile and the resource builders work on the defaulted copy so they do
// not need to repeat nil checks or fallback expressions everywhere. The
// original CR object is never mutated.
func defaulted(in *natsv1alpha1.NatsClusterSpec) natsv1alpha1.NatsClusterSpec {
	out := in.DeepCopy()

	if out.Replicas == nil {
		out.Replicas = ptr(int32(1))
	}

	defaultImage(&out.Image, defaultNatsImage)

	// Container — no nested defaults beyond what corev1 already provides.

	// Reloader.
	if out.Reloader.Enabled == nil {
		out.Reloader.Enabled = ptr(true)
	}
	defaultImage(&out.Reloader.Image, defaultReloaderImage)

	// PromExporter.
	defaultImage(&out.PromExporter.Image, defaultExporterImage)
	if out.PromExporter.Port == 0 {
		out.PromExporter.Port = defaultExporterPort
	}

	// Service.
	if out.Service.Enabled == nil {
		out.Service.Enabled = ptr(true)
	}
	if out.Service.Type == "" {
		out.Service.Type = corev1.ServiceTypeClusterIP
	}

	// Config — listener ports and the listener-specific defaults.
	defaultConfig(&out.Config)

	// PodDisruptionBudget.
	if out.PodDisruptionBudget.Enabled == nil {
		out.PodDisruptionBudget.Enabled = ptr(true)
	}

	return *out
}

func defaultConfig(c *natsv1alpha1.NatsConfigSpec) {
	// Cluster (used only when Replicas > 1).
	if c.Cluster.Port == 0 {
		c.Cluster.Port = defaultClusterPort
	}
	if c.Cluster.NoAdvertise == nil {
		c.Cluster.NoAdvertise = ptr(true)
	}
	if c.Cluster.RouteURLs.K8sClusterDomain == "" {
		c.Cluster.RouteURLs.K8sClusterDomain = defaultClusterDomain
	}
	defaultTLS(&c.Cluster.TLS)

	// Nats client listener.
	if c.Nats.Port == 0 {
		c.Nats.Port = defaultNatsPort
	}
	defaultTLS(&c.Nats.TLS)

	// Optional listeners.
	defaultListener(&c.LeafNodes, defaultLeafNodesPort)
	defaultListener(&c.MQTT, defaultMQTTPort)
	defaultListener(&c.Gateway, defaultGatewayPort)
	defaultWebSocket(&c.WebSocket)

	// Monitor — defaults to enabled on 8222.
	if c.Monitor.Enabled == nil {
		c.Monitor.Enabled = ptr(true)
	}
	if c.Monitor.Port == 0 {
		c.Monitor.Port = defaultMonitorPort
	}

	// Profiling.
	if c.Profiling.Enabled && c.Profiling.Port == 0 {
		c.Profiling.Port = defaultProfilingPort
	}
}

func defaultListener(l *natsv1alpha1.ListenerConfig, port int32) {
	if !l.Enabled {
		return
	}
	if l.Port == 0 {
		l.Port = port
	}
	defaultTLS(&l.TLS)
}

func defaultWebSocket(w *natsv1alpha1.WebSocketConfig) {
	if !w.Enabled {
		return
	}
	if w.Port == 0 {
		w.Port = defaultWebSocketPort
	}
	defaultTLS(&w.TLS)
}

func defaultTLS(t *natsv1alpha1.TLSBlock) {
	if !t.Enabled {
		return
	}
	if t.Cert == "" {
		t.Cert = defaultTLSCertFile
	}
	if t.Key == "" {
		t.Key = defaultTLSKeyFile
	}
}

// defaultImage fills empty fields on i with the operator's defaults.
// Repository defaults to fallback (a full image path including tag); if
// the user has set Repository explicitly, only PullPolicy may be defaulted.
func defaultImage(i *natsv1alpha1.ImageSpec, fallback string) {
	if i.Repository == "" && i.Tag == "" {
		// Split the fallback "repo:tag" into Repository + Tag so the rendered
		// container image follows the same Repository/Tag pattern as user-set
		// images. This makes the merged shape uniform downstream.
		repo, tag := splitImageRef(fallback)
		i.Repository = repo
		if i.Tag == "" {
			i.Tag = tag
		}
	}
	if i.PullPolicy == "" {
		i.PullPolicy = corev1.PullIfNotPresent
	}
}

// splitImageRef splits "registry/repo:tag" into ("registry/repo", "tag").
// Falls back to (ref, "") when no tag is present. Digest references
// ("repo@sha256:...") are returned as-is in the repository part with an empty
// tag, since the digest is the canonical reference.
func splitImageRef(ref string) (repo, tag string) {
	// A digest reference contains '@' — keep it whole as the repo.
	for i := 0; i < len(ref); i++ {
		if ref[i] == '@' {
			return ref, ""
		}
	}
	// Find the last ':' that is not inside a port (registry:port/repo:tag).
	// We split at the last ':' that comes after the last '/'.
	lastSlash := -1
	for i := 0; i < len(ref); i++ {
		if ref[i] == '/' {
			lastSlash = i
		}
	}
	for i := len(ref) - 1; i > lastSlash; i-- {
		if ref[i] == ':' {
			return ref[:i], ref[i+1:]
		}
	}
	return ref, ""
}

func ptr[T any](v T) *T { return &v }
