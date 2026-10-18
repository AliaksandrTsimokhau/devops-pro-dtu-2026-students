# LAB: Ship a Helm Chart with GitLab CI/CD

This lab turns the manual `helm` commands from the other labs into an **automated GitLab
pipeline**: every push lints and validates the chart, packages it, publishes it to GitLab's
**OCI registry**, and deploys it to Kubernetes — safely, with auto-rollback.

> **What was verified, and how.** The chart commands in this pipeline — `helm lint`,
> `values.schema.json` validation, `helm template`, `helm package`, `helm push`/`helm pull` to
> an **OCI registry** (a local `registry:2` run on **podman**), and
> `helm upgrade --install … --rollback-on-failure` deploying **from the OCI registry** into the
> `kind-dtu` cluster — were all run and confirmed locally with Helm v4.2.0. The `.gitlab-ci.yml`
> wires those exact commands into CI. Tools used only for extra gates (`kubeconform`,
> `helm-docs`, `chart-testing`) run inside their container images in CI.
>
> Образы в примерах — `alpine/helm:4.2.0` (Helm 4). Флаг `--rollback-on-failure` есть только в Helm 4;
> в Helm 3 тот же механизм назывался `--atomic`.

---

## The pipeline at a glance

```text
 push / MR                                              merge to main        manual
    │                                                        │                  │
    ▼                                                        ▼                  ▼
┌────────┐   ┌───────────┐   ┌──────────┐   ┌─────────────┐   ┌──────────┐   ┌──────────┐
│ lint   │──►│ validate  │──►│ package  │──►│  publish    │──►│ deploy   │──►│ deploy   │
│ (helm  │   │ (render + │   │ (helm    │   │ (helm push  │   │ staging  │   │ prod     │
│  lint) │   │ kubeconf.)│   │ package) │   │  → OCI reg) │   │ (auto)   │   │ (manual) │
└────────┘   └───────────┘   └──────────┘   └─────────────┘   └──────────┘   └──────────┘
   fast feedback on every commit         artifacts + registry      gated deploys
```

---

## Prerequisites

- A GitLab project containing your chart at `./chart`. Either use the chart from
  [helm_chart_lab.md](helm_chart_lab.md) (copy it into `chart/`), or,
  if that lab is not done yet, create a minimal chart:
  ```sh
  helm create mychart
  mv mychart chart        # CHART_DIR в pipeline = chart; имя чарта берётся из Chart.yaml (mychart)
  ```
- GitLab's **Container/OCI Registry** enabled (Settings → Packages & registries).
- A way for CI to reach your cluster. Two options:
  - **GitLab Kubernetes Agent** (recommended) — secure, pull-based. See
    [gitlab_agent_k8s_lab.md](../gitlab/gitlab_agent_k8s_lab.md).
  - A **`KUBE_CONFIG`** CI/CD variable (base64-encoded kubeconfig) — simplest for a lab.
    *Mask & protect it.*

> **Важно про gitlab.com и kind.** Вариант `KUBE_CONFIG` работает только если API-сервер кластера
> доступен из раннера. Kubeconfig от kind на вашем ноутбуке указывает на `127.0.0.1` — gitlab.com
> до него не достучится. Поэтому на gitlab.com выполните эту лабу до стадии **publish** (lint,
> validate, package, publish), а деплой делайте через [gitlab_agent_k8s_lab.md](../gitlab/gitlab_agent_k8s_lab.md)
> или через собственный (project) runner, который находится в одной сети с кластером.

GitLab injects these built-in variables we rely on: `CI_REGISTRY`, `CI_REGISTRY_USER`,
`CI_REGISTRY_PASSWORD`, `CI_REGISTRY_IMAGE`, `CI_COMMIT_SHORT_SHA`, `CI_DEFAULT_BRANCH`.

---

## Step 1: Add a `values.schema.json` (fail fast on bad input)

Before any pipeline, give your chart a schema so wrong values are rejected at `helm lint`/
`install` time — locally **and** in CI. Save as `chart/values.schema.json`:

```json
{
  "$schema": "https://json-schema.org/draft-07/schema#",
  "type": "object",
  "required": ["replicaCount", "image"],
  "properties": {
    "replicaCount": { "type": "integer", "minimum": 1 },
    "image": {
      "type": "object",
      "required": ["repository"],
      "properties": {
        "repository": { "type": "string" },
        "tag":        { "type": "string" }
      }
    }
  }
}
```

Verify it works (this exact check runs in the pipeline's `lint` stage):

```sh
helm lint ./chart                       # passes
helm lint ./chart --set replicaCount=0  # FAILS: at '/replicaCount': minimum: got 0, want 1
```

---

## Step 2: The pipeline — `.gitlab-ci.yml`

```yaml
stages: [lint, validate, package, publish, deploy]

variables:
  CHART_DIR: chart
  # OCI repo for charts inside this project's registry
  CHART_REGISTRY: oci://$CI_REGISTRY_IMAGE/charts

# A small image that ships the helm CLI for most jobs (Helm 4, см. пометку выше)
.helm:
  image:
    name: alpine/helm:4.2.0
    entrypoint: [""]

# ---------- 1. LINT ----------
lint:
  extends: .helm
  stage: lint
  script:
    - helm lint "$CHART_DIR"                       # also runs values.schema.json validation

# ---------- 2. VALIDATE (render the chart, then check the manifests) ----------
render:
  extends: .helm
  stage: validate
  script:
    - helm template release "$CHART_DIR" > rendered.yaml
  artifacts:
    paths: [rendered.yaml]
    expire_in: 1 day

kubeconform:
  stage: validate
  needs: [render]                                  # берём rendered.yaml из job render
  image:
    name: ghcr.io/yannh/kubeconform:latest-alpine  # проверь: актуальный тег образа kubeconform
    entrypoint: [""]
  script:
    - kubeconform -strict -summary -kubernetes-version 1.30.0 rendered.yaml

# ---------- 3. PACKAGE ----------
package:
  extends: .helm
  stage: package
  script:
    - helm package "$CHART_DIR" --destination dist/
  artifacts:
    paths: [dist/*.tgz]
    expire_in: 1 week

# ---------- 4. PUBLISH to the GitLab OCI registry ----------
publish:
  extends: .helm
  stage: publish
  needs: [package]
  script:
    - echo "$CI_REGISTRY_PASSWORD" | helm registry login "$CI_REGISTRY" -u "$CI_REGISTRY_USER" --password-stdin
    - helm push dist/*.tgz "$CHART_REGISTRY"
  rules:
    - if: '$CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH'   # publish only from main

# ---------- 5. DEPLOY ----------
.deploy:
  image:
    name: alpine/helm:4.2.0
    entrypoint: [""]
  stage: deploy
  before_script:
    - export KUBECONFIG=/tmp/kubeconfig
    - echo "$KUBE_CONFIG" | base64 -d > "$KUBECONFIG"   # KUBE_CONFIG = base64 kubeconfig CI var
  script:
    - echo "$CI_REGISTRY_PASSWORD" | helm registry login "$CI_REGISTRY" -u "$CI_REGISTRY_USER" --password-stdin
    - CHART_NAME=$(grep '^name:' "$CHART_DIR/Chart.yaml" | awk '{print $2}')
    - |
      helm upgrade --install "$RELEASE" "$CHART_REGISTRY/$CHART_NAME" \
        --namespace "$NAMESPACE" --create-namespace \
        --rollback-on-failure --timeout 3m --wait
    - helm status "$RELEASE" --namespace "$NAMESPACE"

deploy:staging:
  extends: .deploy
  variables: { RELEASE: app-staging, NAMESPACE: staging }
  environment: { name: staging }
  rules:
    - if: '$CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH'   # auto-deploy main to staging

deploy:prod:
  extends: .deploy
  variables: { RELEASE: app-prod, NAMESPACE: prod }
  environment: { name: production }
  rules:
    - if: '$CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH'
      when: manual                                    # prod requires a human click
  allow_failure: false
```

> **Про образ деплоя.** Деплой использует образ чарта по умолчанию (`values.yaml`). Чтобы привязать
> запуск к коммиту, job `build` должен собрать и отправить образ в registry; тогда в deploy передайте
> **оба** параметра: `--set image.repository=$CI_REGISTRY_IMAGE --set image.tag=$CI_COMMIT_SHORT_SHA`.
> Только тег без репозитория даст `ImagePullBackOff`: образ `nginx:<sha>` не существует. Лаба сама
> образ не собирает, поэтому `--set` в примере не используется.

> **Про `KUBE_CONFIG`.** Создать переменную на ноутбуке можно так (работает и на macOS, и на Linux):
> `base64 < ~/.kube/config | tr -d '\n'` — вывод вставьте в **Settings → CI/CD → Variables** как
> masked и protected.

---

## Step 3: Understand each stage

| Stage | Command (verified locally) | Why it's in CI |
|-------|----------------------------|----------------|
| **lint** | `helm lint` | Catches template/`Chart.yaml`/schema errors in seconds, on every commit |
| **validate** | `helm template` → `kubeconform` (job `render` → job `kubeconform`) | Confirms the rendered YAML is valid Kubernetes *before* it reaches a cluster |
| **package** | `helm package` | Produces the versioned `.tgz` artifact |
| **publish** | `helm registry login` + `helm push … oci://…` | Stores the chart in GitLab's OCI registry, addressable by version |
| **deploy** | `helm upgrade --install … --rollback-on-failure` | Applies it, waits for health, **auto-reverts on failure** |

Two decisions worth calling out:

- **`--rollback-on-failure --wait --timeout 3m`** is the safety net (Helm 3 called the same
  behaviour `--atomic`). A deploy that doesn't become healthy reverts itself — the pipeline goes
  red, the cluster stays green. Verified in [helm_lifecycle_lab.md](helm_lifecycle_lab.md), Step 5.
- **Тег образа** ties each deploy to the exact commit, but only when `build` публикует образ (см.
  пометку в Step 2). Без этого деплой берёт образ из `values.yaml`.

---

## Step 4: Reproduce the publish + deploy locally (what CI does)

You can run the pipeline's core against `kind` using a throwaway OCI registry — exactly the
flow that was verified for this lab. Use **podman** (the runtime behind `kind-dtu`):

```sh
# stand up a local OCI registry (stand-in for GitLab's)
podman run -d -p 5111:5000 --name cireg docker.io/library/registry:2

# package + publish (local registry is plaintext → --plain-http)
helm package ./chart --destination dist/
helm push dist/*.tgz oci://localhost:5111/charts --plain-http

# deploy straight from the registry, with the CI safety flags
CHART_NAME=$(grep '^name:' ./chart/Chart.yaml | awk '{print $2}')
helm upgrade --install ci-app oci://localhost:5111/charts/$CHART_NAME \
  --namespace ci-demo --create-namespace \
  --rollback-on-failure --timeout 90s --plain-http

kubectl get deploy,svc -n ci-demo
```

> В GitLab CI на **gitlab.com** registry работает по HTTPS: `--plain-http` не нужен, а
> `localhost:5111` заменяется на `$CI_REGISTRY_IMAGE`; аутентификация — через `helm registry login`.
>
> **Self-hosted GitLab в kind** (из [gitlab_in_kind.md](../gitlab/gitlab_in_kind.md))
> отдаёт registry по **plain http**. Тогда `--plain-http` нужен во всех командах с реестром:
> `helm registry login … --plain-http`, `helm push … --plain-http`, `helm upgrade … --plain-http`.
> Добавьте его в `script:` job'ов publish и deploy. Проверено: эти флаги есть в `helm push --help`,
> `helm upgrade --help` и `helm registry login --help` (Helm v4.3.0).

### Cleanup (после лабы)

```sh
# локальный стенд
helm uninstall ci-app -n ci-demo
kubectl delete ns ci-demo
podman rm -f cireg
rm -rf dist/

# релизы, созданные CI в кластере (если деплоили)
helm uninstall app-staging -n staging
helm uninstall app-prod -n prod
kubectl delete ns staging prod          # только если namespace создан лабой
```

- **Chart в registry:** GitLab UI → **Deploy → Package registry** (или Container registry, в
  зависимости от версии UI) → удалите пакет чарта. **проверь:** где в вашей версии UI отображаются
  OCI-чарты и как удалить пакет через `glab`/API.
- **`KUBE_CONFIG`:** **Settings → CI/CD → Variables** → удалите переменную (она содержит доступ к кластеру).
- Для self-hosted: остановите стенд kind по [gitlab_in_kind.md](../gitlab/gitlab_in_kind.md), если он больше не нужен.

---

## Step 5 (optional gates): chart-testing & helm-docs

Mature chart repos add two more jobs. They need their own images, so they're optional here:

```yaml
ct-lint:                         # chart-testing: lint + version-bump checks across changed charts
  stage: lint
  image: quay.io/helmpack/chart-testing:v3.11.0
  script:
    - ct lint --charts "$CHART_DIR" --validate-maintainers=false

docs:                            # fail the build if values are undocumented
  stage: validate
  image: jnorwood/helm-docs:v1.14.2
  script:
    - helm-docs --chart-search-root "$CHART_DIR"
    - git diff --exit-code "$CHART_DIR/README.md"   # README out of date → red pipeline
```

---

## Common pitfalls

- **`helm push` 401/403** — the job didn't `helm registry login`, or the registry isn't enabled
  on the project. The built-in `$CI_REGISTRY_*` vars only work in protected/normal CI context.
- **Deploy can't reach the cluster** — `KUBE_CONFIG` not set/masked, or its server URL isn't
  reachable from the runner (kind на ноутбуке с gitlab.com — именно этот случай, см. Prerequisites).
- **`ImagePullBackOff` после деплоя** — переданы `image.tag` без `image.repository`, либо образ с этим
  тегом не был собран. Уберите `--set` или соберите образ.
- **`not found` при deploy** — имя чарта в registry берётся из `name:` в `Chart.yaml`, а не из имени
  каталога. Поэтому имя вычисляется из `Chart.yaml`.
- **OCI URL shape** — push to a *repository prefix* (`oci://…/charts`); pull/install reference
  the *chart name* (`oci://…/charts/mychart`). Mixing these up is the #1 OCI error.
- **`--plain-http`** нужен только для реестров по HTTP (локальный `registry:2`, self-hosted GitLab в kind). На gitlab.com (HTTPS) его не используйте.
- **`--rollback-on-failure` не распознан** — образ с Helm 3; используйте `alpine/helm:4.2.0` (Helm 4).

## What You Learned

- Wrap the full chart workflow — lint → validate → package → publish → deploy — into GitLab CI
- Fail fast with `values.schema.json` and `kubeconform` before anything touches a cluster
- Publish charts to GitLab's **OCI registry** with `helm registry login` + `helm push`
- Deploy from OCI with `--rollback-on-failure` so bad releases self-heal
- Gate environments: auto-deploy `staging` on `main`, require a manual click for `prod`
- Связывать деплой с коммитом можно через `image.repository` + `image.tag`, когда build job публикует образ

Your chart now ships itself: commit → validated, versioned, published, and safely deployed. 🚀
