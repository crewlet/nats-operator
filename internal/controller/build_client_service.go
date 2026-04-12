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
	"k8s.io/apimachinery/pkg/util/intstr"

	natsv1alpha1 "github.com/crewlet/nats-operator/api/v1alpha1"
)

// buildClientService returns the client-facing Service. Returns nil when
// service.enabled is explicitly false. The operator decides which listener
// ports to expose: nats is always published; leafnodes / websocket / mqtt /
// gateway are exposed iff enabled; cluster / monitor / profiling are kept
// internal-only on the headless Service.
func buildClientService(cr *natsv1alpha1.NatsCluster, spec *natsv1alpha1.NatsClusterSpec) *corev1.Service {
	if !isTrue(spec.Service.Enabled) {
		return nil
	}

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        clientServiceName(cr),
			Namespace:   cr.Namespace,
			Labels:      mergeUserLabels(commonLabels(cr), spec.Service.Labels),
			Annotations: spec.Service.Annotations,
		},
		Spec: corev1.ServiceSpec{
			Type:                  spec.Service.Type,
			LoadBalancerClass:     spec.Service.LoadBalancerClass,
			ExternalTrafficPolicy: spec.Service.ExternalTrafficPolicy,
			Selector:              selectorLabels(cr),
			Ports:                 clientServicePorts(spec),
		},
	}
}

// clientServicePorts walks the listener configs and returns the published
// ServicePort list. NodePort overrides come from spec.service.nodePorts.
func clientServicePorts(spec *natsv1alpha1.NatsClusterSpec) []corev1.ServicePort {
	type listener struct {
		name string
		port int32
	}
	var listeners []listener
	listeners = append(listeners, listener{"nats", spec.Config.Nats.Port})
	if spec.Config.LeafNodes.Enabled {
		listeners = append(listeners, listener{"leafnodes", spec.Config.LeafNodes.Port})
	}
	if spec.Config.WebSocket.Enabled {
		listeners = append(listeners, listener{"websocket", spec.Config.WebSocket.Port})
	}
	if spec.Config.MQTT.Enabled {
		listeners = append(listeners, listener{"mqtt", spec.Config.MQTT.Port})
	}
	if spec.Config.Gateway.Enabled {
		listeners = append(listeners, listener{"gateway", spec.Config.Gateway.Port})
	}

	out := make([]corev1.ServicePort, 0, len(listeners))
	for _, l := range listeners {
		p := corev1.ServicePort{
			Name:       l.name,
			Port:       l.port,
			TargetPort: intstr.FromInt32(l.port),
			Protocol:   corev1.ProtocolTCP,
		}
		if np, ok := spec.Service.NodePorts[l.name]; ok {
			p.NodePort = np
		}
		out = append(out, p)
	}
	return out
}
