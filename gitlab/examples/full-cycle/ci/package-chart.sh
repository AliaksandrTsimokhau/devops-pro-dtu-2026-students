#!/bin/sh
set -eu
: "${CI_REGISTRY_IMAGE:?}" "${IMAGE_DIGEST:?}" "${CI_PIPELINE_URL:?}"
case "$CI_REGISTRY_IMAGE$CI_PIPELINE_URL" in
  *[!a-zA-Z0-9._:/-]*) echo 'Unexpected character in registry repository or pipeline URL' >&2; exit 1 ;;
esac
printf '%s\n' "$IMAGE_DIGEST" | grep -Eq '^sha256:[a-f0-9]{64}$'
mkdir -p dist
rm -rf dist/release-board
cp -R charts/release-board dist/release-board
# Stamp only the copy: the OCI package records the exact scanned image digest, never credentials.
sed -e "s|^  repository:.*|  repository: ${CI_REGISTRY_IMAGE}/web|" \
    -e "s|^  digest:.*|  digest: ${IMAGE_DIGEST}|" \
    -e "s|^pipelineURL:.*|pipelineURL: \"${CI_PIPELINE_URL}\"|" \
    charts/release-board/values.yaml > dist/release-board/values.yaml
