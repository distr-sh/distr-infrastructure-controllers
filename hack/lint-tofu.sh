#!/usr/bin/env bash
# Checks the formatting of every OpenTofu file, then validates and tflints every example. Examples must not
# need a change of their lock file.
set -euo pipefail

cd "$(dirname "$0")/.."
root=$(pwd)

tofu fmt -check -diff -recursive examples

for dir in examples/opentofu/*/; do
  echo "validating ${dir}"
  tofu -chdir="$dir" init -backend=false -input=false -lockfile=readonly >/dev/null
  tofu -chdir="$dir" validate -no-color
  tflint --chdir="$dir" --config="$root/.tflint.hcl" --format=compact
done
