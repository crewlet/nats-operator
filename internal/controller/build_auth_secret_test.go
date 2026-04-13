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
	corev1 "k8s.io/api/core/v1"

	natsv1alpha1 "github.com/crewlet/nats-operator/api/v1alpha1"
)

func TestBuildAuthSecret_NilWithoutJWT(t *testing.T) {
	cr := newNatsCluster()
	spec := defaulted(&cr.Spec)
	assert.Nil(t, buildAuthSecret(cr, &spec, nil))
}

func TestBuildAuthSecret_ContainsExpectedKeys(t *testing.T) {
	cr := newNatsCluster(withJWTAuth(
		"op",
		jwtAccount("sys", "AASYS", "sys-jwt"),
		jwtAccount("app", "BBAPP", "app-jwt"),
	))
	spec := defaulted(&cr.Spec)

	material := &jwtMaterial{
		operatorJWT: []byte("eyJ-fake-operator-jwt"),
		accountJWTs: map[string][]byte{
			"sys": []byte("eyJ-fake-sys-jwt"),
			"app": []byte("eyJ-fake-app-jwt"),
		},
	}

	s := buildAuthSecret(cr, &spec, material)
	require.NotNil(t, s)
	assert.Equal(t, corev1.SecretTypeOpaque, s.Type)

	for _, key := range []string{authFileName, authOperatorJWTFileName, authResolverPreloadName} {
		assert.Containsf(t, s.Data, key, "auth Secret missing key %q", key)
	}

	// The operator JWT bytes must be copied byte-for-byte.
	assert.Equal(t, "eyJ-fake-operator-jwt", string(s.Data[authOperatorJWTFileName]))

	// The auth.conf fragment must reference the mounted operator JWT
	// path (NOT embed the JWT inline, which would leak into the
	// ConfigMap when/if the operator confuses the two).
	authConf := string(s.Data[authFileName])
	wantPath := mountPathAuth + "/" + authOperatorJWTFileName
	assert.Contains(t, authConf, wantPath)
	// Never inline the JWT bytes in auth.conf.
	assert.NotContains(t, authConf, "eyJ-fake-operator-jwt",
		"auth.conf inlined the operator JWT — JWT material must stay in the dedicated file")

	// resolver_preload.conf must list every account's public key
	// against its own JWT bytes.
	preload := string(s.Data[authResolverPreloadName])
	for pk, jwt := range map[string]string{"AASYS": "eyJ-fake-sys-jwt", "BBAPP": "eyJ-fake-app-jwt"} {
		assert.Contains(t, preload, pk)
		assert.Contains(t, preload, jwt)
	}
}

func TestBuildAuthSecret_MemoryResolverEmitsKeyword(t *testing.T) {
	cr := newNatsCluster(withJWTAuth(
		"op",
		jwtAccount("sys", "AASYS", "sys-jwt"),
	))
	cr.Spec.Auth.JWT.Resolver.Type = natsv1alpha1.JWTResolverMemory
	spec := defaulted(&cr.Spec)
	material := &jwtMaterial{operatorJWT: []byte("op"), accountJWTs: map[string][]byte{"sys": []byte("sys")}}

	authConf := string(buildAuthSecret(cr, &spec, material).Data[authFileName])
	// Memory resolver must be rendered as a bare keyword — otherwise
	// nats-server's config parser rejects it.
	assert.Contains(t, authConf, "resolver: MEMORY",
		"memory resolver missing or not emitted as bare keyword")
}

func TestBuildAuthSecret_FullResolverEmitsObjectBlock(t *testing.T) {
	cr := newNatsCluster(withJWTAuth(
		"op",
		jwtAccount("sys", "AASYS", "sys-jwt"),
	))
	cr.Spec.Auth.JWT.Resolver.Type = natsv1alpha1.JWTResolverFull
	cr.Spec.Auth.JWT.Resolver.Storage = &corev1.PersistentVolumeClaimSpec{}
	cr.Spec.Auth.JWT.Resolver.AllowDelete = ptr(true)
	cr.Spec.Auth.JWT.Resolver.Interval = "5m"

	spec := defaulted(&cr.Spec)
	material := &jwtMaterial{operatorJWT: []byte("op"), accountJWTs: map[string][]byte{"sys": []byte("sys")}}
	authConf := string(buildAuthSecret(cr, &spec, material).Data[authFileName])

	assert.Contains(t, authConf, `type: "full"`)
	assert.Contains(t, authConf, `dir: "`+mountPathResolver+`"`)
	assert.Contains(t, authConf, "allow_delete: true")
}

func TestBuildAuthSecret_ResolverPreloadIsDeterministic(t *testing.T) {
	// Building the same spec twice must produce byte-identical
	// resolver_preload.conf — otherwise every reconcile rewrites the
	// Secret and triggers a useless pod restart via the reloader.
	cr := newNatsCluster(withJWTAuth(
		"op",
		jwtAccount("sys", "AASYS", "sys-jwt"),
		jwtAccount("app", "BBAPP", "app-jwt"),
		jwtAccount("ops", "CCOPS", "ops-jwt"),
	))
	spec := defaulted(&cr.Spec)
	material := &jwtMaterial{
		operatorJWT: []byte("op"),
		accountJWTs: map[string][]byte{"sys": []byte("sys-jwt"), "app": []byte("app-jwt"), "ops": []byte("ops-jwt")},
	}

	first := buildAuthSecret(cr, &spec, material).Data[authResolverPreloadName]
	second := buildAuthSecret(cr, &spec, material).Data[authResolverPreloadName]
	assert.Equal(t, string(first), string(second),
		"resolver_preload.conf not stable across builds")
}
