package controller

import (
	. "github.com/onsi/ginkgo/v2"
<<<<<<< HEAD
=======
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	natsv1alpha1 "github.com/crewlet/nats-operator/api/v1alpha1"
>>>>>>> tmp-original-05-05-26-03-23
)

var _ = Describe("NatsBox Controller", func() {
	Context("When reconciling a resource", func() {
<<<<<<< HEAD

		It("should successfully reconcile the resource", func() {

			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
=======
		const resourceName = "test-natsbox"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("creating the custom resource for the Kind NatsBox")
			resource := &natsv1alpha1.NatsBox{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
			}
			err := k8sClient.Get(ctx, typeNamespacedName, &natsv1alpha1.NatsBox{})
			if err != nil && apierrors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &natsv1alpha1.NatsBox{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance NatsBox")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should create the owned Deployment, contexts Secret and stamp status", func() {
			By("Reconciling the created resource")
			controllerReconciler := &NatsBoxReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			cr := &natsv1alpha1.NatsBox{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, cr)).To(Succeed())

			By("creating the contexts Secret")
			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Namespace: typeNamespacedName.Namespace,
				Name:      natsBoxContextsSecretName(cr),
			}, secret)).To(Succeed())
			Expect(secret.OwnerReferences).To(HaveLen(1))
			Expect(secret.OwnerReferences[0].Name).To(Equal(resourceName))
			Expect(secret.OwnerReferences[0].Kind).To(Equal("NatsBox"))

			By("creating the Deployment with the defaulted replica count")
			dep := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Namespace: typeNamespacedName.Namespace,
				Name:      natsBoxDeploymentName(cr),
			}, dep)).To(Succeed())
			Expect(dep.Spec.Replicas).NotTo(BeNil())
			Expect(*dep.Spec.Replicas).To(Equal(int32(1)))
			Expect(dep.OwnerReferences).To(HaveLen(1))
			Expect(dep.OwnerReferences[0].Name).To(Equal(resourceName))

			By("patching status with observedGeneration and an Available condition")
			Expect(cr.Status.ObservedGeneration).To(Equal(cr.Generation))
			// envtest has no kubelet, so the Deployment has 0 ready replicas.
			available := findCondition(cr.Status.Conditions, "Available")
			Expect(available).NotTo(BeNil())
			Expect(available.Status).To(Equal(metav1.ConditionFalse))
			Expect(available.Reason).To(Equal("NotReady"))
		})

		It("should be idempotent across reconciles", func() {
			controllerReconciler := &NatsBoxReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
>>>>>>> tmp-original-05-05-26-03-23
		})
	})
})
