# GitLab CI/CD — Labs Index

Labs are ordered from simple to complex. Each lab builds on concepts from the previous ones.

## Порядок прохождения

1. [`gitlab_ci_lab_walkthrough.md`](gitlab_ci_lab_walkthrough.md) — модель выполнения GitLab CI: jobs, stages, artifacts, `needs`.
2. [`gitlab_runner_lab.md`](gitlab_runner_lab.md) — runner, executors и теги (нужно для лаб с self-hosted инстансом).
3. [`gitlab_ci_lab_rules.md`](gitlab_ci_lab_rules.md) — `rules`, `workflow:rules`.
4. Дальше — лабы Lab 1–7 ниже.

Лабы 1–7 нумерованы отдельно от этого списка и ссылаются друг на друга по номерам.

---

## Prerequisites (all labs)

- GitLab account — [gitlab.com](https://gitlab.com) free tier works for every lab.
- GitLab Runner available (Shared runners are enabled by default on gitlab.com).
- `git` installed locally.

**For local self-hosted GitLab** — see [`gitlab_in_kind.md`](gitlab_in_kind.md).  
Minimum: 6 CPUs, **16 GiB RAM**, 40 GiB disk. Use **Podman rootful** (not Colima) — rootless mode cannot bind port 80. Allow ~40 min total (20 min waiting for GitLab pods).

> **Kubernetes executor note:** the bundled kind runner uses the Kubernetes executor. If its `config.toml` sets no default `image`, a job without `image:` fails with "no image specified". `gitlab_in_kind.md` sets `image = "alpine:latest"`; declaring `image:` in every job is still the safe habit.

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
- `gitlab-terraform` CLI wrapper
- `plan.json` artifact displayed in MR
- Manual approval gates (`when: manual`)

**Requires:** Runner only

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
For local kind: runner must have `privileged: true`.

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
Works best on **gitlab.com** — self-hosted needs OIDC issuer configured.

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
- CODEOWNERS + protected environments for governance
- Two infra options: **Path A — AWS**, **Path B — GitLab provider** (no AWS account needed)

**Requires:** Runner, Terraform knowledge, GitLab 16+.  
Path A additionally requires AWS credentials.

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
```

---

## Quick Reference — Runner Requirements

| Lab | DinD needed | Privileged | gitlab.com OK |
|-----|-------------|------------|--------------|
| 1 Helm | No | No | ✅ |
| 2 Monorepo | No | No | ✅ |
| 3 Components | No | No | ✅ |
| 4 Terraform | No | No | ✅ |
| 5 Trivy | **Yes** | **Yes** | ✅ |
| 6 Cosign | **Yes** | **Yes** | ✅ |
| 7 Five envs | No | No | ✅ |

---

## Local GitLab Setup

See [`gitlab_in_kind.md`](gitlab_in_kind.md) for Kubernetes (kind) installation.  
See [`gitlab_in_minikube.md`](gitlab_in_minikube.md) for Minikube installation.
