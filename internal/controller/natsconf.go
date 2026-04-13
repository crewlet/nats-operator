/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	natsv1alpha1 "github.com/crewlet/nats-operator/api/v1alpha1"
)

// confRaw is a config value emitted unquoted by the renderer. Use it for
// numeric-with-unit literals (e.g. "1GB"), env-var substitutions ("$POD_NAME"),
// and other tokens that must not be wrapped in quotes.
type confRaw string

// confBlock is the renderer's intermediate representation. The serializer
// walks it deterministically (sorted keys) so the same spec always produces
// byte-identical output — critical because the rendered file lands in a
// ConfigMap and unstable rendering would force needless rolling restarts.
type confBlock map[string]any

// includeKeyPrefix tags map keys whose value is an include directive instead
// of a regular key/value pair. The serializer recognizes the prefix and emits
// `include "<value>";` rather than `key: "value"`. The trailing index keeps
// keys sortable in declaration order.
const includeKeyPrefix = "$include"

// envVarServerName is the environment variable nats-server reads to derive
// each pod's unique server_name. Set via downward API in the StatefulSet
// builder; referenced from the rendered nats.conf.
const envVarServerName = "$SERVER_NAME"

// envVarRoutesUser / envVarRoutesPassword are the env vars the operator
// projects from spec.config.cluster.routeURLs.authSecretRef into the nats
// container, and that the rendered nats.conf references in the cluster
// authorization block. Keeping the credentials out of the ConfigMap is the
// reason for the indirection.
const (
	envVarRoutesUser     = "$NATS_ROUTES_USER"
	envVarRoutesPassword = "$NATS_ROUTES_PASSWORD"
)

// renderNatsConf walks the defaulted spec and returns the rendered nats.conf
// bytes the operator writes into the ConfigMap. Callers must pass a spec
// already processed by defaulted() — the renderer does not re-apply defaults.
func renderNatsConf(cr *natsv1alpha1.NatsCluster, spec *natsv1alpha1.NatsClusterSpec) []byte {
	root := confBlock{}

	// Per-pod server name (downward API substitution).
	if spec.Config.ServerNamePrefix != "" {
		root["server_name"] = confRaw(spec.Config.ServerNamePrefix + envVarServerName)
	} else {
		root["server_name"] = confRaw(envVarServerName)
	}

	// Client listener.
	root["port"] = spec.Config.Nats.Port
	if spec.Config.Nats.TLS.Enabled {
		root["tls"] = renderTLS(spec.Config.Nats.TLS, "nats", spec.TLSCA)
	}

	// Monitor.
	if isTrue(spec.Config.Monitor.Enabled) {
		if spec.Config.Monitor.TLSEnabled {
			root["https_port"] = spec.Config.Monitor.Port
		} else {
			root["http_port"] = spec.Config.Monitor.Port
		}
	}

	// Profiling.
	if spec.Config.Profiling.Enabled {
		root["prof_port"] = spec.Config.Profiling.Port
	}

	// Cluster routing — only emitted when there's actually a peer set.
	if spec.Replicas != nil && *spec.Replicas > 1 {
		root["cluster"] = renderCluster(cr, spec)
	}

	if spec.Config.JetStream.Enabled {
		root["jetstream"] = renderJetStream(spec.Config.JetStream)
	}

	if spec.Config.LeafNodes.Enabled {
		root["leafnodes"] = renderListener(spec.Config.LeafNodes, "leafnodes", spec.TLSCA)
	}

	if spec.Config.WebSocket.Enabled {
		root["websocket"] = renderWebSocket(spec.Config.WebSocket, spec.TLSCA)
	}

	if spec.Config.MQTT.Enabled {
		root["mqtt"] = renderListener(spec.Config.MQTT, "mqtt", spec.TLSCA)
	}

	if spec.Config.Gateway.Enabled {
		root["gateway"] = renderGateway(cr, spec.Config.Gateway, spec.TLSCA)
	}

	// When the typed JWT auth path is active, emit an include directive
	// pointing at the operator-managed auth Secret (mounted at
	// /etc/nats-auth). The Secret contains the rendered auth.conf
	// fragment which sets `operator:`, `system_account:`, `resolver:`
	// and pulls in resolver_preload.conf. Keeping the JWT material out
	// of the ConfigMap — only an include reference lands in nats.conf.
	if spec.Auth.JWT != nil {
		root[includeKeyPrefix+"000auth"] = mountPathAuth + "/" + authFileName
	}

	// Config.Includes are rendered last so user-supplied snippets can
	// override values produced from the typed spec. The key prefix
	// sorting ensures they come after the auth include above.
	for i, inc := range spec.Config.Includes {
		key := fmt.Sprintf("%s%03d-%s", includeKeyPrefix, i+1, inc.Name)
		root[key] = includeMountPath(inc.Name)
	}

	var buf bytes.Buffer
	renderBlockBody(&buf, root, 0)
	return buf.Bytes()
}

// ----- per-block renderers -----

func renderTLS(tls natsv1alpha1.TLSBlock, listener string, ca natsv1alpha1.TLSCASpec) confBlock {
	dir := tlsMountPath(listener)
	out := confBlock{
		"cert_file": dir + "/" + tls.Cert,
		"key_file":  dir + "/" + tls.Key,
	}
	if tls.Verify != nil && *tls.Verify {
		out["verify"] = true
	}
	if tls.Timeout != nil {
		out["timeout"] = *tls.Timeout
	}
	if caKey := tlsCAKey(ca); caKey != "" {
		out["ca_file"] = mountPathCA + "/" + caKey
	}
	return out
}

// tlsCAKey returns the key name within the mounted CA bundle, or "" when
// no global CA is configured.
func tlsCAKey(ca natsv1alpha1.TLSCASpec) string {
	switch {
	case ca.Secret != nil && ca.Secret.Key != "":
		return ca.Secret.Key
	case ca.ConfigMap != nil && ca.ConfigMap.Key != "":
		return ca.ConfigMap.Key
	case ca.Secret != nil || ca.ConfigMap != nil:
		return defaultCAKey
	}
	return ""
}

func renderCluster(cr *natsv1alpha1.NatsCluster, spec *natsv1alpha1.NatsClusterSpec) confBlock {
	c := spec.Config.Cluster
	out := confBlock{
		"name": cr.Name,
		"port": c.Port,
	}
	if c.NoAdvertise != nil {
		out["no_advertise"] = *c.NoAdvertise
	}
	if c.TLS.Enabled {
		out["tls"] = renderTLS(c.TLS, "cluster", spec.TLSCA)
	}

	out["routes"] = clusterRoutes(cr, spec)

	if c.RouteURLs.AuthSecretRef != nil {
		out["authorization"] = confBlock{
			"user":     confRaw(envVarRoutesUser),
			"password": confRaw(envVarRoutesPassword),
		}
	}

	return out
}

// clusterRoutes builds the typed list of route URLs the operator stamps into
// the cluster block. Routes use the headless Service DNS so each pod can
// resolve its peers without leaking through the client Service.
//
// The host form is always the fully-qualified
// `<pod>.<headless>.<ns>.svc.<cluster-domain>` — shorter forms work
// inconsistently across resolvers (musl libc and Go's net resolver
// disagree on search-path expansion for names with 2+ dots), which
// previously caused transient "no such host" errors on cold cluster
// boot. Emitting FQDN is safe on every resolver and sidesteps a whole
// class of DNS startup races.
func clusterRoutes(cr *natsv1alpha1.NatsCluster, spec *natsv1alpha1.NatsClusterSpec) []any {
	if spec.Replicas == nil {
		return nil
	}
	port := spec.Config.Cluster.Port
	domain := spec.Config.Cluster.RouteURLs.K8sClusterDomain
	withAuth := spec.Config.Cluster.RouteURLs.AuthSecretRef != nil

	scheme := "nats"
	if spec.Config.Cluster.TLS.Enabled {
		scheme = "tls"
	}

	routes := make([]any, 0, *spec.Replicas)
	for i := int32(0); i < *spec.Replicas; i++ {
		host := podFQDN(cr, i, domain)
		var url string
		if withAuth {
			url = fmt.Sprintf("%s://%s:%s@%s:%d",
				scheme, envVarRoutesUser, envVarRoutesPassword, host, port)
		} else {
			url = fmt.Sprintf("%s://%s:%d", scheme, host, port)
		}
		routes = append(routes, url)
	}
	return routes
}

func renderJetStream(js natsv1alpha1.JetStreamConfig) confBlock {
	out := confBlock{}
	if isTrue(js.FileStore.Enabled) || (js.FileStore.Enabled == nil && !js.MemoryStore.Enabled) {
		out["store_dir"] = mountPathData
	}
	if js.MemoryStore.Enabled && js.MemoryStore.MaxSize != nil {
		out["max_memory_store"] = confRaw(js.MemoryStore.MaxSize.String())
	}
	if js.FileStore.MaxSize != nil {
		out["max_file_store"] = confRaw(js.FileStore.MaxSize.String())
	}
	return out
}

func renderListener(l natsv1alpha1.ListenerConfig, listener string, ca natsv1alpha1.TLSCASpec) confBlock {
	out := confBlock{
		"port": l.Port,
	}
	if l.TLS.Enabled {
		out["tls"] = renderTLS(l.TLS, listener, ca)
	}
	return out
}

func renderWebSocket(w natsv1alpha1.WebSocketConfig, ca natsv1alpha1.TLSCASpec) confBlock {
	out := confBlock{
		"port": w.Port,
	}
	if w.TLS.Enabled {
		out["tls"] = renderTLS(w.TLS, "websocket", ca)
	} else {
		// websocket requires explicit no_tls when running on plain HTTP.
		out["no_tls"] = true
	}
	return out
}

func renderGateway(cr *natsv1alpha1.NatsCluster, g natsv1alpha1.ListenerConfig, ca natsv1alpha1.TLSCASpec) confBlock {
	out := confBlock{
		"name": cr.Name,
		"port": g.Port,
	}
	if g.TLS.Enabled {
		out["tls"] = renderTLS(g.TLS, "gateway", ca)
	}
	return out
}

// ----- serializer -----

// renderBlockBody emits the contents of a confBlock at the given indent
// without surrounding braces. Used for the top-level block (the file body)
// and reused by renderBlock for nested blocks. Include directives are
// always emitted after the regular keys so user-supplied snippets can
// override values produced from the typed spec.
func renderBlockBody(buf *bytes.Buffer, b confBlock, indent int) {
	regular, includes := partitionKeys(b)
	for _, k := range regular {
		v := b[k]

		switch x := v.(type) {
		case confBlock:
			indentWrite(buf, indent)
			fmt.Fprintf(buf, "%s {\n", k)
			renderBlockBody(buf, x, indent+1)
			indentWrite(buf, indent)
			buf.WriteString("}\n")
		case []any:
			// `key = [ ... ]` — the separator matters. nats-server's
			// HOCON parser accepts `key {` for nested objects (standard
			// HOCON shorthand) but is NOT permissive about `key [ ... ]`
			// without an assignment operator for arrays — the bare form
			// parses silently as a path-concatenation expression and
			// leaves the list empty at runtime, which is exactly how we
			// lost our cluster routes and spent an afternoon chasing
			// ghosts. We use `=` rather than `:` to match the examples
			// shipped in the nats-server source tree (e.g.
			// server/configs/srv_a.conf), which is the most reliable
			// signal of what the parser actually accepts cleanly.
			indentWrite(buf, indent)
			fmt.Fprintf(buf, "%s = [\n", k)
			for _, item := range x {
				indentWrite(buf, indent+1)
				renderScalar(buf, item)
				buf.WriteString("\n")
			}
			indentWrite(buf, indent)
			buf.WriteString("]\n")
		default:
			indentWrite(buf, indent)
			fmt.Fprintf(buf, "%s: ", k)
			renderScalar(buf, v)
			buf.WriteString("\n")
		}
	}
	for _, k := range includes {
		indentWrite(buf, indent)
		fmt.Fprintf(buf, "include %q\n", b[k])
	}
}

// partitionKeys returns the regular keys of a confBlock sorted alphabetically
// followed by include-directive keys sorted by their declaration order
// (encoded into the key suffix). Splitting them lets the renderer place
// `include "..."` lines after the structured config so includes can override.
func partitionKeys(b confBlock) (regular, includes []string) {
	for k := range b {
		if strings.HasPrefix(k, includeKeyPrefix) {
			includes = append(includes, k)
		} else {
			regular = append(regular, k)
		}
	}
	sort.Strings(regular)
	sort.Strings(includes)
	return regular, includes
}

// renderScalar emits a scalar value with the appropriate quoting.
func renderScalar(buf *bytes.Buffer, v any) {
	switch x := v.(type) {
	case string:
		fmt.Fprintf(buf, "%q", x)
	case confRaw:
		buf.WriteString(string(x))
	case bool:
		if x {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case int:
		fmt.Fprintf(buf, "%d", x)
	case int32:
		fmt.Fprintf(buf, "%d", x)
	case int64:
		fmt.Fprintf(buf, "%d", x)
	default:
		// Fall back to %v for anything we forgot to special-case.
		fmt.Fprintf(buf, "%v", x)
	}
}

func indentWrite(buf *bytes.Buffer, indent int) {
	for range indent {
		buf.WriteString("  ")
	}
}

func isTrue(p *bool) bool {
	return p != nil && *p
}
