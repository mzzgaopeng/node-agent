#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

bash vendor/k8s.io/code-generator/generate-groups.sh "deepcopy,client,informer,lister" \
  harmonycloud.cn/agents/node-agent/pkg/client harmonycloud.cn/agents/node-agent/pkg/apis \
  hlease:v1alpha1 \
  --output-base /root/go/src/ \
  --go-header-file ./hack/boilerplate.go.txt
