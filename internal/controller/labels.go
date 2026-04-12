/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"maps"

	natsv1alpha1 "github.com/crewlet/nats-operator/api/v1alpha1"
)

// Canonical label keys. The operator owns these on every managed resource —
// user-supplied labels can be added but never override these. The selector
// label is what every Service / PDB / PodMonitor / StatefulSet selects on, so
// changing it across versions would orphan resources.
const (
	labelAppName      = "app.kubernetes.io/name"
	labelAppInstance  = "app.kubernetes.io/instance"
	labelAppManaged   = "app.kubernetes.io/managed-by"
	labelAppPartOf    = "app.kubernetes.io/part-of"
	labelAppComponent = "app.kubernetes.io/component"

	// labelSelectorKey is the canonical selector key used by every selector
	// belonging to a NatsCluster. Two NatsClusters in the same namespace get
	// distinct values, which keeps Service / PDB / PodMonitor selectors from
	// matching the wrong pods.
	labelSelectorKey = "nats.crewlet.cloud/cluster"

	managedByValue = "nats-operator"
	appNameValue   = "nats"
)

// commonLabels returns the canonical label set the operator stamps on every
// managed resource for the given NatsCluster. Callers that need to add
// user-supplied labels should pass the result through mergeUserLabels.
func commonLabels(cr *natsv1alpha1.NatsCluster) map[string]string {
	return map[string]string{
		labelAppName:     appNameValue,
		labelAppInstance: cr.Name,
		labelAppManaged:  managedByValue,
		labelAppPartOf:   appNameValue,
		labelSelectorKey: cr.Name,
	}
}

// selectorLabels returns the minimal label set used as the StatefulSet,
// Service, PDB, and PodMonitor selectors. Selectors are immutable on most
// resource types, so this set must never change across operator versions
// for a given NatsCluster.
func selectorLabels(cr *natsv1alpha1.NatsCluster) map[string]string {
	return map[string]string{
		labelAppName:     appNameValue,
		labelAppInstance: cr.Name,
		labelSelectorKey: cr.Name,
	}
}

// mergeUserLabels returns a new map containing the canonical labels with
// the user-supplied labels overlaid — except canonical keys always win.
// User labels that target a canonical key are silently ignored to keep the
// operator-managed selectors intact.
func mergeUserLabels(canonical, user map[string]string) map[string]string {
	out := make(map[string]string, len(canonical)+len(user))
	for k, v := range user {
		if _, reserved := canonical[k]; reserved {
			continue
		}
		out[k] = v
	}
	maps.Copy(out, canonical)
	return out
}

// mergeAnnotations returns a new map containing the union of base and user
// annotations. User entries take precedence — annotations are not used as
// selectors and are safe to override.
func mergeAnnotations(base, user map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(user))
	maps.Copy(out, base)
	maps.Copy(out, user)
	return out
}
