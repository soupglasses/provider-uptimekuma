#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Sofie Finnes <sofie+git@finnes.dev>
# SPDX-License-Identifier: MPL-2.0
set -euo pipefail
repo_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
work_dir=$(mktemp -d)
trap 'rm -rf -- "$work_dir"' EXIT
fixture="${KIND_CLUSTER_NAME:?}-kuma"
kubectl create namespace monitoring
kubectl label namespace monitoring pod-security.kubernetes.io/enforce=restricted

# Kuma deliberately runs outside Kubernetes, on the isolated test Docker network.
export KUMA_TEST_PASSWORD_FILE="$work_dir/password"
(umask 077 && openssl rand -hex 24 | tr -d '\n' >"$KUMA_TEST_PASSWORD_FILE")
docker run --detach --name "$fixture" --network kind --publish 127.0.0.1::3001 \
    --env UPTIME_KUMA_DB_TYPE=sqlite "$(cat "$repo_dir/tests/kuma-image.txt")" >/dev/null
port=$(docker port "$fixture" 3001/tcp | head -1 | sed 's/.*://')
fixture_ip=$(docker inspect "$fixture" --format '{{(index .NetworkSettings.Networks "kind").IPAddress}}')
export KUMA_TEST_ENDPOINT="http://127.0.0.1:$port"
for ((attempt = 0; attempt < 90; attempt++)); do
    curl --silent --fail "$KUMA_TEST_ENDPOINT" >/dev/null && break
    sleep 1
done
((attempt < 90)) || { echo 'Kuma fixture did not become ready' >&2; exit 1; }
(cd "$repo_dir" && go run ./tests/bootstrap.go)
kubectl create secret generic kuma-credentials --namespace monitoring --from-file=password="$KUMA_TEST_PASSWORD_FILE"

# All fixtures and credentials are new and disposable; production is never queried.
sed "s|REGISTRY/provider-uptimekuma:test|$PROVIDER_PACKAGE_REF|" "$repo_dir/tests/provider.yaml" |
    kubectl apply --filename -
kubectl wait provider/uptimekuma --for=condition=Healthy --timeout=180s
kubectl rollout status --namespace crossplane-system deployment/uptimekuma-controller --timeout=120s
KUMA_ENDPOINT="http://$fixture_ip:3001" yq '
    select(.kind == "ProviderConfig")
    | .spec.endpoint = strenv(KUMA_ENDPOINT)
    | .spec.username = "crossplane-poc"' "$repo_dir/examples/providerconfig.yaml" |
    kubectl apply --filename -
