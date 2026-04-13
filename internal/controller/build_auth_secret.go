/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"bytes"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	natsv1alpha1 "github.com/crewlet/nats-operator/api/v1alpha1"
)

// jwtMaterial is the content the reconciler has pulled from the user's
// JWT Secrets before calling buildAuthSecret. Keeping the builder pure
// (no client calls) makes it easy to unit-test.
type jwtMaterial struct {
	// operatorJWT is the raw bytes of the operator JWT read from the
	// referenced Secret key.
	operatorJWT []byte
	// accountJWTs is keyed by account name (the wrapper's `name` field)
	// and holds the raw account JWT bytes for each entry.
	accountJWTs map[string][]byte
}

// buildAuthSecret returns the operator-managed Secret that the nats
// container mounts at /etc/nats-auth. It contains:
//
//   - operator.jwt — copied from the user-supplied operator Secret
//   - auth.conf — the top-level auth fragment that sets `operator:`,
//     `system_account:` and `resolver:` directives and pulls in the
//     resolver preload file
//   - resolver_preload.conf — a nats.conf fragment with the `resolver_preload`
//     block containing every preloaded account's JWT
//
// Keeping the JWT material in a Secret (not the ConfigMap) avoids leaking
// account JWT contents into an unrestricted resource, and the reloader
// sidecar picks up Secret changes automatically so JWT rotations
// propagate without a pod restart.
func buildAuthSecret(cr *natsv1alpha1.NatsCluster, spec *natsv1alpha1.NatsClusterSpec, material *jwtMaterial) *corev1.Secret {
	if spec.Auth.JWT == nil {
		return nil
	}
	jwt := spec.Auth.JWT

	data := map[string][]byte{
		authOperatorJWTFileName: material.operatorJWT,
		authResolverPreloadName: renderResolverPreload(jwt.Accounts, material.accountJWTs),
		authFileName:            renderAuthConf(jwt),
	}

	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      authSecretName(cr),
			Namespace: cr.Namespace,
			Labels:    commonLabels(cr),
		},
		Type: corev1.SecretTypeOpaque,
		Data: data,
	}
}

// renderAuthConf produces the top-level nats.conf fragment the nats
// container `include`s from /etc/nats-auth/auth.conf. The format is
// nats-server HOCON-flavored config, matching the output of the main
// natsconf renderer.
func renderAuthConf(jwt *natsv1alpha1.JWTAuthSpec) []byte {
	root := confBlock{
		"operator":       mountPathAuth + "/" + authOperatorJWTFileName,
		"system_account": jwt.SystemAccount,
		// The resolver_preload file is pulled in via an include directive
		// so the JWT contents stay out of the parent config file.
		includeKeyPrefix + "zzz-resolver-preload": mountPathAuth + "/" + authResolverPreloadName,
	}

	switch jwt.Resolver.Type {
	case natsv1alpha1.JWTResolverFull:
		resolver := confBlock{
			"type": "full",
			"dir":  mountPathResolver,
		}
		if jwt.Resolver.AllowDelete != nil {
			resolver["allow_delete"] = *jwt.Resolver.AllowDelete
		}
		if jwt.Resolver.Interval != "" {
			resolver["interval"] = jwt.Resolver.Interval
		}
		root["resolver"] = resolver
	default:
		// MEMORY is the default and what nats-server expects as a bare
		// keyword in the config, not an object.
		root["resolver"] = confRaw("MEMORY")
	}

	var buf bytes.Buffer
	renderBlockBody(&buf, root, 0)
	return buf.Bytes()
}

// renderResolverPreload produces the `resolver_preload { ... }` block
// with each account JWT inlined as a quoted string. Written to its own
// file so the parent auth.conf can `include` it; that keeps the JWT
// payloads out of anything the user routinely inspects.
func renderResolverPreload(accounts []natsv1alpha1.JWTAccount, jwts map[string][]byte) []byte {
	var buf bytes.Buffer
	buf.WriteString("resolver_preload: {\n")
	// Sort by account name so the output is deterministic across reconciles.
	names := make([]string, 0, len(accounts))
	byName := make(map[string]natsv1alpha1.JWTAccount, len(accounts))
	for _, a := range accounts {
		names = append(names, a.Name)
		byName[a.Name] = a
	}
	sort.Strings(names)
	for _, name := range names {
		account := byName[name]
		jwtBytes := jwts[name]
		fmt.Fprintf(&buf, "  %s: %q\n", account.PublicKey, string(bytes.TrimSpace(jwtBytes)))
	}
	buf.WriteString("}\n")
	return buf.Bytes()
}
