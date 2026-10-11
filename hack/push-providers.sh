#!/usr/bin/env bash
# Pushes the providers in the dependency lock file of an OpenTofu configuration to OCI repositories, for the
# OpenTofu controller to install them from with PROVIDER_MIRROR=MIRROR. Every provider ends up in
# MIRROR/<namespace>/<type>, tagged with its version, for the platforms of the controller image.
#
# Usage: hack/push-providers.sh DIR MIRROR
#
# ORAS_FLAGS passes additional flags to "oras cp", such as --to-insecure for a registry with a self-signed
# certificate.
set -euo pipefail

dir=${1:?usage: hack/push-providers.sh DIR MIRROR}
mirror=${2:?usage: hack/push-providers.sh DIR MIRROR}
platforms=(linux_amd64 linux_arm64)
read -r -a oras_flags <<<"${ORAS_FLAGS:-}"

work=$(mktemp -d "${TMPDIR:-/tmp}/providers.XXXXXX")
trap 'rm -rf "$work"' EXIT

# "tofu providers mirror" downloads the packages that match the hashes in the lock file. Modules of the
# configuration have to be installed first, since they declare providers too.
cp -R "$dir/." "$work/config"
rm -rf "$work/config/.terraform"
(
  cd "$work/config"
  tofu init -backend=false -input=false -lockfile=readonly >/dev/null
  tofu providers mirror "${platforms[@]/#/-platform=}" "$work/packages" >/dev/null
)

for provider in "$work"/packages/registry.opentofu.org/*/*/; do
  provider=${provider%/}
  type=$(basename "$provider")
  namespace=$(basename "$(dirname "$provider")")
  version=$(find "$provider" -name "terraform-provider-${type}_*_linux_amd64.zip" -exec basename {} \; |
    sed -E "s/^terraform-provider-${type}_(.*)_linux_amd64\.zip$/\1/")
  layout="$work/layouts/$namespace/$type"
  for platform in "${platforms[@]}"; do
    # The title of the layer is the file name, as in the origin registry.
    (cd "$provider" && oras push --artifact-type application/vnd.opentofu.provider-target \
      --artifact-platform "${platform/_//}" --oci-layout "$layout:$platform" \
      "terraform-provider-${type}_${version}_${platform}.zip:archive/zip" >/dev/null)
  done
  oras manifest index create --artifact-type application/vnd.opentofu.provider \
    --oci-layout "$layout:${version//+/_}" "${platforms[@]}" >/dev/null
  oras cp ${oras_flags[@]+"${oras_flags[@]}"} --from-oci-layout "$layout:${version//+/_}" \
    "$mirror/$namespace/$type:${version//+/_}" >/dev/null
  echo "pushed $namespace/$type $version to $mirror/$namespace/$type"
done
