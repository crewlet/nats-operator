/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"fmt"
	"maps"

	natsv1alpha1 "github.com/crewlet/nats-operator/api/v1alpha1"
)

// natsBoxAppName is the value used for app.kubernetes.io/name on every
// resource the operator creates for a NatsBox. Distinct from the NatsCluster
// app name so a single NatsCluster + NatsBox pair are differentiated by
// label selectors and don't accidentally pick up each other's pods.
const natsBoxAppName = "nats-box"

// Resource name suffixes for NatsBox-owned objects.
const (
	natsBoxSuffixContexts = "-contexts"
)

// Mount paths inside the nats-box container. The chart sets
// XDG_CONFIG_HOME=/etc/nats-config and the nats CLI then reads its
// contexts from $XDG_CONFIG_HOME/nats/context/*.json.
const (
	natsBoxConfigHome   = "/etc/nats-config"
	natsBoxContextsDir  = "/etc/nats-config/nats/context"
	natsBoxContextPtr   = "/etc/nats-config/nats/context.txt"
	natsBoxCredsDirRoot = "/etc/nats-creds"
	natsBoxNKeysDirRoot = "/etc/nats-nkeys"
	natsBoxCertsDirRoot = "/etc/nats-certs"
	natsBoxCADirRoot    = "/etc/nats-ca-cert"
	natsBoxContextsVol  = "contexts"
)

// natsBoxLabels is the canonical label set the operator stamps on every
// resource for a NatsBox. Mirrors commonLabels for NatsCluster but uses
// app.kubernetes.io/name=nats-box so selectors don't collide with the
// nats server pods if a user names a NatsCluster and NatsBox identically.
func natsBoxLabels(cr *natsv1alpha1.NatsBox) map[string]string {
	return map[string]string{
		labelAppName:     natsBoxAppName,
		labelAppInstance: cr.Name,
		labelAppManaged:  managedByValue,
		labelAppPartOf:   appNameValue,
		labelSelectorKey: cr.Name,
	}
}

// natsBoxSelectorLabels returns the selector subset for the Deployment
// pod template. Must never change for a given NatsBox.
func natsBoxSelectorLabels(cr *natsv1alpha1.NatsBox) map[string]string {
	return map[string]string{
		labelAppName:     natsBoxAppName,
		labelAppInstance: cr.Name,
		labelSelectorKey: cr.Name,
	}
}

func natsBoxDeploymentName(cr *natsv1alpha1.NatsBox) string {
	return cr.Name
}

func natsBoxContextsSecretName(cr *natsv1alpha1.NatsBox) string {
	return cr.Name + natsBoxSuffixContexts
}

// natsBoxContextCredsPath returns the in-pod path where a context's creds
// file is mounted (one directory per context name to keep mounts isolated).
func natsBoxContextCredsPath(contextName string) string {
	return fmt.Sprintf("%s/%s/nats.creds", natsBoxCredsDirRoot, contextName)
}

func natsBoxContextNKeyPath(contextName string) string {
	return fmt.Sprintf("%s/%s/nats.nk", natsBoxNKeysDirRoot, contextName)
}

func natsBoxContextCertPath(contextName string) string {
	return fmt.Sprintf("%s/%s/tls.crt", natsBoxCertsDirRoot, contextName)
}

func natsBoxContextKeyPath(contextName string) string {
	return fmt.Sprintf("%s/%s/tls.key", natsBoxCertsDirRoot, contextName)
}

func natsBoxContextCAPath(contextName string) string {
	return fmt.Sprintf("%s/%s/ca.crt", natsBoxCADirRoot, contextName)
}

// defaultedNatsBox returns a copy of the spec with controller-side defaults
// applied. Same pattern as defaulted() for NatsCluster — the rest of the
// builder code works on the normalized copy.
func defaultedNatsBox(in *natsv1alpha1.NatsBoxSpec) natsv1alpha1.NatsBoxSpec {
	out := in.DeepCopy()

	if out.Replicas == nil {
		out.Replicas = ptr(int32(1))
	}
	defaultImage(&out.Image, defaultNatsBoxImage)

	if out.DefaultContextName == "" {
		out.DefaultContextName = "default"
	}

	return *out
}

// natsBoxResolvedContexts returns the contexts the operator should render,
// including the auto-generated "default" context derived from clusterRef
// when one isn't already present in the user-supplied map.
func natsBoxResolvedContexts(spec *natsv1alpha1.NatsBoxSpec) map[string]natsv1alpha1.NatsBoxContext {
	out := make(map[string]natsv1alpha1.NatsBoxContext, len(spec.Contexts)+1)
	maps.Copy(out, spec.Contexts)
	if spec.ClusterRef != nil {
		if _, exists := out["default"]; !exists {
			out["default"] = natsv1alpha1.NatsBoxContext{}
		}
	}
	return out
}

// natsBoxContextURL returns the nats:// URL for a context. When the user
// has not provided one, falls back to the headless Service of the NatsCluster
// referenced by clusterRef.
func natsBoxContextURL(cr *natsv1alpha1.NatsBox, ctx natsv1alpha1.NatsBoxContext) string {
	if ctx.URL != "" {
		return ctx.URL
	}
	if cr.Spec.ClusterRef == nil {
		return ""
	}
	// Construct the URL from the headless Service the NatsCluster operator
	// creates: <cluster-name>-headless.<ns>.svc:4222
	return fmt.Sprintf("nats://%s-headless.%s.svc:%d",
		cr.Spec.ClusterRef.Name, cr.Namespace, defaultNatsPort)
}
