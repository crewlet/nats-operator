//go:build e2e

/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package e2e

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2" // nolint:revive,staticcheck
	. "github.com/onsi/gomega"    // nolint:revive,staticcheck

	"github.com/crewlet/nats-operator/test/utils"
)

// testNamespace is the namespace all NATS e2e specs deploy their
// fixtures into. Separate from nats-operator-system so spec-level
// cleanup doesn't affect the operator deployment.
const testNamespace = "nats-e2e"

// -- manifest / kubectl plumbing --------------------------------------------

// kubectlApplyStdin pipes the given YAML into `kubectl apply -f -`.
// Tests build their fixtures as Go string literals (fmt.Sprintf-ready)
// and hand them to this helper, so a failing spec's manifest is
// visible right next to the assertion.
func kubectlApplyStdin(manifest string) {
	GinkgoHelper()
	cmd := exec.Command("kubectl", "apply", "-n", testNamespace, "-f", "-")
	cmd.Stdin = strings.NewReader(manifest)
	out, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "kubectl apply failed:\n%s", out)
}

// kubectlDeleteStdin removes a manifest applied with kubectlApplyStdin.
// Errors are swallowed — the AfterAll should tear down even on failure.
func kubectlDeleteStdin(manifest string) {
	GinkgoHelper()
	cmd := exec.Command("kubectl", "delete", "-n", testNamespace, "--ignore-not-found", "-f", "-")
	cmd.Stdin = strings.NewReader(manifest)
	_, _ = utils.Run(cmd)
}

// kubectlJSONPath runs `kubectl get <kind>/<name> -o jsonpath=<jp>` and
// returns stdout. Useful for reading a single scalar out of a resource
// without unmarshalling the whole object.
func kubectlJSONPath(kind, name, jp string) (string, error) {
	cmd := exec.Command("kubectl", "get", "-n", testNamespace, kind, name,
		"-o", fmt.Sprintf("jsonpath=%s", jp))
	return utils.Run(cmd)
}

// -- NatsCluster readiness --------------------------------------------------

// waitForNatsClusterAvailable blocks until the NatsCluster's
// Available=True condition is reported by the operator. The operator
// only sets Available=True once every replica is reported ready by the
// StatefulSet, so this is the right "NATS is actually running" gate.
func waitForNatsClusterAvailable(name string, timeout time.Duration) {
	GinkgoHelper()
	jp := `{.status.conditions[?(@.type=="Available")].status}`
	Eventually(func(g Gomega) {
		out, err := kubectlJSONPath("natscluster", name, jp)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(strings.TrimSpace(out)).To(Equal("True"),
			"NatsCluster %q not yet Available=True", name)
	}, timeout, 3*time.Second).Should(Succeed())
}

// waitForReadyReplicas blocks until NatsCluster.status.readyReplicas
// matches the expected count. Used as a second gate for clustering
// tests — we need to know every pod is serving, not just that one is.
func waitForReadyReplicas(name string, want int, timeout time.Duration) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		out, err := kubectlJSONPath("natscluster", name, `{.status.readyReplicas}`)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(strings.TrimSpace(out)).To(Equal(fmt.Sprintf("%d", want)),
			"NatsCluster %q readyReplicas != %d yet", name, want)
	}, timeout, 3*time.Second).Should(Succeed())
}

// -- NatsBox -----------------------------------------------------------------

// waitForNatsBoxPod blocks until the nats-box Deployment has a Ready
// pod we can exec into, and returns that pod's name. The operator's
// Deployment name follows <natsbox-name>.
func waitForNatsBoxPod(boxName string, timeout time.Duration) string {
	GinkgoHelper()
	var podName string
	Eventually(func(g Gomega) {
		// The operator labels nats-box pods with app.kubernetes.io/name=nats-box
		// and app.kubernetes.io/instance=<box-name>.
		sel := fmt.Sprintf("app.kubernetes.io/name=nats-box,app.kubernetes.io/instance=%s", boxName)
		cmd := exec.Command("kubectl", "get", "pods",
			"-n", testNamespace, "-l", sel,
			"-o", "jsonpath={.items[?(@.status.phase==\"Running\")].metadata.name}")
		out, err := utils.Run(cmd)
		g.Expect(err).NotTo(HaveOccurred())
		podName = strings.TrimSpace(out)
		g.Expect(podName).NotTo(BeEmpty(), "no Running nats-box pod for %q yet", boxName)
	}, timeout, 3*time.Second).Should(Succeed())
	return podName
}

// execNatsBox runs a command inside the nats-box pod. The caller passes
// only the command arguments (e.g. "nats", "server", "check", "connection")
// — this helper wires up the kubectl exec envelope.
func execNatsBox(podName string, args ...string) (string, error) {
	full := append([]string{"exec", "-n", testNamespace, podName, "--"}, args...)
	cmd := exec.Command("kubectl", full...)
	return utils.Run(cmd)
}

// -- monitor endpoint --------------------------------------------------------

// routezNumRoutes fetches /routez for a specific cluster pod and
// returns the `num_routes` field. Used to assert that a clustered
// NatsCluster actually forms its mesh — regression for the HOCON
// `routes [...]` vs `routes = [...]` bug, which made routes silently
// empty even though TCP and INFO handshakes succeeded.
func routezNumRoutes(natsBoxPod, clusterPodFQDN string) (int, error) {
	out, err := execNatsBox(natsBoxPod, "wget", "-qO-",
		fmt.Sprintf("http://%s:8222/routez", clusterPodFQDN))
	if err != nil {
		return 0, fmt.Errorf("fetch /routez: %w: %s", err, out)
	}
	var r struct {
		NumRoutes int `json:"num_routes"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		return 0, fmt.Errorf("parse /routez: %w:\n%s", err, out)
	}
	return r.NumRoutes, nil
}
