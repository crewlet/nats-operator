/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	natsv1alpha1 "github.com/crewlet/nats-operator/api/v1alpha1"
)

func TestBuildNatsBoxContextsSecret_AutoDefaultContextFromClusterRef(t *testing.T) {
	cr := newNatsBox(withBoxClusterRef())
	spec := defaultedNatsBox(&cr.Spec)
	s := buildNatsBoxContextsSecret(cr, &spec)

	require.NotNil(t, s, "expected a contexts Secret")
	// Both the pointer file and the default context JSON must be there.
	require.Contains(t, s.Data, "context.txt", "contexts Secret missing context.txt pointer file")
	require.Contains(t, s.Data, "default.json", "contexts Secret missing auto-generated default.json")

	// context.txt must name the default context.
	assert.Equal(t, "default", string(s.Data["context.txt"]))

	// default.json must point at the client service of the referenced
	// cluster, NOT the headless service.
	var ctx natsContextFile
	require.NoError(t, json.Unmarshal(s.Data["default.json"], &ctx),
		"default.json is not valid JSON")
	// Regression: using the headless name was a real bug — nats.go
	// failed the client handshake even though TCP, INFO, and raw
	// protocol were all fine. Must point at the client ClusterIP
	// service (`<cluster>`) instead.
	assert.NotContains(t, ctx.URL, "-headless",
		"default context URL points at the headless service (bug)")
	assert.Contains(t, ctx.URL, "my-cluster.",
		"default context URL missing client service name")
	assert.True(t, strings.HasSuffix(ctx.URL, ".svc.cluster.local:4222"),
		"default context URL should be fully qualified, got %q", ctx.URL)
}

func TestBuildNatsBoxContextsSecret_UserOverrideWins(t *testing.T) {
	// If the user has defined a `default` context explicitly, the
	// operator must not overwrite it with the auto-generated one.
	cr := newNatsBox(
		withBoxClusterRef(),
		withBoxContext("default", natsv1alpha1.NatsBoxContext{
			URL: "nats://explicit.example:4222",
		}),
	)
	spec := defaultedNatsBox(&cr.Spec)
	s := buildNatsBoxContextsSecret(cr, &spec)

	var ctx natsContextFile
	require.NoError(t, json.Unmarshal(s.Data["default.json"], &ctx))
	assert.Equal(t, "nats://explicit.example:4222", ctx.URL)
}

func TestBuildNatsBoxContextsSecret_NoDefaultWithoutClusterRef(t *testing.T) {
	cr := newNatsBox()
	spec := defaultedNatsBox(&cr.Spec)
	s := buildNatsBoxContextsSecret(cr, &spec)

	// An empty NatsBox is valid (user can add contexts interactively),
	// and the operator must not synthesize a default context from
	// thin air.
	assert.NotContains(t, s.Data, "default.json",
		"should not auto-create default context without clusterRef")
}
