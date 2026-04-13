/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	jsv1beta2 "github.com/nats-io/nack/pkg/jetstream/apis/jetstream/v1beta2"

	natsv1alpha1 "github.com/crewlet/nats-operator/api/v1alpha1"
)

// buildNackAccounts returns one NACK `jetstream.nats.io/v1beta2` Account CR
// per entry in spec.auth.jwt.accounts[] that has userCreds set. Each CR
// points at the cluster's client Service URL and the user-supplied creds
// Secret, so NACK Stream / Consumer / KV / ObjectStore CRs can then
// reference `account: <natscluster-name>-<account.name>` without repeating
// connection info on every resource.
//
// Returns nil when auth.jwt is unset or no account has userCreds — the
// reconciler uses that to skip the NACK integration entirely.
func buildNackAccounts(cr *natsv1alpha1.NatsCluster, spec *natsv1alpha1.NatsClusterSpec) []*jsv1beta2.Account {
	if spec.Auth.JWT == nil {
		return nil
	}
	// Resolve the connection URL once — every rendered account points at
	// the same cluster. Prefer the client Service, fall back to headless
	// for the (unusual) case where the client Service is disabled.
	url := clusterEndpoints(cr, spec).Client
	if url == "" {
		url = clusterEndpoints(cr, spec).Headless
	}
	if url == "" {
		return nil
	}

	var out []*jsv1beta2.Account
	for _, account := range spec.Auth.JWT.Accounts {
		if account.UserCreds == nil {
			continue
		}
		out = append(out, &jsv1beta2.Account{
			TypeMeta: metav1.TypeMeta{
				APIVersion: jsv1beta2.SchemeGroupVersion.String(),
				Kind:       "Account",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      nackAccountName(cr, account.Name),
				Namespace: cr.Namespace,
				Labels: map[string]string{
					labelAppName:                 "nats-account",
					labelAppInstance:             cr.Name,
					labelAppManaged:              managedByValue,
					labelAppPartOf:               appNameValue,
					"nats.crewlet.cloud/cluster": cr.Name,
					"nats.crewlet.cloud/account": account.Name,
				},
			},
			Spec: jsv1beta2.AccountSpec{
				Servers: []string{url},
				Creds: &jsv1beta2.CredsSecret{
					Secret: &jsv1beta2.SecretRef{Name: account.UserCreds.Name},
					File:   account.UserCreds.Key,
				},
			},
		})
	}
	return out
}
