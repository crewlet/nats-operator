/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NatsBoxSpec defines the desired state of a NatsBox — a long-running
// utility pod with the nats CLI preinstalled and one or more NATS contexts
// pre-configured. Mirrors the natsBox sub-chart from the upstream nats-io/k8s
// helm release: a Deployment running natsio/nats-box, with contexts files
// generated from the spec and credential / TLS material mounted from Secrets.
type NatsBoxSpec struct {
	// replicas is the number of nats-box pods. Defaults to 1.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// image is the nats-box container image. Defaults to natsio/nats-box.
	// +optional
	Image ImageSpec `json:"image,omitzero"`

	// resources sets the nats-box container resource requests/limits.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitzero"`

	// clusterRef references a NatsCluster in the same namespace. When set,
	// the operator auto-generates a context named "default" with its URL
	// derived from the cluster's headless Service. Combine with `contexts`
	// to add credentials / TLS material.
	// +optional
	ClusterRef *corev1.LocalObjectReference `json:"clusterRef,omitempty"`

	// contexts is a map of nats CLI contexts the operator renders into the
	// nats-box pod. The map key is the context name (used as the file name
	// under /etc/nats-config/nats/context/<name>.json). When clusterRef is
	// set and "default" is not present in this map, the operator auto-fills
	// it from the referenced cluster.
	// +optional
	Contexts map[string]NatsBoxContext `json:"contexts,omitempty"`

	// defaultContextName selects which context the nats CLI uses by default.
	// Must match a key in `contexts` or be "default" when clusterRef is set.
	// Defaults to "default".
	// +optional
	DefaultContextName string `json:"defaultContextName,omitempty"`

	// serviceAccountName, when set, is used as the Deployment pods'
	// ServiceAccount. The operator does not create or manage the
	// ServiceAccount — users bring their own.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// podTemplate customizes the Deployment pod template.
	// +optional
	PodTemplate PodTemplateSpec `json:"podTemplate,omitzero"`
}

// NatsBoxContext describes a single nats CLI context. Credentials are
// referenced from Secrets and mounted into the pod by the operator — the
// rendered context JSON points at the resulting in-pod paths.
type NatsBoxContext struct {
	// url overrides the default URL. When omitted and the parent NatsBox has
	// clusterRef set, the URL is derived from the referenced cluster's
	// headless Service.
	// +optional
	URL string `json:"url,omitempty"`

	// description is a human-readable description forwarded into the
	// rendered context JSON.
	// +optional
	Description string `json:"description,omitempty"`

	// creds references a Secret key holding a NATS user credentials (JWT) file.
	// +optional
	Creds *corev1.SecretKeySelector `json:"creds,omitempty"`

	// nkey references a Secret key holding an NKey file.
	// +optional
	NKey *corev1.SecretKeySelector `json:"nkey,omitempty"`

	// tls references a kubernetes.io/tls Secret used for mutual TLS.
	// +optional
	TLS *corev1.LocalObjectReference `json:"tls,omitempty"`

	// ca references a Secret or ConfigMap key holding a CA bundle to verify
	// the nats server certificate against.
	// +optional
	CA *TLSCASpec `json:"ca,omitempty"`
}

// NatsBoxStatus defines the observed state of NatsBox.
type NatsBoxStatus struct {
	// observedGeneration is the .metadata.generation last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// replicas is the total number of nats-box pods.
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// readyReplicas is the number of nats-box pods reported ready.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// conditions represent the current state of the NatsBox resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=nb
// +kubebuilder:printcolumn:name="Replicas",type=integer,JSONPath=`.status.replicas`
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=`.status.readyReplicas`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// NatsBox is the Schema for the natsboxes API
type NatsBox struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of NatsBox
	// +required
	Spec NatsBoxSpec `json:"spec"`

	// status defines the observed state of NatsBox
	// +optional
	Status NatsBoxStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// NatsBoxList contains a list of NatsBox
type NatsBoxList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []NatsBox `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NatsBox{}, &NatsBoxList{})
}
