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

// buildConfigMap returns the ConfigMap holding the rendered nats.conf for
// the given NatsCluster. Skipped (returns nil) when the user has supplied
// configMap.existingName, in which case the operator must not own the CM.
// The rendered bytes are passed in so the reconciler renders once and shares
// the result with the StatefulSet builder (which stamps a checksum on the
// pod template when configChecksumAnnotation is enabled).
func buildConfigMap(cr *natsv1alpha1.NatsCluster, rendered []byte) *corev1.ConfigMap {
	if cr.Spec.ConfigMap.ExistingName != "" {
		return nil
	}
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        configMapName(cr),
			Namespace:   cr.Namespace,
			Labels:      mergeUserLabels(commonLabels(cr), cr.Spec.ConfigMap.Labels),
			Annotations: cr.Spec.ConfigMap.Annotations,
		},
		Data: map[string]string{
			natsConfFileName: string(rendered),
		},
	}
}
