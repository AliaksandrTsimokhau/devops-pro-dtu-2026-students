#!/bin/sh
set -eu
: "${KUBE_CONTEXT:?Set the Agent CI context}" "${KUBE_NAMESPACE:?}" "${RELEASE:?}"
: "${APP_HOST:?}" "${INGRESS_IP:?Set the lab VM private IP}"
: "${REGISTRY_PULL_USER:?}" "${REGISTRY_PULL_PASSWORD:?Use a read_registry deploy token}"
: "${CI_REGISTRY:?}" "${CI_REGISTRY_IMAGE:?}" "${CI_REGISTRY_USER:?}" "${CI_REGISTRY_PASSWORD:?}"
: "${CHART_VERSION:?Missing publish-chart artifact}" "${CI_COMMIT_SHA:?}"
case "$KUBE_NAMESPACE" in default|'') echo 'A dedicated namespace is required' >&2; exit 1 ;; esac
case "$APP_HOST" in *[!a-zA-Z0-9.-]*) echo 'APP_HOST must be a hostname without scheme or port' >&2; exit 1 ;; esac

mkdir -p diagnostics
umask 077
private=$(mktemp -d)
previous=0
changed=false
finish() {
  result=$?
  trap - EXIT
  if [ "$result" -ne 0 ] && [ "$changed" = true ]; then
    if [ "$previous" -gt 0 ]; then
      echo "Release verification failed; rolling back to revision $previous" >&2
      helm rollback "$RELEASE" "$previous" --namespace "$KUBE_NAMESPACE" --kube-context "$KUBE_CONTEXT" --wait --timeout "${DEPLOY_TIMEOUT:-3m}" || echo 'Rollback failed; inspect the cluster' >&2
    else
      helm uninstall "$RELEASE" --namespace "$KUBE_NAMESPACE" --kube-context "$KUBE_CONTEXT" --wait || true
    fi
  fi
  # Do not collect `helm get values/manifest`: registry credentials are stored there.
  helm history "$RELEASE" --namespace "$KUBE_NAMESPACE" --kube-context "$KUBE_CONTEXT" > diagnostics/history.txt 2>&1 || true
  helm status "$RELEASE" --namespace "$KUBE_NAMESPACE" --kube-context "$KUBE_CONTEXT" > diagnostics/status.txt 2>&1 || true
  rm -rf "$private"
  exit "$result"
}
trap finish EXIT
printf '%s' "$REGISTRY_PULL_USER" > "$private/user"
printf '%s' "$REGISTRY_PULL_PASSWORD" > "$private/password"
echo "$CI_REGISTRY_PASSWORD" | helm registry login "$CI_REGISTRY" -u "$CI_REGISTRY_USER" --password-stdin
# Helm's JSON contains one numeric release version; chart versions are strings.
revision=$(helm status "$RELEASE" --namespace "$KUBE_NAMESPACE" --kube-context "$KUBE_CONTEXT" --output json 2>/dev/null | sed -n 's/.*"version":[[:space:]]*\([0-9][0-9]*\).*/\1/p' || true)
[ -z "$revision" ] || previous=$revision
helm upgrade --install "$RELEASE" "oci://${CI_REGISTRY_IMAGE}/charts/release-board" \
  --version "$CHART_VERSION" --namespace "$KUBE_NAMESPACE" \
  --kube-context "$KUBE_CONTEXT" --set-string ingress.host="$APP_HOST" \
  --set-string ingress.className="${INGRESS_CLASS:-traefik}" \
  --set registryCredentials.create=true --set-string registryCredentials.server="$CI_REGISTRY" \
  --set-file registryCredentials.username="$private/user" --set-file registryCredentials.password="$private/password" \
  --rollback-on-failure --wait --timeout "${DEPLOY_TIMEOUT:-3m}"
changed=true
helm test "$RELEASE" --namespace "$KUBE_NAMESPACE" --kube-context "$KUBE_CONTEXT" --logs --timeout 2m

# Same host header as the public URL; the CI pod reaches its own VM over the private network.
verified=false
for attempt in $(seq 1 15); do
  if curl --fail --silent --show-error --max-time 5 \
      --connect-to "${APP_HOST}:80:${INGRESS_IP}:${INGRESS_PORT:-80}" \
      "http://${APP_HOST}/api/release" > smoke-response.json && \
      grep -F "\"version\":\"${CI_COMMIT_SHA}\"" smoke-response.json >/dev/null; then
    verified=true
    break
  fi
  sleep 2
done
[ "$verified" = true ] || { echo 'Ingress did not serve the expected commit' >&2; exit 1; }
echo "Verified http://${APP_HOST} at commit ${CI_COMMIT_SHA}"
