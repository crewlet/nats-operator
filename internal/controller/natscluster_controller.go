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
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	jsv1beta2 "github.com/nats-io/nack/pkg/jetstream/apis/jetstream/v1beta2"

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

	// EnableNackIntegration controls whether the reconciler creates NACK
	// `jetstream.nats.io/v1beta2` Account CRs for auth.jwt.accounts[]
	// entries with userCreds set, and whether SetupWithManager registers
	// an Owns watch on NACK Account. When false, the integration is fully
	// skipped — the condition is not stamped, the watch is not registered,
	// and the operator does not touch the NACK API group at all. Resolved
	// from the --nack-integration flag in main.go (auto / enabled / disabled).
	EnableNackIntegration bool
}

// +kubebuilder:rbac:groups=nats.crewlet.cloud,resources=natsclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=nats.crewlet.cloud,resources=natsclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=nats.crewlet.cloud,resources=natsclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;configmaps;serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=podmonitors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=jetstream.nats.io,resources=accounts,verbs=get;list;watch;create;update;patch;delete

// Reconcile is the main reconcile loop. It fetches the NatsCluster, applies
// controller-side defaults, builds every owned resource from the defaulted
// spec, and server-side-applies them. Status is then patched from the live
// StatefulSet's replica counts.
func (r *NatsClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	cr := &natsv1alpha1.NatsCluster{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	// Snapshot the CR before any mutation so the final status patch can
	// compute a minimal merge patch that captures every change made
	// during this reconcile (NACK conditions + replica counts + endpoints).
	// This also means the patch does not require a matching
	// resourceVersion, so unrelated concurrent updates to the CR do not
	// race our status write.
	beforeStatus := cr.DeepCopy()

	spec := defaulted(&cr.Spec)

	// Resolve the JWT auth material — operator JWT and account JWTs — up
	// front so the auth Secret builder stays pure. If any referenced user
	// Secret is missing we surface a status condition and requeue.
	material, err := r.resolveJWTMaterial(ctx, cr, &spec)
	if err != nil {
		return ctrl.Result{}, err
	}

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
	if authSecret := buildAuthSecret(cr, &spec, material); authSecret != nil {
		objects = append(objects, authSecret)
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

	// NACK integration. We SSA one NACK Account CR per jwt.accounts[]
	// entry that has userCreds set. The NACK CRD may not be installed in
	// the cluster — in that case we degrade gracefully: stamp a status
	// condition, requeue with backoff, keep the rest of the NatsCluster
	// healthy.
	requeue, nackErr := r.applyNackAccounts(ctx, cr, &spec, log)
	if nackErr != nil {
		return ctrl.Result{}, nackErr
	}

	if err := r.updateStatus(ctx, cr, beforeStatus, &spec); err != nil {
		return ctrl.Result{}, err
	}

	if requeue > 0 {
		return ctrl.Result{RequeueAfter: requeue}, nil
	}
	return ctrl.Result{}, nil
}

// resolveJWTMaterial fetches the raw operator JWT and per-account JWT bytes
// from the Secrets the user referenced in spec.auth.jwt. Returns nil when
// the JWT path is not configured. Returns an error on any Secret that is
// missing or missing the referenced key — the reconciler surfaces this as
// a failed reconcile, which in turn retries via the standard backoff.
func (r *NatsClusterReconciler) resolveJWTMaterial(ctx context.Context, cr *natsv1alpha1.NatsCluster, spec *natsv1alpha1.NatsClusterSpec) (*jwtMaterial, error) {
	if spec.Auth.JWT == nil {
		return nil, nil
	}
	jwt := spec.Auth.JWT

	operatorJWT, err := r.readSecretKey(ctx, cr.Namespace, jwt.Operator)
	if err != nil {
		return nil, fmt.Errorf("reading operator JWT from secret %q: %w", jwt.Operator.Name, err)
	}

	accountJWTs := make(map[string][]byte, len(jwt.Accounts))
	for _, account := range jwt.Accounts {
		body, err := r.readSecretKey(ctx, cr.Namespace, account.JWT)
		if err != nil {
			return nil, fmt.Errorf("reading account %q JWT from secret %q: %w", account.Name, account.JWT.Name, err)
		}
		accountJWTs[account.Name] = body
	}
	return &jwtMaterial{operatorJWT: operatorJWT, accountJWTs: accountJWTs}, nil
}

// readSecretKey is a small helper that fetches a Secret and returns the
// bytes at the given key, or a descriptive error.
func (r *NatsClusterReconciler) readSecretKey(ctx context.Context, namespace string, ref corev1.SecretKeySelector) ([]byte, error) {
	s := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: ref.Name}, s); err != nil {
		return nil, err
	}
	body, ok := s.Data[ref.Key]
	if !ok || len(body) == 0 {
		return nil, fmt.Errorf("secret %q has no data under key %q", ref.Name, ref.Key)
	}
	return body, nil
}

// applyNackAccounts SSA-applies one NACK Account CR per jwt.accounts[]
// entry with userCreds set. Returns a non-zero RequeueAfter when the NACK
// CRD is not installed in the cluster, so the operator retries later
// without the user having to edit the NatsCluster.
func (r *NatsClusterReconciler) applyNackAccounts(ctx context.Context, cr *natsv1alpha1.NatsCluster, spec *natsv1alpha1.NatsClusterSpec, log interface {
	Info(msg string, keysAndValues ...any)
}) (time.Duration, error) {
	// Fast exit when the integration is turned off at the operator level.
	// No condition is stamped — "disabled by flag" is not a failure to
	// surface, it's a deliberate config choice.
	if !r.EnableNackIntegration {
		return 0, nil
	}

	accounts := buildNackAccounts(cr, spec)
	if len(accounts) == 0 {
		// No NACK integration requested — clear any stale condition.
		r.setNackCondition(cr, metav1.ConditionTrue, "NotRequested", "no accounts opted in to NACK integration")
		return 0, nil
	}

	for _, account := range accounts {
		if err := controllerutil.SetControllerReference(cr, account, r.Scheme); err != nil {
			return 0, fmt.Errorf("setting owner reference on NACK Account %s: %w", account.Name, err)
		}
		//nolint:staticcheck // SA1019 — see note on client.Apply above.
		if err := r.Patch(ctx, account, client.Apply, client.ForceOwnership, client.FieldOwner(fieldManager)); err != nil {
			if meta.IsNoMatchError(err) || apierrors.IsNotFound(err) {
				// NACK CRD not installed. Surface a condition and retry
				// later — the operator will pick up the integration once
				// NACK is installed, without the user touching anything.
				r.setNackCondition(cr, metav1.ConditionFalse, "NackCRDNotInstalled",
					"jetstream.nats.io/v1beta2 Account CRD is not installed; install NACK to enable automated account wiring")
				log.Info("NACK CRD not installed, deferring NACK integration", "account", account.Name)
				return 60 * time.Second, nil
			}
			return 0, fmt.Errorf("server-side applying NACK Account %s: %w", account.Name, err)
		}
	}

	r.setNackCondition(cr, metav1.ConditionTrue, "Applied",
		fmt.Sprintf("%d NACK Account CR(s) in sync", len(accounts)))
	return 0, nil
}

// setNackCondition stamps the NackIntegrationAvailable condition on the
// cluster's status. updateStatus is responsible for actually pushing the
// status subresource — this helper only mutates the in-memory CR.
func (r *NatsClusterReconciler) setNackCondition(cr *natsv1alpha1.NatsCluster, status metav1.ConditionStatus, reason, message string) {
	cr.Status.Conditions = upsertCondition(cr.Status.Conditions, metav1.Condition{
		Type:               "NackIntegrationAvailable",
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: cr.Generation,
	})
}

// updateStatus reads the live StatefulSet and patches the NatsCluster's
// status subresource with current replica counts and the standard
// Available / Progressing / Degraded conditions.
//
// Uses MergeFrom patch semantics rather than Update so we don't race
// against the `resourceVersion` — the CR can be bumped between our Get
// at the top of Reconcile and this status write (Owns events, user
// edits, SSA side-effects) without us needing a refetch-and-retry loop.
// The `before` snapshot must be taken at the top of Reconcile, before
// any mutation to cr.Status, so the computed merge patch captures every
// change made during the whole reconcile pass (including NACK
// conditions stamped by applyNackAccounts).
func (r *NatsClusterReconciler) updateStatus(ctx context.Context, cr *natsv1alpha1.NatsCluster, before *natsv1alpha1.NatsCluster, spec *natsv1alpha1.NatsClusterSpec) error {
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

	return r.Status().Patch(ctx, cr, client.MergeFrom(before))
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
//
// The NACK Account ownership is only registered when EnableNackIntegration
// is true. controller-runtime's informer does NOT tolerate missing CRDs
// gracefully: a missing kind floods the operator logs with
// "no matches for kind" errors on every polling interval. The
// --nack-integration flag in main.go auto-detects the CRD at startup (or
// lets the user force the mode), and the reconciler only calls Owns()
// here when the integration is actually supposed to run.
func (r *NatsClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr).
		For(&natsv1alpha1.NatsCluster{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&policyv1.PodDisruptionBudget{}).
		Owns(&networkingv1.Ingress{}).
		Named("natscluster")

	if r.EnableNackIntegration {
		b = b.Owns(&jsv1beta2.Account{})
	}

	return b.Complete(r)
}
