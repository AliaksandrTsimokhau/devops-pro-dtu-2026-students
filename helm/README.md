# Helm Labs — Learning Path

A hands-on Helm track, ordered from first chart to production CI/CD. Each lab builds on the
previous one and is **verified end-to-end** against a local `kind` cluster.

> **Environment used to verify these labs:** Helm **v4.2.0**, kind v0.31 (Kubernetes v1.35),
> 1 control-plane + 3 workers. Everything works on Helm 3 too (per-command differences are
> called out inline). On macOS the cluster runs on **podman**; reach Services with
> `kubectl port-forward` (NodePort isn't routable from the host on kind).

**Before you start:**

```sh
helm version                     # v3.x or v4.x
kubectl config current-context   # kind-<your-cluster>
kubectl get nodes                # all Ready
```

---

## The path

Follow these in order. Times are rough, hands-on.

### 1. [Create Your First Helm Chart](helm_chart_lab.md) · *foundations* · ~60 min
The core of Helm in three parts:
- **Part 1–2:** `helm create`, deploy, inspect with `--dry-run`, package & share (HTTP repo
  **and** OCI registry), and add a subchart dependency (Bitnami MariaDB).
- **Part 3:** *From hardcoded YAML to a templated chart* — build a chart by hand, converting
  plain manifests into templates one concept at a time (`.Values` → built-in objects →
  functions/pipelines → `_helpers.tpl` → `if`/`with`/`range`/`required`).

**You leave with:** a working chart and a solid grasp of Go templating.

### 2. [Umbrella Charts — Compose a 3-Tier App](helm_umbrella_lab.md) · *composition* · ~45 min
Wire `frontend` (nginx) + `backend` (whoami) + `database` (postgres) into one umbrella chart.
Learn **global values**, **parent-over-child overrides**, **`condition:`** toggles for whole
tiers, and how subcharts find each other by `<release>-<subchart>` DNS.

**Prereq:** templating from Lab 1, Part 3.

### 3. [Release Lifecycle, Upgrades & Rollback](helm_lifecycle_lab.md) · *day-2 ops* · ~30 min
Operate releases over time: `upgrade`, `history`, inspect any past revision, `rollback`, and
the must-have CI flag **`--rollback-on-failure`** (`--atomic` on Helm 3) that makes a broken
upgrade self-revert.

**You leave with:** confidence to deploy and recover safely.

### 4. [Ship a Chart with GitLab CI/CD](helm_gitlab_cicd_lab.md) · *automation* · ~45 min
Turn the manual commands into a pipeline: **lint → validate (`kubeconform`) → package →
publish (OCI) → gated staging/prod deploy**, with `values.schema.json` and
`--rollback-on-failure`. Includes a local podman-registry reproduction of the publish+deploy
flow.

**Prereq:** Labs 1 & 3 (packaging, OCI, rollback).

### 5. [Helmfile — Declarative Multi-Release Management](../helmfile/helmfile_instructions.md) · *advanced / production* · reference
A real production convention (RU) for managing many releases across many environments with
`helmfile`: pinned release names, namespaces, chart versions, and layered values. The
team-grade evolution of "many `helm upgrade` commands."

**Prereq:** comfortable with everything above.

---

## Reference companion

- **[Helm Template Syntax Cheatsheet](helm_cheatsheet.md)** — quick lookup for `.Values`,
  pipelines, `if`/`with`/`range`, `include`/`define`, `required`, `indent`/`nindent`. Keep it
  open during Labs 1–2. (For the verified, expanded version see the cheat-sheet table at the
  end of [helm_chart_lab.md](helm_chart_lab.md) Part 3.)

---

## At a glance

| # | Lab | Focus | Key skills |
|---|-----|-------|-----------|
| 1 | [helm_chart_lab](helm_chart_lab.md) | Foundations + templating | create, deploy, package, OCI, dependencies, Go templates |
| 2 | [helm_umbrella_lab](helm_umbrella_lab.md) | Composition | subcharts, globals, overrides, conditions |
| 3 | [helm_lifecycle_lab](helm_lifecycle_lab.md) | Day-2 ops | upgrade, history, rollback, auto-rollback |
| 4 | [helm_gitlab_cicd_lab](helm_gitlab_cicd_lab.md) | Automation | CI stages, schema validation, publish, gated deploy |
| 5 | [helmfile_instructions](../helmfile/helmfile_instructions.md) | Production | helmfile, environments, layered values |
| — | [helm_cheatsheet](helm_cheatsheet.md) | Reference | template syntax lookup |
