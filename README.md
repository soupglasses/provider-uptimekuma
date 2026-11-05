<!--
SPDX-FileCopyrightText: 2026 Sofie Finnes <sofie+git@finnes.dev>
SPDX-License-Identifier: MPL-2.0
-->

# Uptime Kuma Crossplane provider

Namespaced Crossplane resources for an existing Uptime Kuma instance, including one
running outside Kubernetes. The Kuma server keeps checking and serving its public
page when Kubernetes or this controller is unavailable.

The provider embeds `breml/terraform-provider-uptimekuma` through Upjet's Terraform
Plugin Framework connector. No Terraform executable, Workspace or separate state
backend is required. Configure and update resource manifests through Git and Flux.

## APIs and ownership

`uptimekuma.ops.finnes.dev/v1alpha1` provides `HTTPMonitor`, `GameMonitor` and
`StatusPage`. `GameMonitor` uses Kuma's native GameDig implementation, including
Minecraft Server List Ping. It does not reduce game health to TCP connectivity.

Configure the runtime's `WATCH_NAMESPACE`. All resources and credential references
must stay in that namespace. The namespaced `ProviderConfig` at `v1beta1` must be
named `default`. One controller runtime manages one Kuma instance because the
embedded provider uses a single global connection pool. An intentional endpoint or
credential change retires that pool; overlapping operations can retry normally.

The ProviderConfig contains `endpoint`, `username` (or `usernameSecretRef`) and
`passwordSecretRef`, whose selector contains `name` and `key`. These are normal Kuma
login credentials. Prometheus API keys cannot manage monitors or status pages.
The runtime reads Secrets directly rather than caching the namespace's Secrets.
Crossplane grants package-owned runtime Deployments broad Secret permissions even
with a custom service account. The deployment example sets that generated runtime
to zero replicas and runs the packaged image in an independently managed Deployment
with a namespaced Role. This keeps Secret listing and cross-namespace access denied
without changing RBAC for existing providers. See `tests/provider.yaml`.

Unbound monitors are adopted by unique name and expected monitor type; pages use
their unique slug. Ambiguous names fail closed. This recovers ownership even after
Kubernetes annotations and status are lost, retaining the same remote IDs and history.
An explicit numeric `crossplane.io/external-name` can disambiguate existing monitors.
Start adoption with `managementPolicies: [Observe]`. Check observed data, then add
`Update` after the desired configuration matches. Omit `Delete` to preserve the
remote resource when a Kubernetes declaration is removed. Resource replacement
requires a deliberate ownership decision; do not discard external identifiers when
rebuilding the cluster.

The bridge converts numeric root IDs to Crossplane's string identifiers and reads
the authoritative public page for monitor groups and visibility flags. The upstream
Socket.IO read alone omits those fields and can otherwise report false convergence.
A failed public read is an error, rather than an excuse to assume desired state is
already applied. Kuma's server currently does not implement changing `published`
through its save-status-page handler; use this provider for published pages.

## Build and verification

Use the package's Mise tools. `./generate.sh` regenerates schema, APIs, controllers
and CRDs from the embedded provider. The generator tools are pinned in `Dockerfile`.
`./build.sh /tmp/provider-uptimekuma.xpkg` builds a native Crossplane package.

`tests/run-native.sh` starts a disposable pinned Kuma container with a generated
credential. It exercises native resource creation, adoption after discarding
Kubernetes status, public-page drift detection and repair, convergence and deletion.
Set `CONTAINER_ENGINE='sudo -n podman'` to run the fixture using rootful Podman.
`kind-test.sh` verifies installation and reconciliation of the packaged controller
under restricted Pod Security Admission in a disposable cluster.

CI verifies generated output, runs both integration layers and publishes the native
package only from main. Existing production state is not a test fixture.
