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

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	natsv1alpha1 "github.com/crewlet/nats-operator/api/v1alpha1"
)

const testNatsTLSSecretName = "nats-tls"

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
				spec.Config.Nats.TLS.SecretName = testNatsTLSSecretName
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
				spec.Config.Nats.TLS.SecretName = testNatsTLSSecretName
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
		{
			name: "mqtt and gateway listeners render with correct ports and TLS",
			mut: func(spec *natsv1alpha1.NatsClusterSpec) {
				spec.Config.MQTT.Enabled = true
				spec.Config.Gateway.Enabled = true
				spec.Config.Gateway.TLS.Enabled = true
				spec.Config.Gateway.TLS.SecretName = "gateway-tls"
			},
			want: `gateway {
  name: "test"
  port: 7222
  tls {
    cert_file: "/etc/nats-certs/gateway/tls.crt"
    key_file: "/etc/nats-certs/gateway/tls.key"
  }
}
http_port: 8222
mqtt {
  port: 1883
}
port: 4222
server_name: $SERVER_NAME
`,
		},
		{
			name: "leafnodes with TLS emits per-listener tls block",
			mut: func(spec *natsv1alpha1.NatsClusterSpec) {
				spec.Config.LeafNodes.Enabled = true
				spec.Config.LeafNodes.TLS.Enabled = true
				spec.Config.LeafNodes.TLS.SecretName = "leafnode-tls"
			},
			want: `http_port: 8222
leafnodes {
  port: 7422
  tls {
    cert_file: "/etc/nats-certs/leafnodes/tls.crt"
    key_file: "/etc/nats-certs/leafnodes/tls.key"
  }
}
port: 4222
server_name: $SERVER_NAME
`,
		},
		{
			name: "profiling listener emits prof_port",
			mut: func(spec *natsv1alpha1.NatsClusterSpec) {
				spec.Config.Profiling.Enabled = true
			},
			want: `http_port: 8222
port: 4222
prof_port: 65432
server_name: $SERVER_NAME
`,
		},
		{
			name: "auth.jwt emits the /etc/nats-auth include",
			mut: func(spec *natsv1alpha1.NatsClusterSpec) {
				spec.Auth.JWT = &natsv1alpha1.JWTAuthSpec{
					Operator:      corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "op"}, Key: "operator.jwt"},
					SystemAccount: "AASYS",
					Accounts: []natsv1alpha1.JWTAccount{
						{Name: "sys", PublicKey: "AASYS", JWT: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "sys-jwt"}, Key: "account.jwt"}},
					},
				}
			},
			want: `http_port: 8222
port: 4222
server_name: $SERVER_NAME
include "/etc/nats-auth/auth.conf"
`,
		},
		{
			name: "auth include comes before config.includes in the output",
			mut: func(spec *natsv1alpha1.NatsClusterSpec) {
				spec.Auth.JWT = &natsv1alpha1.JWTAuthSpec{
					Operator:      corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "op"}, Key: "operator.jwt"},
					SystemAccount: "AASYS",
					Accounts: []natsv1alpha1.JWTAccount{
						{Name: "sys", PublicKey: "AASYS"},
					},
				}
				spec.Config.Includes = []natsv1alpha1.ConfigInclude{
					{Name: "limits.conf", Secret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "l"}}},
				}
			},
			want: `http_port: 8222
port: 4222
server_name: $SERVER_NAME
include "/etc/nats-auth/auth.conf"
include "/etc/nats-extra/limits.conf"
`,
		},
		{
			name: "tlsCA adds ca_file to each listener TLS block",
			mut: func(spec *natsv1alpha1.NatsClusterSpec) {
				spec.Config.Nats.TLS.Enabled = true
				spec.Config.Nats.TLS.SecretName = testNatsTLSSecretName
				spec.Config.LeafNodes.Enabled = true
				spec.Config.LeafNodes.TLS.Enabled = true
				spec.Config.LeafNodes.TLS.SecretName = "leaf-tls"
				spec.TLSCA.Secret = &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "nats-ca"},
					Key:                  "ca.crt",
				}
			},
			want: `http_port: 8222
leafnodes {
  port: 7422
  tls {
    ca_file: "/etc/nats-ca-cert/ca.crt"
    cert_file: "/etc/nats-certs/leafnodes/tls.crt"
    key_file: "/etc/nats-certs/leafnodes/tls.key"
  }
}
port: 4222
server_name: $SERVER_NAME
tls {
  ca_file: "/etc/nats-ca-cert/ca.crt"
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
			assert.Equal(t, tt.want, string(got), "rendered config mismatch")
		})
	}
}
