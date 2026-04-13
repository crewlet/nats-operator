/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	natsv1alpha1 "github.com/crewlet/nats-operator/api/v1alpha1"
)

func TestDefaulted_FillsScalarsOnEmptySpec(t *testing.T) {
	cr := newNatsCluster()
	out := defaulted(&cr.Spec)

	require.NotNil(t, out.Replicas)
	assert.Equal(t, int32(1), *out.Replicas)
	assert.Equal(t, appNameValue, out.Image.Repository)
	assert.NotEmpty(t, out.Image.Tag, "image.tag should have been defaulted")
	assert.Equal(t, corev1.PullIfNotPresent, out.Image.PullPolicy)
	assert.Equal(t, defaultNatsPort, out.Config.Nats.Port)
	assert.Equal(t, defaultClusterPort, out.Config.Cluster.Port)
	require.NotNil(t, out.Config.Cluster.NoAdvertise)
	assert.True(t, *out.Config.Cluster.NoAdvertise)
	assert.Equal(t, defaultClusterDomain, out.Config.Cluster.RouteURLs.K8sClusterDomain)
	require.NotNil(t, out.Config.Monitor.Enabled)
	assert.True(t, *out.Config.Monitor.Enabled)
	assert.Equal(t, defaultMonitorPort, out.Config.Monitor.Port)
	require.NotNil(t, out.Reloader.Enabled)
	assert.True(t, *out.Reloader.Enabled)
	require.NotNil(t, out.Service.Enabled)
	assert.True(t, *out.Service.Enabled)
	assert.Equal(t, corev1.ServiceTypeClusterIP, out.Service.Type)
	require.NotNil(t, out.PodDisruptionBudget.Enabled)
	assert.True(t, *out.PodDisruptionBudget.Enabled)
}

func TestDefaulted_DoesNotOverrideUserValues(t *testing.T) {
	cr := newNatsCluster(
		withReplicas(7),
		withImage("quay.io/custom/nats", "2.11.3"),
	)
	cr.Spec.Config.Nats.Port = 9999
	cr.Spec.Config.Cluster.Port = 9998
	cr.Spec.Service.Type = corev1.ServiceTypeLoadBalancer
	cr.Spec.Reloader.Enabled = ptr(false)
	cr.Spec.Config.Monitor.Enabled = ptr(false)
	cr.Spec.Config.Cluster.NoAdvertise = ptr(false)
	cr.Spec.Config.Cluster.RouteURLs.K8sClusterDomain = "custom.example"

	out := defaulted(&cr.Spec)

	assert.Equal(t, int32(7), *out.Replicas)
	assert.Equal(t, "quay.io/custom/nats", out.Image.Repository)
	assert.Equal(t, "2.11.3", out.Image.Tag)
	assert.Equal(t, int32(9999), out.Config.Nats.Port)
	assert.Equal(t, int32(9998), out.Config.Cluster.Port)
	assert.Equal(t, corev1.ServiceTypeLoadBalancer, out.Service.Type)
	assert.False(t, *out.Reloader.Enabled)
	assert.False(t, *out.Config.Monitor.Enabled)
	assert.False(t, *out.Config.Cluster.NoAdvertise)
	assert.Equal(t, "custom.example", out.Config.Cluster.RouteURLs.K8sClusterDomain)
}

func TestDefaulted_DoesNotMutateInput(t *testing.T) {
	cr := newNatsCluster(withReplicas(3))
	before := cr.Spec.Replicas
	_ = defaulted(&cr.Spec)
	assert.Same(t, before, cr.Spec.Replicas, "defaulted() rewrote the input's Replicas pointer")

	// A fresh empty cluster's Config.Nats.Port should stay zero after
	// defaulted() runs, because defaulted works on a deep copy.
	fresh := newNatsCluster()
	_ = defaulted(&fresh.Spec)
	assert.Zero(t, fresh.Spec.Config.Nats.Port, "defaulted() mutated input spec")
}

func TestDefaulted_JetStreamPVCGetsAccessModes(t *testing.T) {
	cr := newNatsCluster(withJetStream())
	out := defaulted(&cr.Spec)

	assert.Equal(t,
		[]corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		out.Config.JetStream.FileStore.PVC.AccessModes)
}

func TestDefaulted_JetStreamPVCDoesNotOverrideUserAccessModes(t *testing.T) {
	cr := newNatsCluster(withJetStream())
	cr.Spec.Config.JetStream.FileStore.PVC.AccessModes = []corev1.PersistentVolumeAccessMode{
		corev1.ReadWriteMany,
	}
	out := defaulted(&cr.Spec)

	assert.Equal(t,
		[]corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
		out.Config.JetStream.FileStore.PVC.AccessModes)
}

func TestDefaulted_TLSBlockGetsCertAndKeyDefaults(t *testing.T) {
	cr := newNatsCluster(withNatsTLS("nats-tls"))
	out := defaulted(&cr.Spec)

	assert.Equal(t, defaultTLSCertFile, out.Config.Nats.TLS.Cert)
	assert.Equal(t, defaultTLSKeyFile, out.Config.Nats.TLS.Key)
}

func TestDefaulted_TLSDefaultsSkippedWhenDisabled(t *testing.T) {
	cr := newNatsCluster()
	// TLS not enabled — cert/key should stay empty so the operator can
	// detect "user did not configure TLS" cleanly.
	out := defaulted(&cr.Spec)
	require.False(t, out.Config.Nats.TLS.Enabled)
	assert.Empty(t, out.Config.Nats.TLS.Cert)
	assert.Empty(t, out.Config.Nats.TLS.Key)
}

func TestDefaulted_ListenerPortDefaults(t *testing.T) {
	cr := newNatsCluster(
		withLeafNodes(0),
		withMQTT(0),
		withGateway(0),
		withWebSocket(),
		withProfiling(0),
	)
	out := defaulted(&cr.Spec)

	assert.Equal(t, defaultLeafNodesPort, out.Config.LeafNodes.Port, "leafnodes port")
	assert.Equal(t, defaultMQTTPort, out.Config.MQTT.Port, "mqtt port")
	assert.Equal(t, defaultGatewayPort, out.Config.Gateway.Port, "gateway port")
	assert.Equal(t, defaultWebSocketPort, out.Config.WebSocket.Port, "websocket port")
	assert.Equal(t, defaultProfilingPort, out.Config.Profiling.Port, "profiling port")
}

func TestDefaulted_JWTResolverFullPVCAccessModes(t *testing.T) {
	cr := newNatsCluster()
	cr.Spec.Auth.JWT = &natsv1alpha1.JWTAuthSpec{
		Operator:      corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "op"}, Key: "operator.jwt"},
		SystemAccount: "AASYS",
		Accounts: []natsv1alpha1.JWTAccount{
			{Name: "sys", PublicKey: "AASYS", JWT: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "sys-jwt"}, Key: "account.jwt"}},
		},
		Resolver: natsv1alpha1.JWTResolverSpec{
			Type:    natsv1alpha1.JWTResolverFull,
			Storage: &corev1.PersistentVolumeClaimSpec{},
		},
	}

	out := defaulted(&cr.Spec)

	assert.Equal(t,
		[]corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		out.Auth.JWT.Resolver.Storage.AccessModes)
}

func TestDefaulted_JWTResolverMemoryDoesNotAllocatePVC(t *testing.T) {
	cr := newNatsCluster()
	cr.Spec.Auth.JWT = &natsv1alpha1.JWTAuthSpec{
		SystemAccount: "AASYS",
		Accounts:      []natsv1alpha1.JWTAccount{{Name: "sys", PublicKey: "AASYS"}},
		Resolver:      natsv1alpha1.JWTResolverSpec{Type: natsv1alpha1.JWTResolverMemory},
	}

	out := defaulted(&cr.Spec)
	assert.Nil(t, out.Auth.JWT.Resolver.Storage, "memory resolver should not have storage defaulted")
}

// Regression test: the bug where the JetStream PVC got an empty
// accessModes list made the StatefulSet fail to create with "at least
// one access mode is required." This test pins the default so the
// regression can't come back.
func TestDefaulted_Regression_JetStreamPVCAccessModesNotEmpty(t *testing.T) {
	cr := newNatsCluster(withJetStream())
	out := defaulted(&cr.Spec)
	require.NotEmpty(t, out.Config.JetStream.FileStore.PVC.AccessModes,
		"jetstream PVC accessModes must not be empty — K8s rejects the StatefulSet")
}
