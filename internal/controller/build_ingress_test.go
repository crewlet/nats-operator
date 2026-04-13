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
)

func TestBuildWebSocketIngress_NilByDefault(t *testing.T) {
	cr := newNatsCluster(withWebSocket())
	spec := defaulted(&cr.Spec)
	assert.Nil(t, buildWebSocketIngress(cr, &spec),
		"Ingress should be nil when ingress.enabled=false")
}

func TestBuildWebSocketIngress_CreatedWithHosts(t *testing.T) {
	cr := newNatsCluster(withWebSocketIngress("ws.example.com", "alt.example.com"))
	spec := defaulted(&cr.Spec)
	ing := buildWebSocketIngress(cr, &spec)
	require.NotNil(t, ing)
	require.Len(t, ing.Spec.Rules, 2)
	assert.Equal(t, "ws.example.com", ing.Spec.Rules[0].Host)

	// Each rule must point at the client service, not the headless one
	// — headless services don't have a ClusterIP for ingress to route to.
	for _, r := range ing.Spec.Rules {
		for _, p := range r.HTTP.Paths {
			assert.Equal(t, clientServiceName(cr), p.Backend.Service.Name)
			assert.Equal(t, defaultWebSocketPort, p.Backend.Service.Port.Number)
		}
	}
}

func TestBuildWebSocketIngress_TLS(t *testing.T) {
	cr := newNatsCluster(withWebSocketIngress("ws.example.com"))
	cr.Spec.Config.WebSocket.Ingress.TLSSecretName = "ws-cert"
	spec := defaulted(&cr.Spec)
	ing := buildWebSocketIngress(cr, &spec)
	require.Len(t, ing.Spec.TLS, 1)
	assert.Equal(t, "ws-cert", ing.Spec.TLS[0].SecretName)
}
