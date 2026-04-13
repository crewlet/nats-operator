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

func TestBuildPodMonitor_NilWhenExporterDisabled(t *testing.T) {
	cr := newNatsCluster()
	cr.Spec.PromExporter.PodMonitor.Enabled = true
	// PromExporter itself not enabled — the CEL rule would reject this
	// at admission, but the builder must also be defensive.
	spec := defaulted(&cr.Spec)
	assert.Nil(t, buildPodMonitor(cr, &spec),
		"PodMonitor should be nil unless PromExporter is also enabled")
}

func TestBuildPodMonitor_NilWhenPodMonitorDisabled(t *testing.T) {
	cr := newNatsCluster(withPromExporter())
	spec := defaulted(&cr.Spec)
	assert.Nil(t, buildPodMonitor(cr, &spec))
}

func TestBuildPodMonitor_CreatedWhenBothEnabled(t *testing.T) {
	cr := newNatsCluster(withPodMonitor())
	spec := defaulted(&cr.Spec)
	pm := buildPodMonitor(cr, &spec)
	require.NotNil(t, pm)
	assert.Equal(t, podMonitorName(cr), pm.GetName())
	// GVK must match what NACK expects — if we render the wrong GVK
	// the prometheus-operator controller ignores the CR silently.
	gvk := pm.GroupVersionKind()
	assert.Equal(t, "monitoring.coreos.com", gvk.Group)
	assert.Equal(t, "v1", gvk.Version)
	assert.Equal(t, "PodMonitor", gvk.Kind)
	for k, v := range commonLabels(cr) {
		assert.Equalf(t, v, pm.GetLabels()[k], "PodMonitor missing canonical label %q", k)
	}
}
