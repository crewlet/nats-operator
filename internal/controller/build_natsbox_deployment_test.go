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
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	natsv1alpha1 "github.com/crewlet/nats-operator/api/v1alpha1"
)

func TestBuildNatsBoxDeployment_BasicShape(t *testing.T) {
	cr := newNatsBox(withBoxClusterRef())
	spec := defaultedNatsBox(&cr.Spec)
	d := buildNatsBoxDeployment(cr, &spec)

	require.NotNil(t, d)
	assert.Equal(t, "nats-box", d.Spec.Template.Spec.Containers[0].Name)
	// Must keep the container alive — nats-box images don't run a
	// long-lived server.
	cmd := strings.Join(d.Spec.Template.Spec.Containers[0].Command, " ")
	assert.Contains(t, cmd, "tail -f /dev/null")

	// Selector must match template labels — otherwise the Deployment
	// controller refuses to create the ReplicaSet.
	for k, v := range d.Spec.Selector.MatchLabels {
		assert.Equalf(t, v, d.Spec.Template.Labels[k], "selector label %q not in template labels", k)
	}

	// XDG_CONFIG_HOME env var must be set so the nats CLI looks for
	// its context directory at the mounted path.
	var sawXDG bool
	for _, e := range d.Spec.Template.Spec.Containers[0].Env {
		if e.Name == "XDG_CONFIG_HOME" && e.Value == natsBoxConfigHome {
			sawXDG = true
		}
	}
	assert.Truef(t, sawXDG, "container missing XDG_CONFIG_HOME=%q", natsBoxConfigHome)
}

func TestBuildNatsBoxDeployment_ContextsVolumeMountedAtNatsDir(t *testing.T) {
	// Regression: mounting the contexts Secret at the wrong directory
	// was a real bug — context.txt ended up under context/context.txt
	// and the nats CLI couldn't find the default context. The fix was
	// to mount at natsBoxNatsDir and use Secret Items.Path to place
	// context.txt at the mount root and <name>.json under context/.
	cr := newNatsBox(withBoxClusterRef())
	spec := defaultedNatsBox(&cr.Spec)
	d := buildNatsBoxDeployment(cr, &spec)

	var mount *corev1.VolumeMount
	for i, m := range d.Spec.Template.Spec.Containers[0].VolumeMounts {
		if m.Name == natsBoxContextsVol {
			mount = &d.Spec.Template.Spec.Containers[0].VolumeMounts[i]
		}
	}
	require.NotNilf(t, mount, "contexts volume %q not mounted", natsBoxContextsVol)
	assert.Equal(t, natsBoxNatsDir, mount.MountPath)

	var vol *corev1.Volume
	for i, v := range d.Spec.Template.Spec.Volumes {
		if v.Name == natsBoxContextsVol {
			vol = &d.Spec.Template.Spec.Volumes[i]
		}
	}
	require.NotNil(t, vol)
	require.NotNil(t, vol.Secret, "contexts volume must be a Secret source")

	// context.txt must land at the mount root, and <name>.json must
	// land under context/ — the nats CLI won't find them otherwise.
	var sawContextTxt, sawDefaultJSON bool
	for _, item := range vol.Secret.Items {
		if item.Key == "context.txt" && item.Path == "context.txt" {
			sawContextTxt = true
		}
		if item.Key == "default.json" && item.Path == "context/default.json" {
			sawDefaultJSON = true
		}
	}
	assert.True(t, sawContextTxt, "contexts volume missing context.txt → context.txt projection")
	assert.True(t, sawDefaultJSON, "contexts volume missing default.json → context/default.json projection")
}

func TestBuildNatsBoxDeployment_PerContextCredsMounted(t *testing.T) {
	cr := newNatsBox(
		withBoxClusterRef(),
		withBoxContext("app", natsv1alpha1.NatsBoxContext{
			URL: "nats://my-cluster:4222",
			Creds: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "app-creds"},
				Key:                  "nats.creds",
			},
		}),
	)
	spec := defaultedNatsBox(&cr.Spec)
	d := buildNatsBoxDeployment(cr, &spec)

	// Creds volume is named with the context name, not the Secret name,
	// so that two contexts pointing at the same Secret don't collide.
	const wantVol = "creds-app"
	var sawVol bool
	for _, v := range d.Spec.Template.Spec.Volumes {
		if v.Name == wantVol {
			sawVol = true
			assert.Equal(t, "app-creds", v.Secret.SecretName)
		}
	}
	assert.Truef(t, sawVol, "expected per-context creds volume %q", wantVol)

	// Mount path must match what the rendered context JSON points at.
	wantPath := natsBoxCredsDirRoot + "/app"
	var sawMount bool
	for _, m := range d.Spec.Template.Spec.Containers[0].VolumeMounts {
		if m.Name == wantVol {
			sawMount = true
			assert.Equal(t, wantPath, m.MountPath)
		}
	}
	assert.Truef(t, sawMount, "expected per-context creds mount %q", wantVol)
}

func TestBuildNatsBoxDeployment_TopologySelectorOverridden(t *testing.T) {
	// Same invariant as NatsCluster: a user-supplied labelSelector on
	// a topologySpreadConstraint must be replaced with the canonical
	// pod selector — otherwise the constraint targets the wrong pods.
	cr := newNatsBox(withBoxClusterRef())
	cr.Spec.PodTemplate.TopologySpreadConstraints = []corev1.TopologySpreadConstraint{{
		MaxSkew:     1,
		TopologyKey: "topology.kubernetes.io/zone",
		LabelSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"evil": "hijack"},
		},
	}}
	spec := defaultedNatsBox(&cr.Spec)
	d := buildNatsBoxDeployment(cr, &spec)

	tsc := d.Spec.Template.Spec.TopologySpreadConstraints
	require.Len(t, tsc, 1)
	assert.NotContains(t, tsc[0].LabelSelector.MatchLabels, "evil",
		"user-supplied LabelSelector leaked into topology spread constraint")
	for k, v := range natsBoxSelectorLabels(cr) {
		assert.Equalf(t, v, tsc[0].LabelSelector.MatchLabels[k],
			"topology selector missing canonical label %q", k)
	}
}
