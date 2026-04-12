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
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	natsv1alpha1 "github.com/crewlet/nats-operator/api/v1alpha1"
)

// buildNatsBoxContextsSecret returns the Secret containing the rendered nats
// CLI context JSON files. Each context becomes <name>.json under the Secret
// keys, and a context.txt file selects the default context. The Secret is
// mounted into the nats-box pod at $XDG_CONFIG_HOME/nats/context/.
func buildNatsBoxContextsSecret(cr *natsv1alpha1.NatsBox, spec *natsv1alpha1.NatsBoxSpec) *corev1.Secret {
	contexts := natsBoxResolvedContexts(spec)

	data := make(map[string][]byte, len(contexts)+1)
	for name, ctx := range contexts {
		entry := buildContextJSON(cr, name, ctx)
		body, _ := json.MarshalIndent(entry, "", "  ")
		data[name+".json"] = body
	}
	// context.txt selects the default context for the nats CLI.
	data["context.txt"] = []byte(spec.DefaultContextName)

	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      natsBoxContextsSecretName(cr),
			Namespace: cr.Namespace,
			Labels:    natsBoxLabels(cr),
		},
		Type: corev1.SecretTypeOpaque,
		Data: data,
	}
}

// natsContextFile is the on-disk JSON shape the nats CLI reads from
// $XDG_CONFIG_HOME/nats/context/<name>.json. We only model the fields the
// operator currently fills in.
type natsContextFile struct {
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
	Creds       string `json:"creds,omitempty"`
	NKey        string `json:"nkey,omitempty"`
	Cert        string `json:"cert,omitempty"`
	Key         string `json:"key,omitempty"`
	CA          string `json:"ca,omitempty"`
}

func buildContextJSON(cr *natsv1alpha1.NatsBox, name string, ctx natsv1alpha1.NatsBoxContext) natsContextFile {
	out := natsContextFile{
		Description: ctx.Description,
		URL:         natsBoxContextURL(cr, ctx),
	}
	if ctx.Creds != nil {
		out.Creds = natsBoxContextCredsPath(name)
	}
	if ctx.NKey != nil {
		out.NKey = natsBoxContextNKeyPath(name)
	}
	if ctx.TLS != nil {
		out.Cert = natsBoxContextCertPath(name)
		out.Key = natsBoxContextKeyPath(name)
	}
	if ctx.CA != nil && (ctx.CA.Secret != nil || ctx.CA.ConfigMap != nil) {
		out.CA = natsBoxContextCAPath(name)
	}
	return out
}

// sortedContextNames returns the deterministic list of context names so the
// Deployment volumes / volume mounts come out in stable order across
// reconciles. Used by the Deployment builder.
func sortedContextNames(contexts map[string]natsv1alpha1.NatsBoxContext) []string {
	out := make([]string, 0, len(contexts))
	for k := range contexts {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// natsBoxContextsValidate runs cheap pre-flight checks on the rendered
// context set. Returns a non-nil error when defaultContextName references a
// context that doesn't exist — but only when there is at least one context
// to choose from. An empty contexts map is a valid (if minimally useful)
// spec: the box still runs and the user can `nats context add` interactively.
func natsBoxContextsValidate(spec *natsv1alpha1.NatsBoxSpec, contexts map[string]natsv1alpha1.NatsBoxContext) error {
	if len(contexts) == 0 {
		return nil
	}
	if _, ok := contexts[spec.DefaultContextName]; !ok {
		return fmt.Errorf("defaultContextName %q is not present in the resolved context set", spec.DefaultContextName)
	}
	return nil
}
