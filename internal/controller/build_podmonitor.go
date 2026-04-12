/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	natsv1alpha1 "github.com/crewlet/nats-operator/api/v1alpha1"
)

// podMonitorGVK is the GroupVersionKind for prometheus-operator's PodMonitor.
// We use unstructured.Unstructured rather than importing the typed schema to
// avoid pulling the prometheus-operator dependency tree into nats-operator
// for an optional integration.
var podMonitorGVK = schema.GroupVersionKind{
	Group:   "monitoring.coreos.com",
	Version: "v1",
	Kind:    "PodMonitor",
}

// buildPodMonitor returns a PodMonitor scraping the prometheus exporter
// sidecar's metrics port. Returns nil when promExporter or its podMonitor
// sub-block is not enabled.
func buildPodMonitor(cr *natsv1alpha1.NatsCluster, spec *natsv1alpha1.NatsClusterSpec) *unstructured.Unstructured {
	if !spec.PromExporter.Enabled || !spec.PromExporter.PodMonitor.Enabled {
		return nil
	}

	endpoint := map[string]any{
		"port": "metrics",
	}
	if spec.PromExporter.PodMonitor.Interval != "" {
		endpoint["interval"] = spec.PromExporter.PodMonitor.Interval
	}
	if spec.PromExporter.PodMonitor.ScrapeTimeout != "" {
		endpoint["scrapeTimeout"] = spec.PromExporter.PodMonitor.ScrapeTimeout
	}

	pm := &unstructured.Unstructured{}
	pm.SetGroupVersionKind(podMonitorGVK)
	pm.SetName(podMonitorName(cr))
	pm.SetNamespace(cr.Namespace)
	pm.SetLabels(mergeUserLabels(commonLabels(cr), spec.PromExporter.PodMonitor.Labels))

	pm.Object["spec"] = map[string]any{
		"selector": map[string]any{
			"matchLabels": stringMap(selectorLabels(cr)),
		},
		"podMetricsEndpoints": []any{endpoint},
	}
	return pm
}

// stringMap converts a typed map to map[string]any so it round-trips
// through unstructured.Unstructured without losing entries.
func stringMap(in map[string]string) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
