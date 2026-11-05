#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Sofie Finnes <sofie+git@finnes.dev>
# SPDX-License-Identifier: MPL-2.0
set -euo pipefail
cd "$(dirname "$0")"
cluster="kuma-provider-$$"
registry="$cluster-registry"
fixture="$cluster-kuma"
work_dir=$(mktemp -d)
export KUBECONFIG="$work_dir/kubeconfig"
export KUMA_TEST_PASSWORD_FILE="$work_dir/password"
cleanup() {
    result=$?
    trap - EXIT
    if ((result != 0)); then
        kubectl get pods --all-namespaces >&2 || true
        kubectl get gamemonitors,httpmonitors,statuspages --namespace monitoring >&2 || true
        kubectl logs --namespace crossplane-system -l app.kubernetes.io/name=uptimekuma-provider --tail=30 >&2 || true
    fi
    kind delete cluster --name "$cluster" || true
    docker rm --force "$registry" "$fixture" >/dev/null 2>&1 || true
    rm -rf -- "$work_dir"
    exit "$result"
}
trap cleanup EXIT
kind create cluster --name "$cluster" --kubeconfig "$KUBECONFIG" \
    --image kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5
registry_image=registry:3.1.2@sha256:d4032d4cc323c1731c707931ecada07a53076eac38e3b61ae57533f170f2c6c3
docker run --detach --name "$registry" --network kind "$registry_image" >/dev/null
registry_ip=$(docker inspect "$registry" --format '{{(index .NetworkSettings.Networks "kind").IPAddress}}')
registry_address="$registry_ip:5000"
docker exec "$cluster-control-plane" mkdir -p "/etc/containerd/certs.d/$registry_address"
cat <<CONFIG | docker exec -i "$cluster-control-plane" tee "/etc/containerd/certs.d/$registry_address/hosts.toml" >/dev/null
server = "http://$registry_address"
[host."http://$registry_address"]
    capabilities = ["pull", "resolve"]
CONFIG
crossplane xpkg push --insecure-skip-tls-verify --package-files "${PROVIDER_PACKAGE:?Pass native package path}" \
    "$registry_address/provider-uptimekuma:test"
for namespace in crossplane-system monitoring; do
    kubectl create namespace "$namespace"
    kubectl label namespace "$namespace" pod-security.kubernetes.io/enforce=restricted
done
helm upgrade --install crossplane crossplane --repo https://charts.crossplane.io/stable \
    --version 2.4.3 --namespace crossplane-system \
    --set-json 'securityContextCrossplane={"runAsNonRoot":true,"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"seccompProfile":{"type":"RuntimeDefault"}}' \
    --set-json 'securityContextRBACManager={"runAsNonRoot":true,"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"seccompProfile":{"type":"RuntimeDefault"}}' \
    --wait --timeout 4m
# Kuma deliberately runs outside Kubernetes, on the isolated test Docker network.
mkdir "$work_dir/data"
python3 - <<'PY'
import os, secrets
with open(os.open(os.environ['KUMA_TEST_PASSWORD_FILE'], os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as f:
    f.write(secrets.token_urlsafe(36))
PY
docker run --detach --name "$fixture" --network kind --publish 127.0.0.1::3001 \
    --env UPTIME_KUMA_DB_TYPE=sqlite --volume "$work_dir/data:/app/data" "$(cat tests/kuma-image.txt)" >/dev/null
port=$(docker port "$fixture" 3001/tcp | head -1 | sed 's/.*://')
fixture_ip=$(docker inspect "$fixture" --format '{{(index .NetworkSettings.Networks "kind").IPAddress}}')
export KUMA_TEST_ENDPOINT="http://127.0.0.1:$port"
for ((i=0; i<90; i++)); do curl --silent --fail "$KUMA_TEST_ENDPOINT" >/dev/null && break; sleep 1; done
go run ./tests/bootstrap.go
kubectl create secret generic kuma-credentials --namespace monitoring --from-file="password=$KUMA_TEST_PASSWORD_FILE"
# All fixtures and credentials are new and disposable; production is never queried.
python3 - "$registry_address" <<'PY' | kubectl apply --filename -
import json,sys
from pathlib import Path
# The YAML is converted by kubectl below; substitute only the trusted test registry.
s=Path('tests/provider.yaml').read_text().replace('REGISTRY/provider-uptimekuma:test',sys.argv[1]+'/provider-uptimekuma:test')
print(s)
PY
kubectl wait provider/uptimekuma --for=condition=Healthy --timeout=180s
revision=$(kubectl get provider uptimekuma --output jsonpath='{.status.currentRevision}')
kubectl rollout status --namespace crossplane-system deployment/uptimekuma-controller --timeout=120s
for resource in providerconfigs gamemonitors httpmonitors statuspages; do
    [[ $(kubectl get crd "$resource.uptimekuma.ops.finnes.dev" --output jsonpath='{.spec.scope}') == Namespaced ]]
done
identity=system:serviceaccount:crossplane-system:uptimekuma-controller
expect_permission() {
    expected=$1; shift
    actual=$(kubectl auth can-i --as="$identity" "$@" 2>/dev/null || true)
    if [[ "$actual" != "$expected" ]]; then
        printf 'Expected %s for %s; got %s\n' "$expected" "$*" "$actual" >&2
        return 1
    fi
}
expect_permission yes get secret/kuma-credentials --namespace monitoring
expect_permission no list secrets --namespace monitoring
expect_permission no get secret/kuma-credentials --namespace default
cat <<CONFIG | kubectl apply --filename -
apiVersion: uptimekuma.ops.finnes.dev/v1beta1
kind: ProviderConfig
metadata:
  name: default
  namespace: monitoring
spec:
  endpoint: http://$fixture_ip:3001
  username: crossplane-poc
  passwordSecretRef:
    name: kuma-credentials
    key: password
---
apiVersion: uptimekuma.ops.finnes.dev/v1alpha1
kind: HTTPMonitor
metadata:
  name: auth
  namespace: monitoring
spec:
  providerConfigRef: {kind: ProviderConfig, name: default}
  forProvider:
    name: Auth fixture
    url: http://127.0.0.1:3001
    interval: 20
    active: true
---
apiVersion: uptimekuma.ops.finnes.dev/v1alpha1
kind: GameMonitor
metadata:
  name: minecraft
  namespace: monitoring
spec:
  providerConfigRef: {kind: ProviderConfig, name: default}
  forProvider:
    name: Minecraft
    hostname: mc.finnes.dev
    port: 25565
    game: minecraft
    gamedigGivenPortOnly: true
    interval: 20
    active: true
CONFIG
kubectl wait --namespace monitoring gamemonitor/minecraft httpmonitor/auth --for=condition=Ready --timeout=120s
game_id=$(kubectl get gamemonitor minecraft --namespace monitoring --output jsonpath='{.status.atProvider.id}')
http_id=$(kubectl get httpmonitor auth --namespace monitoring --output jsonpath='{.status.atProvider.id}')
cat <<CONFIG | kubectl apply --filename -
apiVersion: uptimekuma.ops.finnes.dev/v1alpha1
kind: StatusPage
metadata:
  name: public
  namespace: monitoring
spec:
  providerConfigRef: {kind: ProviderConfig, name: default}
  forProvider:
    slug: crossplane-fixture
    title: Status fixture
    published: true
    publicGroupList:
      - name: Services
        weight: 1
        monitorList:
          - {id: $game_id, sendUrl: false}
          - {id: $http_id, sendUrl: false}
CONFIG
kubectl wait --namespace monitoring statuspage/public --for=condition=Ready --timeout=120s
kubectl rollout restart deployment/uptimekuma-controller --namespace crossplane-system
kubectl rollout status deployment/uptimekuma-controller --namespace crossplane-system --timeout=90s
kubectl wait --namespace monitoring gamemonitor/minecraft httpmonitor/auth statuspage/public --for=condition=Synced --timeout=90s
[[ $(kubectl get gamemonitor minecraft --namespace monitoring --output jsonpath='{.status.atProvider.id}') == "$game_id" ]]
# Stop the entire control plane. External Kuma must still serve and take fresh samples.
export KUMA_OUTAGE_START=$(date +%s)
docker stop "$cluster-control-plane" >/dev/null
sleep 25
KUMA_HTTP_ID="$http_id" python3 - <<'PY'
import datetime,json,os,urllib.request
base=os.environ['KUMA_TEST_ENDPOINT']
with urllib.request.urlopen(base+'/api/status-page/crossplane-fixture?verify=offline',timeout=10) as r:
    page=json.load(r)
assert len(page['publicGroupList'][0]['monitorList'])==2
with urllib.request.urlopen(base+'/api/status-page/heartbeat/crossplane-fixture?verify=offline',timeout=10) as r:
    heartbeat=json.load(r)['heartbeatList'][os.environ['KUMA_HTTP_ID']][-1]
assert heartbeat['status']==1,heartbeat
stamp=datetime.datetime.fromisoformat(heartbeat['time'].replace('Z','+00:00'))
if stamp.tzinfo is None:stamp=stamp.replace(tzinfo=datetime.timezone.utc)
assert (datetime.datetime.now(datetime.timezone.utc)-stamp).total_seconds()<30
assert stamp.timestamp()>int(os.environ['KUMA_OUTAGE_START'])
print('External status page and fresh healthy HTTP samples survive complete Kubernetes outage.')
PY
