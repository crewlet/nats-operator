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
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestBuildPDB_NilOnSingleReplica(t *testing.T) {
	cr := newNatsCluster()
	spec := defaulted(&cr.Spec)
	assert.Nil(t, buildPDB(cr, &spec),
		"PDB should be nil on single-replica cluster (would block drains)")
}

func TestBuildPDB_NilWhenDisabled(t *testing.T) {
	cr := newNatsCluster(withReplicas(3))
	cr.Spec.PodDisruptionBudget.Enabled = ptr(false)
	spec := defaulted(&cr.Spec)
	assert.Nil(t, buildPDB(cr, &spec))
}

func TestBuildPDB_SelectorAlwaysMatchesPods(t *testing.T) {
	// Regression: the operator must overwrite any user-supplied
	// selector. Letting users set the selector would silently break
	// multi-tenancy when two NatsClusters share a namespace.
	cr := newNatsCluster(withReplicas(3))
	spec := defaulted(&cr.Spec)
	pdb := buildPDB(cr, &spec)
	require.NotNil(t, pdb, "expected a PDB for a 3-replica cluster")
	require.NotNil(t, pdb.Spec.Selector)
	for k, v := range selectorLabels(cr) {
		assert.Equalf(t, v, pdb.Spec.Selector.MatchLabels[k], "PDB selector missing %q", k)
	}
}

func TestBuildPDB_DefaultMaxUnavailableIsOne(t *testing.T) {
	cr := newNatsCluster(withReplicas(3))
	spec := defaulted(&cr.Spec)
	pdb := buildPDB(cr, &spec)

	require.NotNil(t, pdb.Spec.MaxUnavailable,
		"default PDB should set MaxUnavailable=1 when neither min/max is provided")
	assert.Equal(t, intstr.FromInt32(1), *pdb.Spec.MaxUnavailable)
}

func TestBuildPDB_RespectsUserMinAvailable(t *testing.T) {
	cr := newNatsCluster(withReplicas(3))
	ma := intstr.FromString("51%")
	cr.Spec.PodDisruptionBudget.MinAvailable = &ma
	spec := defaulted(&cr.Spec)
	pdb := buildPDB(cr, &spec)

	require.NotNil(t, pdb.Spec.MinAvailable)
	assert.Equal(t, ma, *pdb.Spec.MinAvailable)
	assert.Nil(t, pdb.Spec.MaxUnavailable,
		"operator should not also set MaxUnavailable when user set MinAvailable")
}
