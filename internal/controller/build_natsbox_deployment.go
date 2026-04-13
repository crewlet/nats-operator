/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	natsv1alpha1 "github.com/crewlet/nats-operator/api/v1alpha1"
)

// buildNatsBoxDeployment returns the Deployment running the nats-box pod.
// The pod's container is started with `tail -f /dev/null` so it stays alive
// for kubectl exec sessions, with all referenced credential and TLS material
// mounted at the predictable per-context paths the rendered contexts secret
// points at.
func buildNatsBoxDeployment(cr *natsv1alpha1.NatsBox, spec *natsv1alpha1.NatsBoxSpec) *appsv1.Deployment {
	contexts := natsBoxResolvedContexts(spec)
	names := sortedContextNames(contexts)

	volumes := buildNatsBoxVolumes(cr, contexts, names)
	mounts := buildNatsBoxMounts(contexts, names)

	podLabels := mergeUserLabels(natsBoxLabels(cr), spec.PodTemplate.Labels)

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      natsBoxDeploymentName(cr),
			Namespace: cr.Namespace,
			Labels:    natsBoxLabels(cr),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: spec.Replicas,
			Selector: &metav1.LabelSelector{MatchLabels: natsBoxSelectorLabels(cr)},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      podLabels,
					Annotations: spec.PodTemplate.Annotations,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName:            spec.ServiceAccountName,
					NodeSelector:                  spec.PodTemplate.NodeSelector,
					Tolerations:                   spec.PodTemplate.Tolerations,
					Affinity:                      spec.PodTemplate.Affinity,
					TopologySpreadConstraints:     overrideNatsBoxTopologySelector(cr, spec.PodTemplate.TopologySpreadConstraints),
					PriorityClassName:             spec.PodTemplate.PriorityClassName,
					RuntimeClassName:              spec.PodTemplate.RuntimeClassName,
					TerminationGracePeriodSeconds: spec.PodTemplate.TerminationGracePeriodSeconds,
					DNSPolicy:                     spec.PodTemplate.DNSPolicy,
					DNSConfig:                     spec.PodTemplate.DNSConfig,
					HostAliases:                   spec.PodTemplate.HostAliases,
					SecurityContext:               spec.PodTemplate.SecurityContext,
					ImagePullSecrets:              spec.PodTemplate.ImagePullSecrets,
					Volumes:                       volumes,
					Containers: []corev1.Container{
						buildNatsBoxContainer(spec, mounts),
					},
				},
			},
		},
	}
}

func buildNatsBoxContainer(spec *natsv1alpha1.NatsBoxSpec, mounts []corev1.VolumeMount) corev1.Container {
	return corev1.Container{
		Name:            "nats-box",
		Image:           imageRef(spec.Image),
		ImagePullPolicy: spec.Image.PullPolicy,
		// Keep the container alive so kubectl exec sessions have something
		// to attach to. nats-box images do not run a long-lived server.
		Command: []string{"sh", "-c", "tail -f /dev/null"},
		Env: []corev1.EnvVar{
			{Name: "XDG_CONFIG_HOME", Value: natsBoxConfigHome},
		},
		Resources:    spec.Resources,
		VolumeMounts: mounts,
	}
}

// buildNatsBoxVolumes returns the volume list for the pod: the rendered
// contexts Secret plus one volume per referenced creds / nkey / TLS / CA
// Secret across all contexts. We deduplicate by Secret name so two contexts
// pointing at the same Secret don't double-mount.
func buildNatsBoxVolumes(cr *natsv1alpha1.NatsBox, contexts map[string]natsv1alpha1.NatsBoxContext, names []string) []corev1.Volume {
	// Project the contexts Secret into the layout the nats CLI expects:
	//
	//   $XDG_CONFIG_HOME/nats/context.txt         ← default context name
	//   $XDG_CONFIG_HOME/nats/context/<name>.json ← per-context definitions
	//
	// Secret data keys can't contain "/", so we store them flat
	// (context.txt, <name>.json) and use Items.Path to place them
	// under the right subdirectory when the Secret is mounted. The
	// Volume is then mounted at natsBoxNatsDir — one level UP from
	// the `context` subdir so context.txt lands at the right place.
	contextItems := make([]corev1.KeyToPath, 0, 1+len(names))
	contextItems = append(contextItems, corev1.KeyToPath{Key: "context.txt", Path: "context.txt"})
	for _, name := range names {
		contextItems = append(contextItems, corev1.KeyToPath{
			Key:  name + ".json",
			Path: "context/" + name + ".json",
		})
	}

	vols := []corev1.Volume{
		{
			Name: natsBoxContextsVol,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: natsBoxContextsSecretName(cr),
					Items:      contextItems,
				},
			},
		},
	}

	// Each context's referenced material gets its own per-context volume so
	// the in-pod path matches what the rendered context JSON points at. Per
	// context (not per Secret) is intentional: it keeps the mount paths
	// stable when the same Secret backs multiple contexts and lets us use
	// SubPath to project a single key.
	for _, name := range names {
		ctx := contexts[name]
		if ctx.Creds != nil {
			vols = append(vols, corev1.Volume{
				Name: "creds-" + sanitizeVolumeName(name),
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: ctx.Creds.Name,
						Items: []corev1.KeyToPath{
							{Key: ctx.Creds.Key, Path: "nats.creds"},
						},
					},
				},
			})
		}
		if ctx.NKey != nil {
			vols = append(vols, corev1.Volume{
				Name: "nkey-" + sanitizeVolumeName(name),
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: ctx.NKey.Name,
						Items: []corev1.KeyToPath{
							{Key: ctx.NKey.Key, Path: "nats.nk"},
						},
					},
				},
			})
		}
		if ctx.TLS != nil {
			vols = append(vols, corev1.Volume{
				Name: "tls-" + sanitizeVolumeName(name),
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{SecretName: ctx.TLS.Name},
				},
			})
		}
		if ctx.CA != nil {
			switch {
			case ctx.CA.Secret != nil:
				vols = append(vols, corev1.Volume{
					Name: "ca-" + sanitizeVolumeName(name),
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: ctx.CA.Secret.Name,
							Items: []corev1.KeyToPath{
								{Key: ctx.CA.Secret.Key, Path: "ca.crt"},
							},
						},
					},
				})
			case ctx.CA.ConfigMap != nil:
				vols = append(vols, corev1.Volume{
					Name: "ca-" + sanitizeVolumeName(name),
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: ctx.CA.ConfigMap.Name},
							Items: []corev1.KeyToPath{
								{Key: ctx.CA.ConfigMap.Key, Path: "ca.crt"},
							},
						},
					},
				})
			}
		}
	}
	return vols
}

func buildNatsBoxMounts(contexts map[string]natsv1alpha1.NatsBoxContext, names []string) []corev1.VolumeMount {
	// Mount the contexts Secret at $XDG_CONFIG_HOME/nats (natsBoxNatsDir).
	// The volume's Items.Path projections (see buildNatsBoxVolumes) place
	// context.txt at the mount root and each <name>.json under the
	// `context/` subdirectory — matching the nats CLI's native layout.
	mounts := []corev1.VolumeMount{
		{
			Name:      natsBoxContextsVol,
			MountPath: natsBoxNatsDir,
			ReadOnly:  true,
		},
	}
	for _, name := range names {
		ctx := contexts[name]
		if ctx.Creds != nil {
			mounts = append(mounts, corev1.VolumeMount{
				Name:      "creds-" + sanitizeVolumeName(name),
				MountPath: natsBoxCredsDirRoot + "/" + name,
				ReadOnly:  true,
			})
		}
		if ctx.NKey != nil {
			mounts = append(mounts, corev1.VolumeMount{
				Name:      "nkey-" + sanitizeVolumeName(name),
				MountPath: natsBoxNKeysDirRoot + "/" + name,
				ReadOnly:  true,
			})
		}
		if ctx.TLS != nil {
			mounts = append(mounts, corev1.VolumeMount{
				Name:      "tls-" + sanitizeVolumeName(name),
				MountPath: natsBoxCertsDirRoot + "/" + name,
				ReadOnly:  true,
			})
		}
		if ctx.CA != nil && (ctx.CA.Secret != nil || ctx.CA.ConfigMap != nil) {
			mounts = append(mounts, corev1.VolumeMount{
				Name:      "ca-" + sanitizeVolumeName(name),
				MountPath: natsBoxCADirRoot + "/" + name,
				ReadOnly:  true,
			})
		}
	}
	return mounts
}

// overrideNatsBoxTopologySelector mirrors overrideTopologySelector for
// NatsCluster: any user-supplied labelSelector is replaced with the
// canonical pod selector so the constraint actually targets nats-box pods.
func overrideNatsBoxTopologySelector(cr *natsv1alpha1.NatsBox, in []corev1.TopologySpreadConstraint) []corev1.TopologySpreadConstraint {
	if len(in) == 0 {
		return nil
	}
	out := make([]corev1.TopologySpreadConstraint, len(in))
	for i, c := range in {
		c.LabelSelector = &metav1.LabelSelector{MatchLabels: natsBoxSelectorLabels(cr)}
		out[i] = c
	}
	return out
}
