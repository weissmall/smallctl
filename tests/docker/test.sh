#!/usr/bin/env bash
set -Eeuo pipefail

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
image_name="smallctl-integration-test"

docker build --file "$repo_root/tests/docker/Dockerfile" --tag "$image_name" "$repo_root"
exec docker run --rm "$image_name"
