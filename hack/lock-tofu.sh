#!/usr/bin/env bash
# Writes the dependency lock file of every example with the checksums of all platforms the controller image
# is built for and the controller is developed on. The controller runs "tofu init -lockfile=readonly", which
# fails for a platform missing here, and hack/push-providers.sh mirrors the locked providers.
set -euo pipefail

cd "$(dirname "$0")/.."

for dir in examples/opentofu/*/; do
  echo "locking ${dir}"
  tofu -chdir="$dir" init -backend=false -input=false >/dev/null
  tofu -chdir="$dir" providers lock \
    -platform=linux_amd64 \
    -platform=linux_arm64 \
    -platform=darwin_arm64 \
    -platform=darwin_amd64
done
