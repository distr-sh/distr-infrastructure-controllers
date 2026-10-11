#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

for chart in examples/opentofu/*/deploy/charts/*/; do
  echo "linting ${chart}"
  helm lint --strict "$chart" --set instanceName=lint-test
  helm template lint "$chart" --set instanceName=lint-test --set stateEncryptionPassphrase=lint-passphrase-0123 |
    kubeconform -strict -summary -kubernetes-version 1.35.0 -
done
