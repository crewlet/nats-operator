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

func TestBuildNackAccounts_NilWithoutJWT(t *testing.T) {
	cr := newNatsCluster()
	spec := defaulted(&cr.Spec)
	assert.Nil(t, buildNackAccounts(cr, &spec))
}

func TestBuildNackAccounts_SkipsEntriesWithoutUserCreds(t *testing.T) {
	cr := newNatsCluster(withJWTAuth(
		"op",
		jwtAccount("sys", "AASYS", "sys-jwt"),
	))
	spec := defaulted(&cr.Spec)

	assert.Empty(t, buildNackAccounts(cr, &spec))
}

func TestBuildNackAccounts_OnePerEntryWithUserCreds(t *testing.T) {
	cr := newNatsCluster(withJWTAuth(
		"op",
		jwtAccount("sys", "AASYS", "sys-jwt", withAccountUserCreds("sys-creds", "nats.creds")),
		jwtAccount("app", "BBAPP", "app-jwt", withAccountUserCreds("app-creds", "nats.creds")),
		jwtAccount("no-nack", "CCNACK", "no-nack-jwt"),
	))
	spec := defaulted(&cr.Spec)

	accounts := buildNackAccounts(cr, &spec)
	require.Len(t, accounts, 2)

	// Each generated Account points at the cluster's client endpoint
	// with the conventional name suffix.
	seen := map[string]bool{}
	for _, a := range accounts {
		seen[a.Name] = true
		assert.Lenf(t, a.Spec.Servers, 1, "NACK Account %q must have exactly one server URL", a.Name)
		assert.NotEmptyf(t, a.Spec.Servers[0], "NACK Account %q has empty server URL", a.Name)
		require.NotNilf(t, a.Spec.Creds, "NACK Account %q missing Creds", a.Name)
		assert.NotNilf(t, a.Spec.Creds.Secret, "NACK Account %q missing Creds secret ref", a.Name)
	}
	assert.True(t, seen["test-sys"])
	assert.True(t, seen["test-app"])
	assert.False(t, seen["test-no-nack"],
		"entry without userCreds should not produce a NACK Account")
}

func TestBuildNackAccounts_UsesClientEndpoint(t *testing.T) {
	cr := newNatsCluster(withJWTAuth(
		"op",
		jwtAccount("sys", "AASYS", "sys-jwt", withAccountUserCreds("sys-creds", "nats.creds")),
	))
	spec := defaulted(&cr.Spec)
	accounts := buildNackAccounts(cr, &spec)

	require.Len(t, accounts, 1)
	ep := clusterEndpoints(cr, &spec)
	assert.Equal(t, ep.Client, accounts[0].Spec.Servers[0])
}
