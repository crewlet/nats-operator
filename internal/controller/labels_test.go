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
)

func TestCommonLabels_HasCanonicalSet(t *testing.T) {
	cr := newNatsCluster()
	got := commonLabels(cr)

	mustHave := map[string]string{
		labelAppName:     appNameValue,
		labelAppInstance: testName,
		labelAppManaged:  managedByValue,
		labelAppPartOf:   appNameValue,
		labelSelectorKey: testName,
	}
	for k, want := range mustHave {
		assert.Equalf(t, want, got[k], "commonLabels[%q]", k)
	}
}

func TestSelectorLabels_IsSubsetOfCommon(t *testing.T) {
	cr := newNatsCluster()
	sel := selectorLabels(cr)
	com := commonLabels(cr)

	for k, v := range sel {
		assert.Equalf(t, v, com[k], "selectorLabels[%q] not in commonLabels", k)
	}

	// The selector must include the per-cluster canonical key so two
	// clusters in the same namespace don't pick up each other's pods.
	assert.NotEmptyf(t, sel[labelSelectorKey], "selectorLabels must contain %q", labelSelectorKey)
}

func TestSelectorLabels_IsUniquePerClusterName(t *testing.T) {
	a := selectorLabels(newNatsCluster())
	// A different NatsCluster name must produce a different selector
	// value — otherwise PDB / Service / StatefulSet selectors would
	// match the wrong pods across two NatsClusters in the same namespace.
	other := newNatsCluster()
	other.Name = "other-cluster"
	b := selectorLabels(other)

	assert.NotEqualf(t, a[labelSelectorKey], b[labelSelectorKey],
		"selectorLabels for two different NatsClusters share %q", labelSelectorKey)
}

func TestMergeUserLabels_CanonicalAlwaysWins(t *testing.T) {
	canonical := map[string]string{
		labelAppName:     appNameValue,
		labelAppInstance: "demo",
		labelSelectorKey: "demo",
	}
	user := map[string]string{
		// Hostile user input trying to hijack the selector.
		labelSelectorKey: "not-demo",
		labelAppName:     "something-else",
		"env":            "prod",
	}

	out := mergeUserLabels(canonical, user)

	assert.Equal(t, "demo", out[labelSelectorKey], "user must not hijack selector key")
	assert.Equal(t, appNameValue, out[labelAppName], "user must not hijack app.kubernetes.io/name")
	assert.Equal(t, "prod", out["env"], "non-canonical user label should pass through")
}

func TestMergeUserLabels_NilUserReturnsCopyOfCanonical(t *testing.T) {
	canonical := map[string]string{"k": "v"}
	out := mergeUserLabels(canonical, nil)
	assert.Equal(t, canonical, out)
	// Must be a copy — caller must not be able to mutate the canonical map.
	out["k"] = "mutated"
	assert.Equal(t, "v", canonical["k"], "mergeUserLabels must not alias the canonical map")
}

func TestMergeAnnotations_UserWins(t *testing.T) {
	base := map[string]string{"a": "base", "b": "base"}
	user := map[string]string{"b": "user", "c": "user"}

	out := mergeAnnotations(base, user)

	assert.Equal(t, map[string]string{"a": "base", "b": "user", "c": "user"}, out)
}
