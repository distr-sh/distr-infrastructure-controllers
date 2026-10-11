#!/usr/bin/env bash
# Packages an OpenTofu configuration as the zip archive of an OpenTofu module package, which
# "oras push --artifact-type=application/vnd.opentofu.modulepkg REFERENCE PACKAGE:archive/zip" pushes.
#
# Usage: hack/package-module.sh DIR PACKAGE [EXTRA_FILE...]
#
# The package contains DIR without its deploy directory, the files of a local "tofu init" and state files,
# and the extra files, such as overrides. It requires the dependency lock file, since the OpenTofu
# controller installs exactly the locked providers. Equal content yields an equal package, so pushing a
# package again does not make the controllers download it again.
set -euo pipefail

dir=${1:?usage: hack/package-module.sh DIR PACKAGE [EXTRA_FILE...]}
package=${2:?usage: hack/package-module.sh DIR PACKAGE [EXTRA_FILE...]}
shift 2

[[ -f "$dir/.terraform.lock.hcl" ]] || {
  echo "$dir has no .terraform.lock.hcl, run \"tofu init\" or hack/lock-tofu.sh first" >&2
  exit 1
}

staging=$(mktemp -d "${TMPDIR:-/tmp}/module.XXXXXX")
trap 'rm -rf "$staging"' EXIT

tar -C "$dir" --exclude=./deploy --exclude=./.terraform --exclude='*.tfstate' --exclude='*.tfstate.*' \
  --exclude='*.tfplan' --exclude=.DS_Store -cf - . | tar -C "$staging" -xf -
for extra in "$@"; do
  cp "$extra" "$staging/"
done

find "$staging" -exec touch -h -t 198001010000 {} +
mkdir -p "$(dirname "$package")"
package=$(cd "$(dirname "$package")" && pwd)/$(basename "$package")
rm -f "$package"
(cd "$staging" && LC_ALL=C find . -type f | LC_ALL=C sort | sed 's|^\./||' | zip -q -X -D "$package" -@)
echo "$package"
