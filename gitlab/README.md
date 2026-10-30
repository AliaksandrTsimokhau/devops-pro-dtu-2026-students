# GitLab CI/CD — Labs Index

Labs are ordered from simple to complex. Each lab builds on concepts from the previous ones.

## Порядок прохождения

1. [`gitlab_ci_lab_walkthrough.md`](gitlab_ci_lab_walkthrough.md) — модель выполнения GitLab CI: jobs, stages, artifacts, `needs`.
2. [`gitlab_runner_lab.md`](gitlab_runner_lab.md) — runner, executors и теги (нужно для лаб с self-hosted инстансом).
3. [`gitlab_ci_lab_rules.md`](gitlab_ci_lab_rules.md) — `rules`, `workflow:rules`.
4. [`gitlab_agent_k8s_lab.md`](gitlab_agent_k8s_lab.md) — деплой в kind через GitLab Agent (нужна [Helm-лаба](../helm/helm_gitlab_cicd_lab.md)).
5. [`gitlab_ci_lab_devsecops.md`](gitlab_ci_lab_devsecops.md) — сканеры безопасности в обычных container jobs.
6. Дальше — лабы Lab 1–8 ниже.

Лабы 1–8 нумерованы отдельно от этого списка и ссылаются друг на друга по номерам.

---

## Servercore stand

Если оператор выдал готовый Servercore стенд, начните с [servercore_gitlab.md](servercore_gitlab.md): GitLab уже установлен, jobs выполняются Kubernetes executor в вашем kind. Там описаны runner tags, HTTPS/Registry, DinD и доступ к кластеру через Agent. Устанавливать GitLab из `gitlab_in_kind.md` или `gitlab_in_minikube.md` на этом стенде не нужно.

## Prerequisites (all labs)

- GitLab account on GitLab.com or the Servercore instance. Cosign public keyless signing requires GitLab.com; paid governance features are optional.
- An available Docker or Kubernetes executor. On GitLab.com, check hosted runner eligibility and available compute quota before starting.
- `git` installed locally.

**For local self-hosted GitLab** — see [`gitlab_in_kind.md`](gitlab_in_kind.md).  
Minimum: 6 CPUs, **16 GiB RAM**, 40 GiB disk. Use **Podman rootful** (not Colima) — rootless mode cannot bind port 80. Allow ~40 min total (20 min waiting for GitLab pods).

> **Kubernetes executor note:** the bundled kind runner uses the Kubernetes executor. If its `config.toml` sets no default `image`, a job without `image:` fails with "no image specified". `gitlab_in_kind.md` sets `image = "alpine:3.22"`; declaring `image:` in every job is still the safe habit.

---

## Lab 1 — Helm Chart CI/CD

**File:** [`gitlab_ci_lab_helm.md`](gitlab_ci_lab_helm.md)  
**Duration:** 30–45 min  
**Difficulty:** ★☆☆☆☆

Build a pipeline that packages a Helm chart, lints it, and publishes it to the GitLab Package Registry.

**What you learn:**
- Basic `.gitlab-ci.yml` stages: `build → test → deploy`
- `helm package` / `helm lint` in CI
- Uploading artifacts to GitLab Package Registry via `curl`

**Requires:** Runner (no DinD), Package Registry enabled in project

---

## Lab 2 — Monorepo CI/CD

**File:** [`gitlab_ci_lab_monorepo.md`](gitlab_ci_lab_monorepo.md)  
**Duration:** 30–40 min  
**Difficulty:** ★★☆☆☆

Build a pipeline for a monorepo where each app rebuilds only when its own directory changes.

**What you learn:**
- `include: local:` with `rules: changes:` (GitLab 16.4+ feature)
- Why legacy `extends` approach is painful at scale
- Conditional pipeline inclusion — control plane pattern
- Gotchas: first push to new branch, `java/*` vs `java/**/*`

**Requires:** Runner only, GitLab 16.4+

---

## Lab 3 — CI/CD Components

**File:** [`gitlab_ci_lab_component.md`](gitlab_ci_lab_component.md)  
**Duration:** 30–45 min  
**Difficulty:** ★★☆☆☆

Create a reusable CI/CD Component (versioned, input-parametrized) and consume it from a second project.

**What you learn:**
- `templates/` directory convention for components
- `spec: inputs:` + `$[[ inputs.X ]]` syntax
- Semantic versioning via git tags
- Self-test pipeline for the component itself
- Optional: GitLab API access from CI via PAT

**Requires:** Runner only, GitLab 16.0+

---

## Lab 4 — Terraform State in GitLab

**File:** [`gitlab_ci_lab_tf.md`](gitlab_ci_lab_tf.md)  
**Duration:** 30–45 min  
**Difficulty:** ★★★☆☆

Use GitLab as a Terraform/OpenTofu HTTP state backend. Run `plan → apply → destroy` from CI.

**What you learn:**
- GitLab-managed Terraform state (HTTP backend)
- Pinned OpenTofu jobs with explicit HTTP backend configuration
- Saved plan and JSON artifacts passed to apply
- Manual approval gates (`when: manual`)

**Requires:** Container executor with access to `ghcr.io`, Maintainer/Owner for state writes, GitLab 18.4+ for the artifact access setting

---

## Lab 5 — Container Scanning with Trivy

**File:** [`gitlab_ci_lab_basic_trivy_scan.md`](gitlab_ci_lab_basic_trivy_scan.md)  
**Duration:** 30–40 min  
**Difficulty:** ★★★☆☆

Scan Docker images for CVEs using Trivy inside a GitLab pipeline, then scan the repository itself.

**What you learn:**
- Docker-in-Docker (DinD) setup in GitLab CI
- Trivy image scan + repo scan
- `gl-container-scanning-report.json` artifact format
- Failing pipeline on CRITICAL severity

**Requires:** **Privileged runner (DinD)** — works out of the box on gitlab.com shared runners.  
For local kind: runner must have `privileged: true`. On the Servercore stand the runner is unprivileged; ask the operator to enable privileged on your VM (see [Servercore onboarding](servercore_gitlab.md)).

> ⚠️ DinD does **not** work in Minikube/kind without a privileged runner.

---

## Lab 6 — Image Signing with Cosign (Keyless)

**File:** [`gitlab_ci_lab_cosign.md`](gitlab_ci_lab_cosign.md)  
**Duration:** 30–40 min  
**Difficulty:** ★★★★☆

Sign Docker images using Cosign keyless flow (GitLab OIDC → Sigstore Fulcio) and publish provenance to Rekor transparency log.

**What you learn:**
- `id_tokens:` / OIDC JWT in GitLab CI (GitLab 16.0+)
- Keyless signing: no private keys, identity = GitLab pipeline
- Cosign sign + verify with certificate identity
- SBOM generation with Syft (bonus)
- Admission control patterns (Kyverno)

**Requires:** **Privileged runner (DinD)**, Container Registry enabled, GitLab 16.0+ (for `id_tokens`).  
On the Servercore stand this lab is not supported: use GitLab.com. The public Sigstore flow requires **GitLab.com**. Self-managed needs its own Sigstore infrastructure; configuring OIDC alone is insufficient.

---

## Lab 7 — CD to 5 Environments (Child Pipelines)

**File:** [`gitlab_ci_lab_five_envs.md`](gitlab_ci_lab_five_envs.md)  
**Duration:** 60–90 min  
**Difficulty:** ★★★★★

Build a production-grade CD pipeline with parent → first-layer → grand-child architecture, deploying to 5 environments with different trigger policies.

**What you learn:**
- `trigger: include:` with `strategy: depend` (child pipelines)
- `forward: yaml_variables: true` — variable propagation
- `environment: on_stop:` — auto-destroy on MR close
- `rules:` matrix: branch / tag / manual triggers per environment
- GitLab-managed Terraform state per environment
- Shared OpenTofu jobs from Lab 4 (`.gitlab-ci/opentofu.yml`, OpenTofu 1.13.1), a separate state per environment
- Governance: CODEOWNERS and protected environments need Premium/Ultimate; on Free/CE restrict pushes and release tags
- Two infra options: **Path A — AWS**, **Path B — GitLab provider** (no AWS account needed)

**Requires:** finished Lab 4 (copy its `.gitlab-ci/opentofu.yml`), container executor, GitLab 18.4+.
Path B also needs an Owner role in a sandbox (sub)group and a PAT with `api` scope. Path A additionally requires AWS credentials.

---

## Lab 8 — Full CI/CD: live application and recovery

**File:** [gitlab_ci_lab_full_cycle.md](gitlab_ci_lab_full_cycle.md)
**Duration:** 2–3 hours after stand setup

Ship a working web application from a monorepo to the student's kind cluster. Build and scan one image, publish the image and Helm OCI chart to GitLab Registry, deploy through a namespace-scoped Agent, then verify the live commit through ingress.

Includes four break/fix exercises: API regression, synthetic leaked token, privileged workload and failed readiness rollout with rollback.

**Requires:** Servercore onboarding, Kubernetes executor tagged `kubernetes`, an unprivileged runner without DinD (images are built with buildah), trusted Registry HTTPS, Agent and reachable Traefik ingress. Start after Lab 2 and the [Helm](../helm/helm_gitlab_cicd_lab.md) and [Agent](gitlab_agent_k8s_lab.md) labs; Lab 5 is recommended but not required (it needs privileged, this lab does not).

---

## Dependency Map

```
Lab 1 (Helm basics)
    └── independent

Lab 2 (Monorepo)
    └── independent

Lab 3 (Components)
    └── builds on Lab 2 concepts (include, rules)

Lab 4 (Terraform)
    └── prerequisite mindset for Lab 7

Lab 5 (Trivy)
    └── independent (needs DinD)

Lab 6 (Cosign)
    └── builds on Lab 5 (DinD, container builds)
        └── recommended after Lab 5

Lab 7 (Five environments)
    └── builds on Lab 4 (Terraform)

Lab 8 (Live full cycle)
    └── builds on Lab 2, Helm deployment and Agent (Lab 5 recommended)
```

---

## Quick Reference — Runner Requirements

| Lab | DinD needed | Privileged | gitlab.com OK |
|-----|-------------|------------|--------------|
| 1 Helm | No | No | ✅ |
| 2 Monorepo | No | No | ✅ |
| 3 Components | No | No | ✅ |
| 4 Terraform | No | No | ✅ |
| 5 Trivy | **Yes** | **Yes** (stand: operator opt-in per VM) | ✅ |
| 6 Cosign | **Yes** | **Yes** (stand: not supported, use GitLab.com) | ✅ |
| 7 Five envs | No | No | ✅ |
| 8 Full cycle | No (buildah) | No | Student VM runner + Agent |

---

## Local GitLab Setup

See [`gitlab_in_kind.md`](gitlab_in_kind.md) for Kubernetes (kind) installation.  
See [`gitlab_in_minikube.md`](gitlab_in_minikube.md) for Minikube installation.
