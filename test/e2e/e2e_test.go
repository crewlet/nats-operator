//go:build e2e
// +build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/crewlet/nats-operator/test/utils"
)

// namespace where the project is deployed in
const namespace = "nats-operator-system"

// serviceAccountName created for the project
const serviceAccountName = "nats-operator-controller-manager"

// metricsServiceName is the name of the metrics service of the project
const metricsServiceName = "nats-operator-controller-manager-metrics-service"

// metricsRoleBindingName is the name of the RBAC that will be created to allow get the metrics data
const metricsRoleBindingName = "nats-operator-metrics-binding"

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string

	// Before running the tests, set up the environment by creating the namespace,
	// enforce the restricted security policy to the namespace, installing CRDs,
	// and deploying the controller.
	BeforeAll(func() {
		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", namespace)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")

		By("labeling the namespace to enforce the restricted security policy")
		cmd = exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
			"pod-security.kubernetes.io/enforce=restricted")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label namespace with restricted policy")

		By("installing CRDs")
		cmd = exec.Command("make", "install")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("deploying the controller-manager")
		cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", managerImage))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")

		By("creating the NATS e2e test namespace")
		cmd = exec.Command("kubectl", "create", "ns", testNamespace)
		_, err = utils.Run(cmd)
		// Recreate-on-reuse is fine — the AfterAll blows it away.
		if err != nil && !strings.Contains(err.Error(), "AlreadyExists") {
			Expect(err).NotTo(HaveOccurred())
		}
	})

	// After all tests have been executed, clean up by undeploying the controller, uninstalling CRDs,
	// and deleting the namespace.
	AfterAll(func() {
		By("removing the NATS e2e test namespace")
		cmd := exec.Command("kubectl", "delete", "ns", testNamespace, "--ignore-not-found", "--wait=false")
		_, _ = utils.Run(cmd)

		By("cleaning up the curl pod for metrics")
		cmd = exec.Command("kubectl", "delete", "pod", "curl-metrics", "-n", namespace)
		_, _ = utils.Run(cmd)

		By("undeploying the controller-manager")
		cmd = exec.Command("make", "undeploy")
		_, _ = utils.Run(cmd)

		By("uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)

		By("removing manager namespace")
		cmd = exec.Command("kubectl", "delete", "ns", namespace)
		_, _ = utils.Run(cmd)
	})

	// After each test, check for failures and collect logs, events,
	// and pod descriptions for debugging.
	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			By("Fetching controller manager pod logs")
			cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
			controllerLogs, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", controllerLogs)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Controller logs: %s", err)
			}

			By("Fetching Kubernetes events")
			cmd = exec.Command("kubectl", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
			eventsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes events:\n%s", eventsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Kubernetes events: %s", err)
			}

			By("Fetching curl-metrics logs")
			cmd = exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
			metricsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Metrics logs:\n %s", metricsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get curl-metrics logs: %s", err)
			}

			By("Fetching controller manager pod description")
			cmd = exec.Command("kubectl", "describe", "pod", controllerPodName, "-n", namespace)
			podDescription, err := utils.Run(cmd)
			if err == nil {
				fmt.Println("Pod description:\n", podDescription)
			} else {
				fmt.Println("Failed to describe controller pod")
			}
		}
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	Context("Manager", func() {
		It("should run successfully", func() {
			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func(g Gomega) {
				By("getting the name of the controller-manager pod")
				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)

				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(1), "expected 1 controller pod running")
				controllerPodName = podNames[0]
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				By("validating the pod's status")
				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "Incorrect controller-manager pod status")
			}
			Eventually(verifyControllerUp).Should(Succeed())
		})

		It("should ensure the metrics endpoint is serving metrics", func() {
			By("creating a ClusterRoleBinding for the service account to allow access to metrics")
			cmd := exec.Command("kubectl", "create", "clusterrolebinding", metricsRoleBindingName,
				"--clusterrole=nats-operator-metrics-reader",
				fmt.Sprintf("--serviceaccount=%s:%s", namespace, serviceAccountName),
			)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create ClusterRoleBinding")

			By("validating that the metrics service is available")
			cmd = exec.Command("kubectl", "get", "service", metricsServiceName, "-n", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Metrics service should exist")

			By("getting the service account token")
			token, err := serviceAccountToken()
			Expect(err).NotTo(HaveOccurred())
			Expect(token).NotTo(BeEmpty())

			By("ensuring the controller pod is ready")
			verifyControllerPodReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pod", controllerPodName, "-n", namespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"), "Controller pod not ready")
			}
			Eventually(verifyControllerPodReady, 3*time.Minute, time.Second).Should(Succeed())

			By("verifying that the controller manager is serving the metrics server")
			verifyMetricsServerStarted := func(g Gomega) {
				cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("Serving metrics server"),
					"Metrics server not yet started")
			}
			Eventually(verifyMetricsServerStarted, 3*time.Minute, time.Second).Should(Succeed())

			// +kubebuilder:scaffold:e2e-metrics-webhooks-readiness

			By("creating the curl-metrics pod to access the metrics endpoint")
			cmd = exec.Command("kubectl", "run", "curl-metrics", "--restart=Never",
				"--namespace", namespace,
				"--image=curlimages/curl:latest",
				"--overrides",
				fmt.Sprintf(`{
					"spec": {
						"containers": [{
							"name": "curl",
							"image": "curlimages/curl:latest",
							"command": ["/bin/sh", "-c"],
							"args": [
								"for i in $(seq 1 30); do curl -v -k -H 'Authorization: Bearer %s' https://%s.%s.svc.cluster.local:8443/metrics && exit 0 || sleep 2; done; exit 1"
							],
							"securityContext": {
								"readOnlyRootFilesystem": true,
								"allowPrivilegeEscalation": false,
								"capabilities": {
									"drop": ["ALL"]
								},
								"runAsNonRoot": true,
								"runAsUser": 1000,
								"seccompProfile": {
									"type": "RuntimeDefault"
								}
							}
						}],
						"serviceAccountName": "%s"
					}
				}`, token, metricsServiceName, namespace, serviceAccountName))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create curl-metrics pod")

			By("waiting for the curl-metrics pod to complete.")
			verifyCurlUp := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "curl-metrics",
					"-o", "jsonpath={.status.phase}",
					"-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Succeeded"), "curl pod in wrong status")
			}
			Eventually(verifyCurlUp, 5*time.Minute).Should(Succeed())

			By("getting the metrics by checking curl-metrics logs")
			verifyMetricsAvailable := func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve logs from curl pod")
				g.Expect(metricsOutput).NotTo(BeEmpty())
				g.Expect(metricsOutput).To(ContainSubstring("< HTTP/1.1 200 OK"))
			}
			Eventually(verifyMetricsAvailable, 2*time.Minute).Should(Succeed())
		})

		// +kubebuilder:scaffold:e2e-webhooks-checks
	})

	// The Contexts below exercise the NATS deployment shapes the
	// operator is expected to support, and verify them from NATS's
	// perspective — not just "the object shape is right" but "NATS
	// actually behaves like NATS." Each scenario applies a
	// NatsCluster + NatsBox pair, waits for Available=True, and
	// drives the `nats` CLI / NATS monitor endpoints via exec into
	// the nats-box pod to prove the deployment works end to end.
	//
	// Contexts are Ordered so per-scenario BeforeAll / AfterAll stage
	// fixtures and tear them down individually — keeping failures
	// isolated without paying for a namespace per spec.

	// ---------------------------------------------------------------
	// Scenario 1: minimal single-replica NatsCluster + NatsBox.
	// Verifies the golden path: operator stands up a single NATS
	// server, nats-box attaches via its auto-generated default context,
	// and a pub/sub round-trip succeeds. This is a regression guard
	// for the `context.txt` mount-path bug where context.txt landed
	// under context/context.txt and the nats CLI silently fell back
	// to nats://127.0.0.1:4222.
	// ---------------------------------------------------------------
	Context("single-replica minimal", Ordered, func() {
		const (
			clusterName = "e2e-solo"
			boxName     = "e2e-solo-box"
		)
		manifest := fmt.Sprintf(`
apiVersion: nats.crewlet.cloud/v1alpha1
kind: NatsCluster
metadata:
  name: %s
spec:
  replicas: 1
---
apiVersion: nats.crewlet.cloud/v1alpha1
kind: NatsBox
metadata:
  name: %s
spec:
  clusterRef:
    name: %s
`, clusterName, boxName, clusterName)

		BeforeAll(func() {
			By("applying minimal NatsCluster + NatsBox")
			kubectlApplyStdin(manifest)
		})
		AfterAll(func() {
			By("deleting minimal fixtures")
			kubectlDeleteStdin(manifest)
		})

		It("reports Available=True", func() {
			waitForNatsClusterAvailable(clusterName, 3*time.Minute)
		})

		It("nats-box can connect via the auto-generated default context", func() {
			pod := waitForNatsBoxPod(boxName, 2*time.Minute)

			// `nats server check connection` exits non-zero if the
			// default context cannot reach a NATS server — this is
			// the single cleanest signal that the context file and
			// mount path are both right.
			Eventually(func(g Gomega) {
				out, err := execNatsBox(pod, "nats", "server", "check", "connection")
				g.Expect(err).NotTo(HaveOccurred(), "nats check failed:\n%s", out)
			}, 90*time.Second, 3*time.Second).Should(Succeed())
		})

		It("pub/sub round-trips through the single replica", func() {
			pod := waitForNatsBoxPod(boxName, 30*time.Second)

			// Publish a message and read it back with `nats sub --count=1`.
			// We use a shell pipeline inside the container so the
			// subscribe is running before the publish lands, and we
			// time out the subscribe itself rather than kubectl exec.
			shell := "nats sub --count=1 --timeout=20s e2e.solo & sleep 1 && nats pub e2e.solo hello-solo && wait"
			out, err := execNatsBox(pod, "sh", "-c", shell)
			Expect(err).NotTo(HaveOccurred(), "pub/sub round-trip failed:\n%s", out)
			Expect(out).To(ContainSubstring("hello-solo"))
		})
	})

	// ---------------------------------------------------------------
	// Scenario 2: 3-replica clustered NatsCluster.
	// Verifies route formation end to end. This is the regression guard
	// for the HOCON `routes [...]` vs `routes = [...]` bug: the server
	// silently parsed the bare form as a no-op, so TCP handshakes
	// still worked but the /routez endpoint reported num_routes=0.
	// We assert every pod has num_routes >= 2, then publish on one
	// pod and subscribe on another to prove subject propagation.
	// ---------------------------------------------------------------
	Context("three-replica clustered", Ordered, func() {
		const (
			clusterName = "e2e-trio"
			boxName     = "e2e-trio-box"
		)
		manifest := fmt.Sprintf(`
apiVersion: nats.crewlet.cloud/v1alpha1
kind: NatsCluster
metadata:
  name: %s
spec:
  replicas: 3
---
apiVersion: nats.crewlet.cloud/v1alpha1
kind: NatsBox
metadata:
  name: %s
spec:
  clusterRef:
    name: %s
`, clusterName, boxName, clusterName)

		BeforeAll(func() {
			By("applying 3-replica NatsCluster + NatsBox")
			kubectlApplyStdin(manifest)
		})
		AfterAll(func() {
			By("deleting 3-replica fixtures")
			kubectlDeleteStdin(manifest)
		})

		It("reports Available=True with readyReplicas=3", func() {
			waitForNatsClusterAvailable(clusterName, 5*time.Minute)
			waitForReadyReplicas(clusterName, 3, 30*time.Second)
		})

		It("every pod has its route mesh formed (num_routes >= 2)", func() {
			// Regression: the renderer emitted `routes [...]` which
			// nats-server silently parsed as a path expression, not
			// an array — so /routez showed num_routes=0 even though
			// /varz looked fine. Assert against every pod, not just
			// pod 0, so a partial mesh can't slip through.
			pod := waitForNatsBoxPod(boxName, 2*time.Minute)

			Eventually(func(g Gomega) {
				for i := range 3 {
					fqdn := fmt.Sprintf("%s-%d.%s-headless.%s.svc.cluster.local",
						clusterName, i, clusterName, testNamespace)
					n, err := routezNumRoutes(pod, fqdn)
					g.Expect(err).NotTo(HaveOccurred(), "pod %d /routez", i)
					g.Expect(n).To(BeNumerically(">=", 2),
						"pod %d has num_routes=%d, want full mesh (>=2)", i, n)
				}
			}, 2*time.Minute, 5*time.Second).Should(Succeed())
		})

		It("pub/sub propagates across the cluster", func() {
			pod := waitForNatsBoxPod(boxName, 30*time.Second)

			// Force subscribe and publish to pin to different cluster
			// pods by hitting their per-pod FQDNs directly. If the
			// route mesh is broken, this times out even though both
			// pods accept the TCP connection.
			sub := fmt.Sprintf("%s-0.%s-headless.%s.svc.cluster.local:4222",
				clusterName, clusterName, testNamespace)
			pub := fmt.Sprintf("%s-2.%s-headless.%s.svc.cluster.local:4222",
				clusterName, clusterName, testNamespace)

			shell := fmt.Sprintf(
				"nats --server nats://%s sub --count=1 --timeout=20s e2e.trio & "+
					"sleep 2 && nats --server nats://%s pub e2e.trio hello-trio && wait",
				sub, pub,
			)
			out, err := execNatsBox(pod, "sh", "-c", shell)
			Expect(err).NotTo(HaveOccurred(), "cross-pod pub/sub failed:\n%s", out)
			Expect(out).To(ContainSubstring("hello-trio"))
		})
	})

	// ---------------------------------------------------------------
	// Scenario 3: JetStream with file storage.
	// Verifies stream create + publish + consume end to end, and that
	// messages actually land in the file-backed store. Single replica
	// to keep meta-leader election out of the way — clustered
	// JetStream gets its own scenario once the basic path is green.
	// ---------------------------------------------------------------
	Context("JetStream file store", Ordered, func() {
		const (
			clusterName = "e2e-js"
			boxName     = "e2e-js-box"
			streamName  = "E2E"
			subject     = "e2e.js.>"
		)
		manifest := fmt.Sprintf(`
apiVersion: nats.crewlet.cloud/v1alpha1
kind: NatsCluster
metadata:
  name: %s
spec:
  replicas: 1
  config:
    jetstream:
      enabled: true
      fileStore:
        enabled: true
        pvc:
          resources:
            requests:
              storage: 1Gi
---
apiVersion: nats.crewlet.cloud/v1alpha1
kind: NatsBox
metadata:
  name: %s
spec:
  clusterRef:
    name: %s
`, clusterName, boxName, clusterName)

		BeforeAll(func() {
			By("applying JetStream NatsCluster + NatsBox")
			kubectlApplyStdin(manifest)
		})
		AfterAll(func() {
			By("deleting JetStream fixtures")
			kubectlDeleteStdin(manifest)
		})

		It("reports Available=True", func() {
			waitForNatsClusterAvailable(clusterName, 5*time.Minute)
		})

		It("stream create + publish + info round-trips", func() {
			pod := waitForNatsBoxPod(boxName, 2*time.Minute)

			By("creating a file-backed stream")
			// Wait for JetStream to be enabled — the server reports
			// "JetStream not currently available" for a moment after
			// startup while it initializes the file store.
			Eventually(func(g Gomega) {
				out, err := execNatsBox(pod,
					"nats", "stream", "add", streamName,
					"--subjects", subject,
					"--storage", "file",
					"--replicas", "1",
					"--defaults",
				)
				g.Expect(err).NotTo(HaveOccurred(), "stream add failed:\n%s", out)
			}, 90*time.Second, 3*time.Second).Should(Succeed())

			By("publishing 5 messages")
			for i := range 5 {
				out, err := execNatsBox(pod, "nats", "pub",
					fmt.Sprintf("e2e.js.msg.%d", i),
					fmt.Sprintf("payload-%d", i),
				)
				Expect(err).NotTo(HaveOccurred(), "publish %d:\n%s", i, out)
			}

			By("verifying the stream reports 5 messages")
			// `nats stream info --json` emits a JSON blob whose
			// .state.messages field is read straight from the
			// JetStream file store, so this is the authoritative
			// end-to-end proof that the 5 publishes were captured
			// and persisted. We deliberately don't also try to
			// consume them back with `nats sub`: that subscribes via
			// core NATS, which only sees live traffic — the messages
			// are at rest in the stream, so a core sub would hang
			// until timeout and fail the suite. The pull-consumer
			// delivery path deserves its own scenario if we want to
			// cover it explicitly.
			Eventually(func(g Gomega) {
				out, err := execNatsBox(pod, "nats", "stream", "info", streamName, "--json")
				g.Expect(err).NotTo(HaveOccurred())
				// Avoid pulling in a full unmarshal — a substring
				// match on the state line is specific enough here.
				g.Expect(out).To(ContainSubstring(`"messages": 5`))
			}, 30*time.Second, 2*time.Second).Should(Succeed())
		})
	})
})

// serviceAccountToken returns a token for the specified service account in the given namespace.
// It uses the Kubernetes TokenRequest API to generate a token by directly sending a request
// and parsing the resulting token from the API response.
func serviceAccountToken() (string, error) {
	const tokenRequestRawString = `{
		"apiVersion": "authentication.k8s.io/v1",
		"kind": "TokenRequest"
	}`

	By("creating temporary file to store the token request")
	secretName := fmt.Sprintf("%s-token-request", serviceAccountName)
	tokenRequestFile := filepath.Join("/tmp", secretName)
	err := os.WriteFile(tokenRequestFile, []byte(tokenRequestRawString), os.FileMode(0o644))
	if err != nil {
		return "", err
	}

	var out string
	verifyTokenCreation := func(g Gomega) {
		By("executing kubectl command to create the token")
		cmd := exec.Command("kubectl", "create", "--raw", fmt.Sprintf(
			"/api/v1/namespaces/%s/serviceaccounts/%s/token",
			namespace,
			serviceAccountName,
		), "-f", tokenRequestFile)

		output, err := cmd.CombinedOutput()
		g.Expect(err).NotTo(HaveOccurred())

		By("parsing the JSON output to extract the token")
		var token tokenRequest
		err = json.Unmarshal(output, &token)
		g.Expect(err).NotTo(HaveOccurred())

		out = token.Status.Token
	}
	Eventually(verifyTokenCreation).Should(Succeed())

	return out, err
}

// getMetricsOutput retrieves and returns the logs from the curl pod used to access the metrics endpoint.
func getMetricsOutput() (string, error) {
	By("getting the curl-metrics logs")
	cmd := exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
	return utils.Run(cmd)
}

// tokenRequest is a simplified representation of the Kubernetes TokenRequest API response,
// containing only the token field that we need to extract.
type tokenRequest struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}
