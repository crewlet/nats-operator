/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResourceNames_DerivedFromCRName(t *testing.T) {
	cr := newNatsCluster()
	cases := map[string]string{
		"configMapName":   configMapName(cr),
		"headlessService": headlessServiceName(cr),
		"clientService":   clientServiceName(cr),
		"statefulSet":     statefulSetName(cr),
		"pdb":             pdbName(cr),
		"serviceAccount":  serviceAccountName(cr),
		"podMonitor":      podMonitorName(cr),
		"ingress":         ingressName(cr),
		"authSecret":      authSecretName(cr),
	}
	for k, v := range cases {
		assert.Truef(t, strings.HasPrefix(v, testName),
			"%s = %q, expected to start with %q so two clusters can't collide", k, v, testName)
	}
}

func TestConfigMapName_RespectsExistingName(t *testing.T) {
	const byo = "byo-cm"
	cr := newNatsCluster()
	cr.Spec.ConfigMap.ExistingName = byo
	assert.Equal(t, byo, configMapName(cr))
}

func TestPodFQDN_AlwaysQualifiedWithClusterDomain(t *testing.T) {
	cr := newNatsCluster()
	assert.Equal(t, "test-0.test-headless.default.svc.cluster.local", podFQDN(cr, 0, "cluster.local"))
}

func TestNackAccountName_UnambiguousAcrossClusters(t *testing.T) {
	a := nackAccountName(newNatsCluster(), "system")
	other := newNatsCluster()
	other.Name = "another"
	b := nackAccountName(other, "system")
	assert.NotEqual(t, a, b, "nackAccountName collided across NatsClusters")
}

func TestTLSMountPath_PerListener(t *testing.T) {
	cases := map[string]string{
		appNameValue: "/etc/nats-certs/nats",
		"cluster":    "/etc/nats-certs/cluster",
		"leafnodes":  "/etc/nats-certs/leafnodes",
	}
	for listener, want := range cases {
		assert.Equalf(t, want, tlsMountPath(listener), "tlsMountPath(%q)", listener)
	}
}

func TestIncludeMountPath_UsesExtraDir(t *testing.T) {
	assert.Equal(t, "/etc/nats-extra/auth.conf", includeMountPath("auth.conf"))
}

func TestClusterEndpoints_ClientAndHeadless(t *testing.T) {
	cr := newNatsCluster()
	spec := defaulted(&cr.Spec)
	ep := clusterEndpoints(cr, &spec)

	assert.Equal(t, "nats://test.default.svc:4222", ep.Client)
	assert.Equal(t, "nats://test-headless.default.svc:4222", ep.Headless)
}

func TestClusterEndpoints_TLSScheme(t *testing.T) {
	cr := newNatsCluster(withNatsTLS("nats-tls"))
	spec := defaulted(&cr.Spec)
	ep := clusterEndpoints(cr, &spec)

	assert.Truef(t, strings.HasPrefix(ep.Client, "tls://"),
		"client endpoint should use tls:// scheme, got %q", ep.Client)
}

func TestClusterEndpoints_ClientEmptyWhenServiceDisabled(t *testing.T) {
	cr := newNatsCluster()
	cr.Spec.Service.Enabled = ptr(false)
	spec := defaulted(&cr.Spec)
	ep := clusterEndpoints(cr, &spec)

	assert.Empty(t, ep.Client, "client endpoint should be empty when service is disabled")
	assert.NotEmpty(t, ep.Headless, "headless endpoint should still be populated")
}
