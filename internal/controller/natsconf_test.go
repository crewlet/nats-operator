/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	natsv1alpha1 "github.com/crewlet/nats-operator/api/v1alpha1"
)

// TestRenderNatsConf is a set of golden tests pinning the renderer's output
// for representative spec shapes. The expected strings are inlined so a
// reader of the test file sees the exact bytes the operator writes into the
// ConfigMap. When the renderer changes intentionally, update the golden
// strings — when it changes unintentionally, the test fails.
func TestRenderNatsConf(t *testing.T) {
	tests := []struct {
		name string
		mut  func(spec *natsv1alpha1.NatsClusterSpec)
		want string
	}{
		{
			name: "single-replica minimal",
			mut:  func(spec *natsv1alpha1.NatsClusterSpec) {},
			want: `http_port: 8222
port: 4222
server_name: $SERVER_NAME
`,
		},
		{
			name: "three-replica clustered",
			mut: func(spec *natsv1alpha1.NatsClusterSpec) {
				spec.Replicas = ptr(int32(3))
			},
			want: `cluster {
  name: "test"
  no_advertise: true
  port: 6222
  routes = [
    "nats://test-0.test-headless.default.svc.cluster.local:6222"
    "nats://test-1.test-headless.default.svc.cluster.local:6222"
    "nats://test-2.test-headless.default.svc.cluster.local:6222"
  ]
}
http_port: 8222
port: 4222
server_name: $SERVER_NAME
`,
		},
		{
			name: "jetstream file store",
			mut: func(spec *natsv1alpha1.NatsClusterSpec) {
				spec.Config.JetStream.Enabled = true
			},
			want: `http_port: 8222
jetstream {
  store_dir: "/data"
}
port: 4222
server_name: $SERVER_NAME
`,
		},
		{
			name: "client TLS with CA",
			mut: func(spec *natsv1alpha1.NatsClusterSpec) {
				spec.Config.Nats.TLS.Enabled = true
				spec.Config.Nats.TLS.SecretName = "nats-tls"
				spec.Config.Nats.TLS.Cert = defaultTLSCertFile
				spec.Config.Nats.TLS.Key = defaultTLSKeyFile
				spec.TLSCA.Secret = &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "ca-bundle"},
					Key:                  "ca.crt",
				}
			},
			want: `http_port: 8222
port: 4222
server_name: $SERVER_NAME
tls {
  ca_file: "/etc/nats-ca-cert/ca.crt"
  cert_file: "/etc/nats-certs/nats/tls.crt"
  key_file: "/etc/nats-certs/nats/tls.key"
}
`,
		},
		{
			name: "websocket and leafnodes",
			mut: func(spec *natsv1alpha1.NatsClusterSpec) {
				spec.Config.WebSocket.Enabled = true
				spec.Config.LeafNodes.Enabled = true
			},
			want: `http_port: 8222
leafnodes {
  port: 7422
}
port: 4222
server_name: $SERVER_NAME
websocket {
  no_tls: true
  port: 8080
}
`,
		},
		{
			name: "includes are emitted last in declaration order",
			mut: func(spec *natsv1alpha1.NatsClusterSpec) {
				spec.Config.Includes = []natsv1alpha1.ConfigInclude{
					{
						Name:   "auth.conf",
						Secret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "auth"}},
					},
					{
						Name:      "limits.conf",
						ConfigMap: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "limits"}},
					},
				}
			},
			want: `http_port: 8222
port: 4222
server_name: $SERVER_NAME
include "/etc/nats-extra/auth.conf"
include "/etc/nats-extra/limits.conf"
`,
		},
		{
			name: "cluster routes use auth env vars when authSecretRef set",
			mut: func(spec *natsv1alpha1.NatsClusterSpec) {
				spec.Replicas = ptr(int32(2))
				spec.Config.Cluster.RouteURLs.AuthSecretRef = &corev1.LocalObjectReference{Name: "routes-auth"}
			},
			want: `cluster {
  authorization {
    password: $NATS_ROUTES_PASSWORD
    user: $NATS_ROUTES_USER
  }
  name: "test"
  no_advertise: true
  port: 6222
  routes = [
    "nats://$NATS_ROUTES_USER:$NATS_ROUTES_PASSWORD@test-0.test-headless.default.svc.cluster.local:6222"
    "nats://$NATS_ROUTES_USER:$NATS_ROUTES_PASSWORD@test-1.test-headless.default.svc.cluster.local:6222"
  ]
}
http_port: 8222
port: 4222
server_name: $SERVER_NAME
`,
		},
		{
			name: "monitor TLS uses https_port",
			mut: func(spec *natsv1alpha1.NatsClusterSpec) {
				spec.Config.Nats.TLS.Enabled = true
				spec.Config.Nats.TLS.SecretName = "nats-tls"
				spec.Config.Monitor.TLSEnabled = true
			},
			want: `https_port: 8222
port: 4222
server_name: $SERVER_NAME
tls {
  cert_file: "/etc/nats-certs/nats/tls.crt"
  key_file: "/etc/nats-certs/nats/tls.key"
}
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cr := &natsv1alpha1.NatsCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			}
			// Mutate the raw spec first, then default — that mirrors what the
			// reconciler does (fetch CR → default → render). Mutating the
			// already-defaulted spec would skip defaulting for fields the
			// mutation just enabled.
			tt.mut(&cr.Spec)
			spec := defaulted(&cr.Spec)

			got := renderNatsConf(cr, &spec)
			if string(got) != tt.want {
				t.Errorf("rendered config mismatch.\n--- got ---\n%s\n--- want ---\n%s", string(got), tt.want)
			}
		})
	}
}
