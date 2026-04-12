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

// buildHeadlessService returns the headless Service the StatefulSet uses
// for stable per-pod DNS. Always created, regardless of whether the
// client-facing Service is enabled, because the StatefulSet selector
// depends on it.
func buildHeadlessService(cr *natsv1alpha1.NatsCluster, spec *natsv1alpha1.NatsClusterSpec) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        headlessServiceName(cr),
			Namespace:   cr.Namespace,
			Labels:      mergeUserLabels(commonLabels(cr), cr.Spec.HeadlessService.Labels),
			Annotations: cr.Spec.HeadlessService.Annotations,
		},
		Spec: corev1.ServiceSpec{
			Type:                     corev1.ServiceTypeClusterIP,
			ClusterIP:                corev1.ClusterIPNone,
			PublishNotReadyAddresses: true,
			Selector:                 selectorLabels(cr),
			Ports:                    headlessServicePorts(spec),
		},
	}
}

// headlessServicePorts publishes every enabled listener so the per-pod DNS
// records resolve consistently — including the cluster routing port and
// the monitor port, which the operator deliberately keeps off the
// client-facing Service.
func headlessServicePorts(spec *natsv1alpha1.NatsClusterSpec) []corev1.ServicePort {
	ports := []corev1.ServicePort{
		{Name: "nats", Port: spec.Config.Nats.Port, Protocol: corev1.ProtocolTCP},
	}
	if spec.Replicas != nil && *spec.Replicas > 1 {
		ports = append(ports, corev1.ServicePort{
			Name: "cluster", Port: spec.Config.Cluster.Port, Protocol: corev1.ProtocolTCP,
		})
	}
	if isTrue(spec.Config.Monitor.Enabled) {
		ports = append(ports, corev1.ServicePort{
			Name: "monitor", Port: spec.Config.Monitor.Port, Protocol: corev1.ProtocolTCP,
		})
	}
	if spec.Config.LeafNodes.Enabled {
		ports = append(ports, corev1.ServicePort{
			Name: "leafnodes", Port: spec.Config.LeafNodes.Port, Protocol: corev1.ProtocolTCP,
		})
	}
	if spec.Config.WebSocket.Enabled {
		ports = append(ports, corev1.ServicePort{
			Name: "websocket", Port: spec.Config.WebSocket.Port, Protocol: corev1.ProtocolTCP,
		})
	}
	if spec.Config.MQTT.Enabled {
		ports = append(ports, corev1.ServicePort{
			Name: "mqtt", Port: spec.Config.MQTT.Port, Protocol: corev1.ProtocolTCP,
		})
	}
	if spec.Config.Gateway.Enabled {
		ports = append(ports, corev1.ServicePort{
			Name: "gateway", Port: spec.Config.Gateway.Port, Protocol: corev1.ProtocolTCP,
		})
	}
	if spec.Config.Profiling.Enabled {
		ports = append(ports, corev1.ServicePort{
			Name: "profiling", Port: spec.Config.Profiling.Port, Protocol: corev1.ProtocolTCP,
		})
	}
	if spec.PromExporter.Enabled {
		ports = append(ports, corev1.ServicePort{
			Name: "metrics", Port: spec.PromExporter.Port, Protocol: corev1.ProtocolTCP,
		})
	}
	return ports
}
