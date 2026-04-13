/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	natsv1alpha1 "github.com/crewlet/nats-operator/api/v1alpha1"
)

const testMetricsContainerName = "metrics"

// buildTestStatefulSet defaults + renders + builds a StatefulSet,
// returning the rendered bytes too so individual assertions can
// cross-reference what was baked into the pod template.
func buildTestStatefulSet(t *testing.T, cr *natsv1alpha1.NatsCluster) (*appsv1.StatefulSet, []byte) {
	t.Helper()
	spec := defaulted(&cr.Spec)
	rendered := renderNatsConf(cr, &spec)
	sts := buildStatefulSet(cr, &spec, rendered)
	require.NotNil(t, sts, "buildStatefulSet returned nil")
	return sts, rendered
}

func TestBuildStatefulSet_Basics(t *testing.T) {
	cr := newNatsCluster(withReplicas(3), withJetStream())
	sts, _ := buildTestStatefulSet(t, cr)

	assert.Equal(t, statefulSetName(cr), sts.Name)
	require.NotNil(t, sts.Spec.Replicas)
	assert.Equal(t, int32(3), *sts.Spec.Replicas)
	assert.Equal(t, headlessServiceName(cr), sts.Spec.ServiceName)
	assert.Equal(t, appsv1.ParallelPodManagement, sts.Spec.PodManagementPolicy)

	// Selector must exactly match the pod template labels — mismatch
	// makes the StatefulSet reject the update and stuck reconcile.
	for k, v := range sts.Spec.Selector.MatchLabels {
		assert.Equalf(t, v, sts.Spec.Template.Labels[k], "pod template label %q missing", k)
	}
}

func TestBuildStatefulSet_NatsContainerImageAndArgs(t *testing.T) {
	cr := newNatsCluster(withImage("my-registry.io/nats", "2.12.6"))
	sts, _ := buildTestStatefulSet(t, cr)

	nats := findNatsContainer(t, sts)
	assert.Equal(t, "my-registry.io/nats:2.12.6", nats.Image)
	require.GreaterOrEqual(t, len(nats.Args), 2)
	assert.Equal(t, "--config", nats.Args[0])
	assert.Equal(t, mountPathConfig+"/"+natsConfFileName, nats.Args[1])
}

func TestBuildStatefulSet_ReloaderSidecarPresentByDefault(t *testing.T) {
	cr := newNatsCluster()
	sts, _ := buildTestStatefulSet(t, cr)
	seen := false
	for _, c := range sts.Spec.Template.Spec.Containers {
		if c.Name == "reloader" {
			seen = true
		}
	}
	assert.True(t, seen, "reloader sidecar should be present by default")
}

func TestBuildStatefulSet_ReloaderSkippedWhenDisabled(t *testing.T) {
	cr := newNatsCluster()
	cr.Spec.Reloader.Enabled = ptr(false)
	// Without reloader, checksum annotation must be enabled or CEL would
	// reject the spec. Set it so defaulted() doesn't fail downstream.
	cr.Spec.PodTemplate.ConfigChecksumAnnotation = true

	sts, _ := buildTestStatefulSet(t, cr)
	for _, c := range sts.Spec.Template.Spec.Containers {
		assert.NotEqual(t, "reloader", c.Name,
			"reloader sidecar should be absent when explicitly disabled")
	}
}

func TestBuildStatefulSet_MetricsSidecarGatedOnPromExporter(t *testing.T) {
	// Default: no metrics sidecar.
	sts, _ := buildTestStatefulSet(t, newNatsCluster())
	for _, c := range sts.Spec.Template.Spec.Containers {
		assert.NotEqual(t, testMetricsContainerName, c.Name,
			"metrics sidecar should be absent by default")
	}

	// Enabled: metrics container is there, with the metrics port declared.
	cr := newNatsCluster(withPromExporter())
	sts, _ = buildTestStatefulSet(t, cr)
	var metrics *corev1.Container
	for i := range sts.Spec.Template.Spec.Containers {
		c := &sts.Spec.Template.Spec.Containers[i]
		if c.Name == testMetricsContainerName {
			metrics = c
		}
	}
	require.NotNil(t, metrics, "metrics sidecar missing when PromExporter is enabled")
	var hasMetricsPort bool
	for _, p := range metrics.Ports {
		if p.Name == testMetricsContainerName && p.ContainerPort == defaultExporterPort {
			hasMetricsPort = true
		}
	}
	assert.True(t, hasMetricsPort, "metrics sidecar missing the metrics container port")
}

func TestBuildStatefulSet_DownwardAPIEnvVars(t *testing.T) {
	cr := newNatsCluster()
	sts, _ := buildTestStatefulSet(t, cr)

	nats := findNatsContainer(t, sts)
	var podName, serverName bool
	for _, e := range nats.Env {
		if e.Name == "POD_NAME" && e.ValueFrom != nil && e.ValueFrom.FieldRef != nil {
			if e.ValueFrom.FieldRef.FieldPath == "metadata.name" {
				podName = true
			}
		}
		if e.Name == "SERVER_NAME" && e.Value == "$(POD_NAME)" {
			serverName = true
		}
	}
	assert.True(t, podName, "POD_NAME env var not wired from downward API")
	assert.True(t, serverName, "SERVER_NAME env var not wired to $(POD_NAME)")
}

func TestBuildStatefulSet_JetStreamPVCTemplateHasManagedByLabel(t *testing.T) {
	// Regression: PVC templates must carry the managed-by label so
	// cleanup can find them; stale JetStream state on unlabeled PVCs
	// persisted across cluster recreations and broke meta-leader
	// election.
	cr := newNatsCluster(withJetStream())
	sts, _ := buildTestStatefulSet(t, cr)
	require.NotEmpty(t, sts.Spec.VolumeClaimTemplates, "expected a JetStream PVC template")
	pvc := sts.Spec.VolumeClaimTemplates[0]
	assert.Equal(t, pvcVolumeNameJetStream, pvc.Name)
	assert.Equal(t, managedByValue, pvc.Labels[labelAppManaged])
	assert.Equal(t, testName, pvc.Labels[labelSelectorKey])
}

func TestBuildStatefulSet_RoutesUseFQDN_Regression(t *testing.T) {
	// Regression guard: the rendered nats.conf inside the ConfigMap
	// must use fully-qualified hostnames for cluster routes. Short
	// forms resolve inconsistently across musl/glibc/Go resolvers.
	cr := newNatsCluster(withReplicas(3))
	_, rendered := buildTestStatefulSet(t, cr)
	assert.True(t, bytes.Contains(rendered, []byte("svc.cluster.local:6222")),
		"rendered config must use FQDN route URLs")
}

func TestBuildStatefulSet_ConfigChecksumAnnotationStampedOnRequest(t *testing.T) {
	cr := newNatsCluster(withConfigChecksumAnnotation())
	sts, _ := buildTestStatefulSet(t, cr)

	annot := sts.Spec.Template.Annotations["nats.crewlet.cloud/config-hash"]
	require.NotEmpty(t, annot, "config-hash annotation missing from pod template")
	assert.Len(t, annot, 64, "config-hash should be sha256 hex")
}

func TestBuildStatefulSet_ConfigChecksumStable(t *testing.T) {
	// Determinism: building the same spec twice must produce the same
	// hash — otherwise every reconcile rolls the StatefulSet.
	cr := newNatsCluster(withConfigChecksumAnnotation())
	sts1, _ := buildTestStatefulSet(t, cr)
	sts2, _ := buildTestStatefulSet(t, cr)

	assert.Equal(t,
		sts1.Spec.Template.Annotations["nats.crewlet.cloud/config-hash"],
		sts2.Spec.Template.Annotations["nats.crewlet.cloud/config-hash"],
		"config-hash not stable across builds")
}

func TestBuildStatefulSet_TLSVolumeMountForEachListener(t *testing.T) {
	cr := newNatsCluster(withNatsTLS("nats-tls"))
	sts, _ := buildTestStatefulSet(t, cr)
	nats := findNatsContainer(t, sts)

	var mount *corev1.VolumeMount
	for i := range nats.VolumeMounts {
		if nats.VolumeMounts[i].Name == tlsVolumeName(appNameValue) {
			mount = &nats.VolumeMounts[i]
		}
	}
	require.NotNil(t, mount, "tls-nats volume mount missing")
	assert.Equal(t, tlsMountPath(appNameValue), mount.MountPath)
	assert.True(t, mount.ReadOnly, "tls-nats mount should be read-only")
}

func TestBuildStatefulSet_AuthVolumeMountWhenJWTEnabled(t *testing.T) {
	cr := newNatsCluster(withJWTAuth(
		"op-jwt",
		jwtAccount("sys", "AASYS", "sys-jwt"),
	))
	sts, _ := buildTestStatefulSet(t, cr)
	nats := findNatsContainer(t, sts)

	var mount *corev1.VolumeMount
	for i := range nats.VolumeMounts {
		if nats.VolumeMounts[i].Name == volumeNameAuth {
			mount = &nats.VolumeMounts[i]
		}
	}
	require.NotNil(t, mount, "auth volume mount missing when auth.jwt is set")
	assert.Equal(t, mountPathAuth, mount.MountPath)
}

func TestBuildStatefulSet_TopologySpreadSelectorOverwritten(t *testing.T) {
	cr := newNatsCluster()
	cr.Spec.PodTemplate.TopologySpreadConstraints = []corev1.TopologySpreadConstraint{
		{
			MaxSkew:     1,
			TopologyKey: "kubernetes.io/hostname",
			// Hostile user input trying to hijack the selector.
			LabelSelector: nil,
		},
	}
	sts, _ := buildTestStatefulSet(t, cr)

	tsc := sts.Spec.Template.Spec.TopologySpreadConstraints
	require.Len(t, tsc, 1)
	require.NotNil(t, tsc[0].LabelSelector,
		"operator must stamp a labelSelector so the constraint actually targets nats pods")
	// The stamped labelSelector must match the StatefulSet selector.
	for k, v := range selectorLabels(cr) {
		assert.Equalf(t, v, tsc[0].LabelSelector.MatchLabels[k],
			"topology labelSelector missing %q", k)
	}
}

// findNatsContainer returns the main nats container from the built
// StatefulSet pod template, failing the test if it is missing.
func findNatsContainer(t *testing.T, sts *appsv1.StatefulSet) *corev1.Container {
	t.Helper()
	for i := range sts.Spec.Template.Spec.Containers {
		c := &sts.Spec.Template.Spec.Containers[i]
		if c.Name == appNameValue {
			return c
		}
	}
	t.Fatalf("nats container not found")
	return nil
}
