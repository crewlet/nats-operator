/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	natsv1alpha1 "github.com/crewlet/nats-operator/api/v1alpha1"
)

// buildWebSocketIngress returns the Ingress fronting the websocket listener
// when both the listener and its ingress are enabled. Returns nil otherwise.
// CEL on WebSocketIngress already enforces hosts being non-empty when
// enabled, so we trust the slice here.
func buildWebSocketIngress(cr *natsv1alpha1.NatsCluster, spec *natsv1alpha1.NatsClusterSpec) *networkingv1.Ingress {
	if !spec.Config.WebSocket.Enabled || !spec.Config.WebSocket.Ingress.Enabled {
		return nil
	}
	in := spec.Config.WebSocket.Ingress

	pathType := networkingv1.PathTypeExact
	if in.PathType != "" {
		pathType = networkingv1.PathType(in.PathType)
	}
	path := in.Path
	if path == "" {
		path = "/"
	}

	rules := make([]networkingv1.IngressRule, 0, len(in.Hosts))
	for _, host := range in.Hosts {
		rules = append(rules, networkingv1.IngressRule{
			Host: host,
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{
						{
							Path:     path,
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: clientServiceName(cr),
									Port: networkingv1.ServiceBackendPort{
										Number: spec.Config.WebSocket.Port,
									},
								},
							},
						},
					},
				},
			},
		})
	}

	var tls []networkingv1.IngressTLS
	if in.TLSSecretName != "" {
		tls = []networkingv1.IngressTLS{
			{Hosts: in.Hosts, SecretName: in.TLSSecretName},
		}
	}

	return &networkingv1.Ingress{
		TypeMeta: metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "Ingress"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        ingressName(cr),
			Namespace:   cr.Namespace,
			Labels:      commonLabels(cr),
			Annotations: in.Annotations,
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: stringOrNil(in.ClassName),
			TLS:              tls,
			Rules:            rules,
		},
	}
}

func stringOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
