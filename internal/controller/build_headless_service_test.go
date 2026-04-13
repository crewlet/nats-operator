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
)

func TestBuildHeadlessService_Invariants(t *testing.T) {
	cr := newNatsCluster()
	spec := defaulted(&cr.Spec)
	svc := buildHeadlessService(cr, &spec)

	require.NotNil(t, svc)
	assert.Equal(t, headlessServiceName(cr), svc.Name)
	assert.Equal(t, corev1.ClusterIPNone, svc.Spec.ClusterIP,
		"headless service must have ClusterIP=None")
	assert.True(t, svc.Spec.PublishNotReadyAddresses,
		"headless service must set publishNotReadyAddresses=true so per-pod DNS records exist before pods are Ready")

	// Selector must exactly match the canonical selector labels, not
	// just the common label set — otherwise new labels added to common
	// would make the selector miss pods.
	for k, v := range selectorLabels(cr) {
		assert.Equalf(t, v, svc.Spec.Selector[k], "selector missing %q", k)
	}
}

func TestBuildHeadlessService_PortsReflectEnabledListeners(t *testing.T) {
	cr := newNatsCluster(
		withReplicas(3),
		withLeafNodes(0),
		withWebSocket(),
		withMQTT(0),
		withGateway(0),
		withProfiling(0),
		withPromExporter(),
	)
	spec := defaulted(&cr.Spec)
	svc := buildHeadlessService(cr, &spec)

	got := map[string]int32{}
	for _, p := range svc.Spec.Ports {
		got[p.Name] = p.Port
	}
	wantPorts := map[string]int32{
		appNameValue: defaultNatsPort,
		"cluster":    defaultClusterPort,
		"monitor":    defaultMonitorPort,
		"leafnodes":  defaultLeafNodesPort,
		"websocket":  defaultWebSocketPort,
		"mqtt":       defaultMQTTPort,
		"gateway":    defaultGatewayPort,
		"profiling":  defaultProfilingPort,
		"metrics":    defaultExporterPort,
	}
	for name, port := range wantPorts {
		assert.Equalf(t, port, got[name], "headless port %q", name)
	}
}

func TestBuildHeadlessService_ClusterPortAbsentAtSingleReplica(t *testing.T) {
	// With a single replica there's no cluster to route, so the cluster
	// port should not be advertised on the headless service.
	cr := newNatsCluster()
	spec := defaulted(&cr.Spec)
	svc := buildHeadlessService(cr, &spec)
	for _, p := range svc.Spec.Ports {
		assert.NotEqual(t, "cluster", p.Name,
			"cluster port should not be exposed on a single-replica NatsCluster")
	}
}
