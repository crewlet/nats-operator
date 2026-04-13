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

func TestBuildClientService_Invariants(t *testing.T) {
	cr := newNatsCluster()
	spec := defaulted(&cr.Spec)
	svc := buildClientService(cr, &spec)

	require.NotNil(t, svc)
	assert.Equal(t, clientServiceName(cr), svc.Name)
	assert.Equal(t, corev1.ServiceTypeClusterIP, svc.Spec.Type)
	// Must be a regular ClusterIP service (NOT headless) so kube-proxy
	// load-balances to ready pods — that is why the nats-box context
	// points at the client service, not the headless one.
	assert.NotEqual(t, corev1.ClusterIPNone, svc.Spec.ClusterIP,
		"client service must NOT be headless")
	for k, v := range selectorLabels(cr) {
		assert.Equalf(t, v, svc.Spec.Selector[k], "selector missing %q", k)
	}
}

func TestBuildClientService_NilWhenDisabled(t *testing.T) {
	cr := newNatsCluster()
	cr.Spec.Service.Enabled = ptr(false)
	spec := defaulted(&cr.Spec)

	assert.Nil(t, buildClientService(cr, &spec))
}

func TestBuildClientService_OnlyClientFacingPorts(t *testing.T) {
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
	svc := buildClientService(cr, &spec)

	got := map[string]bool{}
	for _, p := range svc.Spec.Ports {
		got[p.Name] = true
	}

	// Must expose
	for _, name := range []string{appNameValue, "leafnodes", "websocket", "mqtt", "gateway"} {
		assert.Truef(t, got[name], "client service missing %q listener port", name)
	}
	// Must NOT expose — these are internal-only and only belong on the
	// headless service / PodMonitor.
	for _, name := range []string{"cluster", "monitor", "profiling", "metrics"} {
		assert.Falsef(t, got[name], "client service must not expose internal %q port", name)
	}
}

func TestBuildClientService_NodePortMapOverride(t *testing.T) {
	cr := newNatsCluster()
	cr.Spec.Service.NodePorts = map[string]int32{appNameValue: 30422}
	spec := defaulted(&cr.Spec)
	svc := buildClientService(cr, &spec)

	for _, p := range svc.Spec.Ports {
		if p.Name == appNameValue {
			assert.Equal(t, int32(30422), p.NodePort)
		}
	}
}
