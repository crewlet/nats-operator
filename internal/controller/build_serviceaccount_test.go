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

func TestBuildServiceAccount_NilByDefault(t *testing.T) {
	cr := newNatsCluster()
	spec := defaulted(&cr.Spec)
	assert.Nil(t, buildServiceAccount(cr, &spec),
		"ServiceAccount should be nil unless serviceAccount.enabled=true")
}

func TestBuildServiceAccount_CreatedWhenEnabled(t *testing.T) {
	cr := newNatsCluster(withServiceAccount())
	cr.Spec.ServiceAccount.Annotations = map[string]string{"eks.amazonaws.com/role-arn": "arn:aws:iam::..."}

	spec := defaulted(&cr.Spec)
	sa := buildServiceAccount(cr, &spec)

	require.NotNil(t, sa)
	assert.Equal(t, serviceAccountName(cr), sa.Name)
	assert.NotEmpty(t, sa.Annotations["eks.amazonaws.com/role-arn"],
		"user annotation (e.g. IRSA) not propagated")
	for k, v := range commonLabels(cr) {
		assert.Equalf(t, v, sa.Labels[k], "SA missing canonical label %q", k)
	}
}
