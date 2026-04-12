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

package controller

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	natsv1alpha1 "github.com/crewlet/nats-operator/api/v1alpha1"
)

// fieldManager is the SSA field manager identifier the operator uses on
// every applied object. Distinct from any user / kubectl field manager so
// users can add their own labels/annotations without us stomping them.
const fieldManager = "nats-operator"

// NatsClusterReconciler reconciles a NatsCluster object.
type NatsClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=nats.crewlet.cloud,resources=natsclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=nats.crewlet.cloud,resources=natsclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=nats.crewlet.cloud,resources=natsclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;configmaps;serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=podmonitors,verbs=get;list;watch;create;update;patch;delete

// Reconcile is the main reconcile loop. It fetches the NatsCluster, applies
// controller-side defaults, builds every owned resource from the defaulted
// spec, and server-side-applies them. Status is then patched from the live
// StatefulSet's replica counts.
func (r *NatsClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = logf.FromContext(ctx)

	cr := &natsv1alpha1.NatsCluster{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	spec := defaulted(&cr.Spec)

	// Render nats.conf once and share the bytes between the ConfigMap
	// builder and the StatefulSet builder (which stamps a checksum
	// annotation when configChecksumAnnotation is enabled).
	rendered := renderNatsConf(cr, &spec)

	// Build the desired object set. Each builder returns nil when the
	// resource isn't applicable to the current spec (PDB at single replica,
	// ServiceAccount when disabled, ...).
	objects := []client.Object{}
	if cm := buildConfigMap(cr, rendered); cm != nil {
		objects = append(objects, cm)
	}
	objects = append(objects, buildHeadlessService(cr, &spec))
	if svc := buildClientService(cr, &spec); svc != nil {
		objects = append(objects, svc)
	}
	if sa := buildServiceAccount(cr, &spec); sa != nil {
		objects = append(objects, sa)
	}
	objects = append(objects, buildStatefulSet(cr, &spec, rendered))
	if pdb := buildPDB(cr, &spec); pdb != nil {
		objects = append(objects, pdb)
	}
	if pm := buildPodMonitor(cr, &spec); pm != nil {
		objects = append(objects, pm)
	}
	if ing := buildWebSocketIngress(cr, &spec); ing != nil {
		objects = append(objects, ing)
	}

	for _, obj := range objects {
		if err := controllerutil.SetControllerReference(cr, obj, r.Scheme); err != nil {
			return ctrl.Result{}, fmt.Errorf("setting owner reference on %T %s: %w", obj, obj.GetName(), err)
		}
		// Migrating to client.Client.Apply() requires generated ApplyConfigurations
		// for every type we own (corev1, appsv1, our v1alpha1, ...). Out of scope
		// for v1alpha1 — staying on the patch-based SSA path until then.
		//nolint:staticcheck // SA1019
		if err := r.Patch(ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner(fieldManager)); err != nil {
			return ctrl.Result{}, fmt.Errorf("server-side applying %T %s: %w", obj, obj.GetName(), err)
		}
	}

	if err := r.updateStatus(ctx, cr, &spec); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// updateStatus reads the live StatefulSet and patches the NatsCluster's
// status subresource with current replica counts and the standard
// Available / Progressing / Degraded conditions.
func (r *NatsClusterReconciler) updateStatus(ctx context.Context, cr *natsv1alpha1.NatsCluster, spec *natsv1alpha1.NatsClusterSpec) error {
	sts := &appsv1.StatefulSet{}
	stsKey := client.ObjectKey{Namespace: cr.Namespace, Name: statefulSetName(cr)}
	if err := r.Get(ctx, stsKey, sts); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	desired := int32(1)
	if spec.Replicas != nil {
		desired = *spec.Replicas
	}

	cr.Status.ObservedGeneration = cr.Generation
	cr.Status.Replicas = sts.Status.Replicas
	cr.Status.ReadyReplicas = sts.Status.ReadyReplicas
	cr.Status.ConfigMapName = configMapName(cr)
	cr.Status.Endpoints = clusterEndpoints(cr, spec)
	setConditions(cr, sts, desired)

	return r.Status().Update(ctx, cr)
}

// setConditions stamps the Available / Progressing / Degraded conditions on
// the CR status from the StatefulSet's replica counts. Mirrors the
// convention used by cnpg / prometheus-operator / etc.
func setConditions(cr *natsv1alpha1.NatsCluster, sts *appsv1.StatefulSet, desired int32) {
	now := metav1.Now()
	available := metav1.Condition{
		Type:               "Available",
		LastTransitionTime: now,
		ObservedGeneration: cr.Generation,
	}
	progressing := metav1.Condition{
		Type:               "Progressing",
		LastTransitionTime: now,
		ObservedGeneration: cr.Generation,
	}

	switch {
	case sts.Status.ReadyReplicas == desired && sts.Status.UpdatedReplicas == desired:
		available.Status = metav1.ConditionTrue
		available.Reason = "AllReplicasReady"
		available.Message = fmt.Sprintf("%d/%d replicas ready", sts.Status.ReadyReplicas, desired)
		progressing.Status = metav1.ConditionFalse
		progressing.Reason = "Stable"
	case sts.Status.ReadyReplicas > 0:
		available.Status = metav1.ConditionTrue
		available.Reason = "PartiallyReady"
		available.Message = fmt.Sprintf("%d/%d replicas ready", sts.Status.ReadyReplicas, desired)
		progressing.Status = metav1.ConditionTrue
		progressing.Reason = "Rolling"
	default:
		available.Status = metav1.ConditionFalse
		available.Reason = "NotReady"
		available.Message = "no replicas ready"
		progressing.Status = metav1.ConditionTrue
		progressing.Reason = "Initializing"
	}

	cr.Status.Conditions = upsertCondition(cr.Status.Conditions, available)
	cr.Status.Conditions = upsertCondition(cr.Status.Conditions, progressing)
}

// upsertCondition replaces an existing condition of the same Type or appends
// the new one. Preserves LastTransitionTime when the status didn't actually
// change so we don't churn the timestamp on every reconcile.
func upsertCondition(in []metav1.Condition, c metav1.Condition) []metav1.Condition {
	for i, existing := range in {
		if existing.Type != c.Type {
			continue
		}
		if existing.Status == c.Status {
			c.LastTransitionTime = existing.LastTransitionTime
		}
		in[i] = c
		return in
	}
	return append(in, c)
}

// SetupWithManager wires the controller to the manager and declares the
// owned resource types so changes to them trigger reconciles for the
// parent NatsCluster. PodMonitor is intentionally not in the Owns list —
// the operator owns it via SSA but doesn't need to react to its events.
func (r *NatsClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&natsv1alpha1.NatsCluster{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&policyv1.PodDisruptionBudget{}).
		Owns(&networkingv1.Ingress{}).
		Named("natscluster").
		Complete(r)
}
