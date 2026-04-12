/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	natsv1alpha1 "github.com/crewlet/nats-operator/api/v1alpha1"
)

// buildPDB returns the PodDisruptionBudget. Returns nil when explicitly
// disabled or when there is only one replica (a PDB with maxUnavailable=0
// on a single pod would block all voluntary disruptions and break drains).
func buildPDB(cr *natsv1alpha1.NatsCluster, spec *natsv1alpha1.NatsClusterSpec) *policyv1.PodDisruptionBudget {
	if !isTrue(spec.PodDisruptionBudget.Enabled) {
		return nil
	}
	if spec.Replicas == nil || *spec.Replicas < 2 {
		return nil
	}

	pdbSpec := spec.PodDisruptionBudget.PodDisruptionBudgetSpec
	pdbSpec.Selector = &metav1.LabelSelector{MatchLabels: selectorLabels(cr)}

	// Default to maxUnavailable=1 when the user hasn't expressed either
	// minAvailable or maxUnavailable. The CEL rule on the type already
	// rejects setting both.
	if pdbSpec.MinAvailable == nil && pdbSpec.MaxUnavailable == nil {
		one := intstr.FromInt32(1)
		pdbSpec.MaxUnavailable = &one
	}

	return &policyv1.PodDisruptionBudget{
		TypeMeta: metav1.TypeMeta{APIVersion: "policy/v1", Kind: "PodDisruptionBudget"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        pdbName(cr),
			Namespace:   cr.Namespace,
			Labels:      mergeUserLabels(commonLabels(cr), spec.PodDisruptionBudget.Labels),
			Annotations: spec.PodDisruptionBudget.Annotations,
		},
		Spec: pdbSpec,
	}
}
