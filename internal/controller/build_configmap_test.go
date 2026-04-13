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

func TestBuildConfigMap_Basics(t *testing.T) {
	cr := newNatsCluster()
	spec := defaulted(&cr.Spec)
	rendered := renderNatsConf(cr, &spec)

	cm := buildConfigMap(cr, rendered)
	require.NotNil(t, cm)
	assert.Equal(t, configMapName(cr), cm.Name)
	assert.Equal(t, testNamespace, cm.Namespace)

	// Canonical labels must be present so GC / cleanup selectors match.
	for k, v := range commonLabels(cr) {
		assert.Equalf(t, v, cm.Labels[k], "missing label %q on ConfigMap", k)
	}

	// The rendered nats.conf must land under the conventional key.
	data, ok := cm.Data[natsConfFileName]
	require.Truef(t, ok, "ConfigMap missing key %q", natsConfFileName)
	assert.Equal(t, string(rendered), data)
	// Sanity: the rendered content should at least contain a port.
	assert.Contains(t, data, "port: 4222")
}

func TestBuildConfigMap_NilWhenExistingName(t *testing.T) {
	cr := newNatsCluster()
	cr.Spec.ConfigMap.ExistingName = "byo-cm"

	spec := defaulted(&cr.Spec)
	rendered := renderNatsConf(cr, &spec)

	assert.Nil(t, buildConfigMap(cr, rendered),
		"expected nil ConfigMap when existingName is set")
}

func TestBuildConfigMap_UserLabelsAndAnnotationsMerged(t *testing.T) {
	cr := newNatsCluster()
	cr.Spec.ConfigMap.Labels = map[string]string{"env": "prod"}
	cr.Spec.ConfigMap.Annotations = map[string]string{"owner": "team-a"}

	spec := defaulted(&cr.Spec)
	rendered := renderNatsConf(cr, &spec)

	cm := buildConfigMap(cr, rendered)
	assert.Equal(t, "prod", cm.Labels["env"])
	assert.Equal(t, "team-a", cm.Annotations["owner"])
	// Canonical label must still be there after merging.
	assert.Equal(t, testName, cm.Labels[labelSelectorKey],
		"canonical selector label got clobbered by user labels")
}
