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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	natsv1alpha1 "github.com/crewlet/nats-operator/api/v1alpha1"
)

// Test fixtures. Every builder/renderer unit test starts from one of
// these and layers on functional options so the setup of each case
// stays under ~5 lines. Keeping this in a *_test.go file means the
// helpers do not bloat the production binary.

const (
	testNamespace = "default"
	testName      = "test"
	testBoxName   = "test-box"
)

// clusterOption mutates a NatsCluster spec in place.
type clusterOption func(*natsv1alpha1.NatsCluster)

// newNatsCluster returns a minimal NatsCluster with the given name and
// applies the options in order. The default state has no replicas,
// no JetStream, and no listeners enabled — callers opt in explicitly.
func newNatsCluster(opts ...clusterOption) *natsv1alpha1.NatsCluster {
	cr := &natsv1alpha1.NatsCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: natsv1alpha1.GroupVersion.String(),
			Kind:       "NatsCluster",
		},
		ObjectMeta: metav1.ObjectMeta{Name: testName, Namespace: testNamespace},
	}
	for _, o := range opts {
		o(cr)
	}
	return cr
}

func withReplicas(n int32) clusterOption {
	return func(cr *natsv1alpha1.NatsCluster) { cr.Spec.Replicas = ptr(n) }
}

func withImage(repo, tag string) clusterOption {
	return func(cr *natsv1alpha1.NatsCluster) {
		cr.Spec.Image.Repository = repo
		cr.Spec.Image.Tag = tag
	}
}

func withJetStream() clusterOption {
	return func(cr *natsv1alpha1.NatsCluster) {
		cr.Spec.Config.JetStream.Enabled = true
		cr.Spec.Config.JetStream.FileStore.PVC.PersistentVolumeClaimSpec = corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("1Gi"),
				},
			},
		}
	}
}

func withLeafNodes(port int32) clusterOption {
	return func(cr *natsv1alpha1.NatsCluster) {
		cr.Spec.Config.LeafNodes.Enabled = true
		cr.Spec.Config.LeafNodes.Port = port
	}
}

func withMQTT(port int32) clusterOption {
	return func(cr *natsv1alpha1.NatsCluster) {
		cr.Spec.Config.MQTT.Enabled = true
		cr.Spec.Config.MQTT.Port = port
	}
}

func withGateway(port int32) clusterOption {
	return func(cr *natsv1alpha1.NatsCluster) {
		cr.Spec.Config.Gateway.Enabled = true
		cr.Spec.Config.Gateway.Port = port
	}
}

func withWebSocket() clusterOption {
	return func(cr *natsv1alpha1.NatsCluster) {
		cr.Spec.Config.WebSocket.Enabled = true
	}
}

func withProfiling(port int32) clusterOption {
	return func(cr *natsv1alpha1.NatsCluster) {
		cr.Spec.Config.Profiling.Enabled = true
		cr.Spec.Config.Profiling.Port = port
	}
}

func withNatsTLS(secretName string) clusterOption {
	return func(cr *natsv1alpha1.NatsCluster) {
		cr.Spec.Config.Nats.TLS.Enabled = true
		cr.Spec.Config.Nats.TLS.SecretName = secretName
	}
}

func withPromExporter() clusterOption {
	return func(cr *natsv1alpha1.NatsCluster) {
		cr.Spec.PromExporter.Enabled = true
	}
}

func withPodMonitor() clusterOption {
	return func(cr *natsv1alpha1.NatsCluster) {
		cr.Spec.PromExporter.Enabled = true
		cr.Spec.PromExporter.PodMonitor.Enabled = true
	}
}

func withServiceAccount() clusterOption {
	return func(cr *natsv1alpha1.NatsCluster) {
		cr.Spec.ServiceAccount.Enabled = ptr(true)
	}
}

func withWebSocketIngress(hosts ...string) clusterOption {
	return func(cr *natsv1alpha1.NatsCluster) {
		cr.Spec.Config.WebSocket.Enabled = true
		cr.Spec.Config.WebSocket.Ingress.Enabled = true
		cr.Spec.Config.WebSocket.Ingress.Hosts = hosts
	}
}

func withConfigChecksumAnnotation() clusterOption {
	return func(cr *natsv1alpha1.NatsCluster) {
		cr.Spec.PodTemplate.ConfigChecksumAnnotation = true
	}
}

func withJWTAuth(operatorSecret string, accounts ...natsv1alpha1.JWTAccount) clusterOption {
	return func(cr *natsv1alpha1.NatsCluster) {
		cr.Spec.Auth.JWT = &natsv1alpha1.JWTAuthSpec{
			Operator: corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: operatorSecret},
				Key:                  "operator.jwt",
			},
			SystemAccount: "AASYS",
			Accounts:      accounts,
		}
	}
}

// jwtAccount builds a JWTAccount with the given handles. Optional
// variadic callback lets the caller override e.g. userCreds in place.
func jwtAccount(name, pubkey, jwtSecret string, opts ...func(*natsv1alpha1.JWTAccount)) natsv1alpha1.JWTAccount {
	a := natsv1alpha1.JWTAccount{
		Name:      name,
		PublicKey: pubkey,
		JWT: corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: jwtSecret},
			Key:                  "account.jwt",
		},
	}
	for _, o := range opts {
		o(&a)
	}
	return a
}

func withAccountUserCreds(secretName, key string) func(*natsv1alpha1.JWTAccount) {
	return func(a *natsv1alpha1.JWTAccount) {
		a.UserCreds = &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
			Key:                  key,
		}
	}
}

// --------- NatsBox fixtures ---------

type boxOption func(*natsv1alpha1.NatsBox)

func newNatsBox(opts ...boxOption) *natsv1alpha1.NatsBox {
	cr := &natsv1alpha1.NatsBox{
		TypeMeta: metav1.TypeMeta{
			APIVersion: natsv1alpha1.GroupVersion.String(),
			Kind:       "NatsBox",
		},
		ObjectMeta: metav1.ObjectMeta{Name: testBoxName, Namespace: testNamespace},
	}
	for _, o := range opts {
		o(cr)
	}
	return cr
}

func withBoxClusterRef() boxOption {
	return func(cr *natsv1alpha1.NatsBox) {
		cr.Spec.ClusterRef = &corev1.LocalObjectReference{Name: "my-cluster"}
	}
}

func withBoxContext(name string, ctx natsv1alpha1.NatsBoxContext) boxOption {
	return func(cr *natsv1alpha1.NatsBox) {
		if cr.Spec.Contexts == nil {
			cr.Spec.Contexts = map[string]natsv1alpha1.NatsBoxContext{}
		}
		cr.Spec.Contexts[name] = ctx
	}
}
