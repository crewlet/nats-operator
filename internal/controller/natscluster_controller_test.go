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

var _ = Describe("NatsCluster Controller", func() {
	Context("When reconciling a resource", func() {
<<<<<<< HEAD

		It("should successfully reconcile the resource", func() {

			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
=======
		const resourceName = "test-natscluster"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("creating the custom resource for the Kind NatsCluster")
			resource := &natsv1alpha1.NatsCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
			}
			err := k8sClient.Get(ctx, typeNamespacedName, &natsv1alpha1.NatsCluster{})
			if err != nil && apierrors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &natsv1alpha1.NatsCluster{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance NatsCluster")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should create the owned resources and stamp status", func() {
			By("Reconciling the created resource")
			controllerReconciler := &NatsClusterReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			cr := &natsv1alpha1.NatsCluster{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, cr)).To(Succeed())

			By("creating the rendered config ConfigMap")
			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Namespace: typeNamespacedName.Namespace,
				Name:      configMapName(cr),
			}, cm)).To(Succeed())
			Expect(cm.Data).To(HaveKey(natsConfFileName))
			Expect(cm.Labels).To(HaveKeyWithValue(labelAppName, appNameValue))
			Expect(cm.Labels).To(HaveKeyWithValue(labelAppInstance, resourceName))
			Expect(cm.Labels).To(HaveKeyWithValue(labelAppManaged, managedByValue))
			Expect(cm.OwnerReferences).To(HaveLen(1))
			Expect(cm.OwnerReferences[0].Name).To(Equal(resourceName))
			Expect(cm.OwnerReferences[0].Kind).To(Equal("NatsCluster"))

			By("creating the headless Service")
			headless := &corev1.Service{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Namespace: typeNamespacedName.Namespace,
				Name:      headlessServiceName(cr),
			}, headless)).To(Succeed())
			Expect(headless.Spec.ClusterIP).To(Equal(corev1.ClusterIPNone))
			Expect(headless.Spec.Selector).To(HaveKeyWithValue(labelSelectorKey, resourceName))

			By("creating the client Service (default enabled)")
			clientSvc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Namespace: typeNamespacedName.Namespace,
				Name:      clientServiceName(cr),
			}, clientSvc)).To(Succeed())
			Expect(clientSvc.Spec.Selector).To(HaveKeyWithValue(labelSelectorKey, resourceName))

			By("creating the StatefulSet with the defaulted replica count")
			sts := &appsv1.StatefulSet{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Namespace: typeNamespacedName.Namespace,
				Name:      statefulSetName(cr),
			}, sts)).To(Succeed())
			Expect(sts.Spec.Replicas).NotTo(BeNil())
			Expect(*sts.Spec.Replicas).To(Equal(int32(1)))
			Expect(sts.Spec.ServiceName).To(Equal(headlessServiceName(cr)))
			Expect(sts.Spec.Selector.MatchLabels).To(HaveKeyWithValue(labelSelectorKey, resourceName))

			By("patching status with observedGeneration, endpoints and conditions")
			Expect(cr.Status.ObservedGeneration).To(Equal(cr.Generation))
			Expect(cr.Status.ConfigMapName).To(Equal(configMapName(cr)))
			Expect(cr.Status.Endpoints.Headless).NotTo(BeEmpty())
			Expect(cr.Status.Endpoints.Client).NotTo(BeEmpty())
			// envtest has no kubelet so the STS has 0 ready replicas —
			// Available=False/NotReady and Progressing=True/Initializing.
			available := findCondition(cr.Status.Conditions, "Available")
			Expect(available).NotTo(BeNil())
			Expect(available.Status).To(Equal(metav1.ConditionFalse))
			Expect(available.Reason).To(Equal("NotReady"))
			progressing := findCondition(cr.Status.Conditions, "Progressing")
			Expect(progressing).NotTo(BeNil())
			Expect(progressing.Status).To(Equal(metav1.ConditionTrue))
		})

		It("should be idempotent across reconciles", func() {
			controllerReconciler := &NatsClusterReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// Two back-to-back reconciles must not error; SSA is a no-op
			// when the desired state already matches.
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
>>>>>>> tmp-original-05-05-26-03-23
		})
	})
})

// findCondition returns a pointer to the condition with the given type, or
// nil if none is present. Inlined here to keep the test file self-contained.
func findCondition(conds []metav1.Condition, t string) *metav1.Condition {
	for i := range conds {
		if conds[i].Type == t {
			return &conds[i]
		}
	}
	return nil
}
