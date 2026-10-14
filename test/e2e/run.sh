#!/usr/bin/env bash
# Creates the kind cluster dcnet-e2e with multus (thick) and patchpanel-cni, runs the e2e tests and always deletes the cluster. Requires helm locally.
# The image patchpanel-cni:dev must exist locally (make image). Extra arguments are passed to go test.
set -euo pipefail
cd "$(dirname "$0")/../.."

cluster=dcnet-e2e
kctx=kind-$cluster
multus=https://raw.githubusercontent.com/k8snetworkplumbingwg/multus-cni/v4.3.1/deployments/multus-daemonset-thick.yml
tools=nicolaka/netshoot:v0.14

trap 'kind delete cluster --name "$cluster"' EXIT

kind delete cluster --name "$cluster"
kind create cluster --name "$cluster" --wait 120s

# The manifest references :snapshot-thick, which is not a release.
curl -sSfL "$multus" | sed 's#multus-cni:snapshot-thick#multus-cni:v4.3.1-thick#' | kubectl --context "$kctx" apply -f -

docker pull "$tools"
kind load docker-image patchpanel-cni:dev "$tools" --name "$cluster"

kubectl --context "$kctx" create namespace dcnet-system
kubectl --context "$kctx" label namespace dcnet-system pod-security.kubernetes.io/enforce=privileged
helm --kube-context "$kctx" install patchpanel-cni charts/patchpanel-cni -n dcnet-system \
  --set image.repository=patchpanel-cni --set image.tag=dev --set image.pullPolicy=Never
kubectl --context "$kctx" -n kube-system rollout status ds/kube-multus-ds --timeout=180s
kubectl --context "$kctx" -n dcnet-system rollout status ds/dcnet-agent --timeout=180s

go test -tags e2e -count=1 -timeout 30m "$@" ./test/e2e/...
