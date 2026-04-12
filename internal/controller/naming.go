/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"fmt"

	natsv1alpha1 "github.com/crewlet/nats-operator/api/v1alpha1"
)

// Resource name suffixes. Names are derived as <natscluster-name><suffix>
// so two NatsClusters in the same namespace can never produce the same
// resource name.
const (
	suffixConfig   = "-config"
	suffixHeadless = "-headless"
	suffixReloader = "-reloader"
	suffixExporter = "-exporter"
	suffixPDB      = "-pdb"
	suffixSA       = "-sa"
	suffixPodMon   = "-podmonitor"
	suffixIngress  = "-ws-ingress"
)

// Container mount paths. The operator owns these — users do not configure
// where things land. Listener TLS secrets, includes, jetstream storage and
// the CA bundle all use predictable, hardcoded locations so the rendered
// nats.conf can reference them by absolute path.
const (
	mountPathConfig    = "/etc/nats-config"
	mountPathExtra     = "/etc/nats-extra"
	mountPathCA        = "/etc/nats-ca-cert"
	mountPathTLSPrefix = "/etc/nats-certs/" // suffixed with the listener name
	mountPathData      = "/data"
	mountPathResolver  = "/data/resolver"

	// natsConfFileName is the file name inside the config ConfigMap.
	natsConfFileName = "nats.conf"

	// pvcVolumeNameJetStream is the volume claim template name for the
	// jetstream file store. The PVC name K8s actually creates is
	// <pvcVolumeNameJetStream>-<statefulset-name>-<ordinal>.
	pvcVolumeNameJetStream = "jetstream"
	pvcVolumeNameResolver  = "resolver"
)

// Volume names used by the StatefulSet pod template.
const (
	volumeNameConfig = "config"
	volumeNameExtra  = "extra-config"
	volumeNameCA     = "ca-cert"
)

// configMapName returns the ConfigMap name the operator uses for the
// rendered nats.conf. If the user supplied configMap.existingName, that
// value wins (BYO ConfigMap mode); otherwise the default suffix-derived
// name is used.
func configMapName(cr *natsv1alpha1.NatsCluster) string {
	if cr.Spec.ConfigMap.ExistingName != "" {
		return cr.Spec.ConfigMap.ExistingName
	}
	return cr.Name + suffixConfig
}

func headlessServiceName(cr *natsv1alpha1.NatsCluster) string {
	return cr.Name + suffixHeadless
}

func clientServiceName(cr *natsv1alpha1.NatsCluster) string {
	return cr.Name
}

func statefulSetName(cr *natsv1alpha1.NatsCluster) string {
	return cr.Name
}

func pdbName(cr *natsv1alpha1.NatsCluster) string {
	return cr.Name + suffixPDB
}

func serviceAccountName(cr *natsv1alpha1.NatsCluster) string {
	return cr.Name + suffixSA
}

func podMonitorName(cr *natsv1alpha1.NatsCluster) string {
	return cr.Name + suffixPodMon
}

func ingressName(cr *natsv1alpha1.NatsCluster) string {
	return cr.Name + suffixIngress
}

// tlsMountPath returns the mount path for the named listener's TLS secret
// (e.g. /etc/nats-certs/nats, /etc/nats-certs/cluster).
func tlsMountPath(listener string) string {
	return mountPathTLSPrefix + listener
}

// includeMountPath returns the absolute path under /etc/nats-extra for an
// include entry. Used both as the include directive value in nats.conf and
// as the volume mount path in the container.
func includeMountPath(name string) string {
	return mountPathExtra + "/" + name
}

// podFQDN returns the predictable per-pod DNS name used in cluster route
// URLs and JetStream server names.
func podFQDN(cr *natsv1alpha1.NatsCluster, ordinal int32, useFQDN bool, clusterDomain string) string {
	short := fmt.Sprintf("%s-%d.%s.%s.svc",
		statefulSetName(cr), ordinal, headlessServiceName(cr), cr.Namespace)
	if useFQDN {
		return short + "." + clusterDomain
	}
	return short
}

// clusterEndpoints returns the canonical connection URLs the operator
// publishes in NatsCluster.Status.Endpoints. NACK wrapper CRs and external
// clients consume these instead of guessing the Service name pattern.
func clusterEndpoints(cr *natsv1alpha1.NatsCluster, spec *natsv1alpha1.NatsClusterSpec) natsv1alpha1.NatsClusterEndpoints {
	port := spec.Config.Nats.Port
	scheme := "nats"
	if spec.Config.Nats.TLS.Enabled {
		scheme = "tls"
	}
	out := natsv1alpha1.NatsClusterEndpoints{
		Headless: fmt.Sprintf("%s://%s.%s.svc:%d", scheme, headlessServiceName(cr), cr.Namespace, port),
	}
	if isTrue(spec.Service.Enabled) {
		out.Client = fmt.Sprintf("%s://%s.%s.svc:%d", scheme, clientServiceName(cr), cr.Namespace, port)
	}
	return out
}
