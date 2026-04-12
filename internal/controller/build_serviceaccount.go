/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	natsv1alpha1 "github.com/crewlet/nats-operator/api/v1alpha1"
)

// buildServiceAccount returns the ServiceAccount the StatefulSet pods run
// as. Returns nil when serviceAccount.enabled is false (or unset) — in
// that case the StatefulSet inherits the namespace default.
func buildServiceAccount(cr *natsv1alpha1.NatsCluster, spec *natsv1alpha1.NatsClusterSpec) *corev1.ServiceAccount {
	if !isTrue(spec.ServiceAccount.Enabled) {
		return nil
	}
	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ServiceAccount"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        serviceAccountName(cr),
			Namespace:   cr.Namespace,
			Labels:      commonLabels(cr),
			Annotations: spec.ServiceAccount.Annotations,
		},
		ImagePullSecrets: spec.ServiceAccount.ImagePullSecrets,
	}
}
