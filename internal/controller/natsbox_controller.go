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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	natsv1alpha1 "github.com/crewlet/nats-operator/api/v1alpha1"
)

// NatsBoxReconciler reconciles a NatsBox object.
type NatsBoxReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=nats.crewlet.cloud,resources=natsboxes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=nats.crewlet.cloud,resources=natsboxes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=nats.crewlet.cloud,resources=natsboxes/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

// Reconcile fetches the NatsBox, applies controller-side defaults, builds
// the rendered contexts Secret + Deployment, and server-side-applies them.
// Status is patched from the live Deployment's replica counts.
func (r *NatsBoxReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	cr := &natsv1alpha1.NatsBox{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	// Snapshot for the final MergeFrom patch; see the NatsCluster
	// reconciler for the rationale.
	beforeStatus := cr.DeepCopy()

	spec := defaultedNatsBox(&cr.Spec)

	contexts := natsBoxResolvedContexts(&spec)
	if err := natsBoxContextsValidate(&spec, contexts); err != nil {
		log.Error(err, "natsbox spec validation failed")
		return ctrl.Result{}, err
	}

	objects := []client.Object{
		buildNatsBoxContextsSecret(cr, &spec),
		buildNatsBoxDeployment(cr, &spec),
	}

	for _, obj := range objects {
		if err := controllerutil.SetControllerReference(cr, obj, r.Scheme); err != nil {
			return ctrl.Result{}, fmt.Errorf("setting owner reference on %T %s: %w", obj, obj.GetName(), err)
		}
		//nolint:staticcheck // SA1019 — see NatsClusterReconciler for migration note.
		if err := r.Patch(ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner(fieldManager)); err != nil {
			return ctrl.Result{}, fmt.Errorf("server-side applying %T %s: %w", obj, obj.GetName(), err)
		}
	}

	if err := r.updateStatus(ctx, cr, beforeStatus, &spec); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *NatsBoxReconciler) updateStatus(ctx context.Context, cr *natsv1alpha1.NatsBox, before *natsv1alpha1.NatsBox, spec *natsv1alpha1.NatsBoxSpec) error {
	dep := &appsv1.Deployment{}
	depKey := client.ObjectKey{Namespace: cr.Namespace, Name: natsBoxDeploymentName(cr)}
	if err := r.Get(ctx, depKey, dep); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	desired := int32(1)
	if spec.Replicas != nil {
		desired = *spec.Replicas
	}

	cr.Status.ObservedGeneration = cr.Generation
	cr.Status.Replicas = dep.Status.Replicas
	cr.Status.ReadyReplicas = dep.Status.ReadyReplicas
	setNatsBoxConditions(cr, dep, desired)

	return r.Status().Patch(ctx, cr, client.MergeFrom(before))
}

func setNatsBoxConditions(cr *natsv1alpha1.NatsBox, dep *appsv1.Deployment, desired int32) {
	now := metav1.Now()
	available := metav1.Condition{
		Type:               "Available",
		LastTransitionTime: now,
		ObservedGeneration: cr.Generation,
	}
	switch {
	case dep.Status.ReadyReplicas == desired:
		available.Status = metav1.ConditionTrue
		available.Reason = "AllReplicasReady"
		available.Message = fmt.Sprintf("%d/%d replicas ready", dep.Status.ReadyReplicas, desired)
	case dep.Status.ReadyReplicas > 0:
		available.Status = metav1.ConditionTrue
		available.Reason = "PartiallyReady"
		available.Message = fmt.Sprintf("%d/%d replicas ready", dep.Status.ReadyReplicas, desired)
	default:
		available.Status = metav1.ConditionFalse
		available.Reason = "NotReady"
		available.Message = "no replicas ready"
	}
	cr.Status.Conditions = upsertCondition(cr.Status.Conditions, available)
}

// SetupWithManager wires the controller and declares Owns on the Deployment
// and the contexts Secret so changes trigger reconcile.
func (r *NatsBoxReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&natsv1alpha1.NatsBox{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Secret{}).
		Named("natsbox").
		Complete(r)
}
