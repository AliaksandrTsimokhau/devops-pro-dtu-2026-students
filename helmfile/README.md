# Helmfile Labs — Learning Path

A hands-on Helmfile track, ordered from your first `helmfile.yaml` to production multi-tier
conventions. Labs 1–4 are **verified end-to-end** against a local `kind` cluster; Labs 5–6 are
the production patterns those fundamentals build toward.

> **Environment used to verify Labs 1–4:** Helmfile **v1.5.2**, Helm **v4.2.0**, kind v0.31
> (Kubernetes v1.35), context `kind-<your-cluster>`. Charts used are public and auth-free (a `helm
> create` local chart + `podinfo` from its HTTP repo / GHCR OCI). On `kind`, reach Services with
> `kubectl port-forward` (NodePort isn't routable from the macOS host).

**Before you start:**

```sh
helmfile version                 # v1.x
helm version                     # v3.x or v4.x
kubectl config current-context   # kind-<your-cluster>
kubectl get nodes                # all Ready
```

> **Prereq for the whole track:** comfort with Helm itself. If charts/templating are new, do the
> [Helm labs](../helm/README.md) first — Helmfile *orchestrates* Helm, it doesn't replace it.

---

## The path

### 1. [Your First Helmfile](helmfile_101_lab.md) · *foundations* · ~30 min
Wrap one local chart + one remote chart into a single `helmfile.yaml` and run the everyday loop:
`deps → template → diff → sync → list → destroy`. The core mental model — **many releases as one
declarative file; Helmfile calls Helm under the hood.**

**You leave with:** a working multi-release helmfile and the command loop in muscle memory.

### 2. [Environments & DRY](helmfile_environments_lab.md) · *multi-env* · ~30 min
One file → `staging` and `prod` (as two namespaces on kind) with different replica counts and a
feature flag that toggles a whole release per environment. Covers `environments:`,
`{{ .Environment.Name }}`, `.Values`/`.StateValues`, `installed:` flags, `--selector` labels, and
why Helmfile v1 needs the **`.gotmpl`** extension.

**Prereq:** Lab 1.

### 3. [Ordering, Health & Hooks](helmfile_ordering_lab.md) · *day-2 / safety* · ~30 min
Make releases deploy **in order** with `needs:`, gate them on health with `helmDefaults`
(`wait`/`timeout`/`cleanupOnFail`), and run `hooks:` around a sync. Proves the safety net: a
broken dependency **stops the rollout** instead of half-deploying the stack.

**Prereq:** Labs 1–2.

### 4. [Remote Charts & Values from git / OCI](helmfile_remote_lab.md) · *sourcing* · ~30 min
Point `chart:` and `values:` at **git** (`git::…?ref=`), **OCI** (`oci://…` + `version:`), and
HTTP — version-pinned, no vendoring. Includes a local podman `registry:2` + `--plain-http` path
for insecure registries.

**Prereq:** Labs 1–3.

### 5. [Production Repo with GitLab CI](helmfile_lab.md) · *production* · ~90 min
Assemble everything into a real GitLab repo: layered abstraction tiers (`bases:`, sub-helmfiles),
local **and** registry charts, `environments.yaml` with `kubeContext`, release templates, and a
`scan → validate → plan → deploy` pipeline (`helmfile diff` as plan, `helmfile apply` as deploy).

**Prereq:** Labs 1–4.

### 6. [Helmfile Conventions (production reference, RU)](helmfile_instructions.md) · *reference*
A real team's operating convention: pinned release names/namespaces, the layered values ordering
(`values.yaml` → `.gotmpl` → `values-tier-*` → `values-{env}`), add/remove/rename release
migrations, and the full day-to-day command + FAQ + troubleshooting reference.

---

## At a glance

| # | Lab | Focus | Key skills |
|---|-----|-------|-----------|
| 1 | [helmfile_101_lab](helmfile_101_lab.md) | Foundations | deps/template/diff/sync, local + remote charts |
| 2 | [helmfile_environments_lab](helmfile_environments_lab.md) | Multi-env DRY | environments, `.Environment`/`.Values`, flags, selectors, `.gotmpl` |
| 3 | [helmfile_ordering_lab](helmfile_ordering_lab.md) | Ordering & safety | `needs`, `helmDefaults` (wait/timeout/cleanup), `hooks` |
| 4 | [helmfile_remote_lab](helmfile_remote_lab.md) | Remote sourcing | git/OCI/HTTP charts & values, `?ref=`/`version:` pinning |
| 5 | [helmfile_lab](helmfile_lab.md) | Production repo | bases, sub-helmfiles, kubeContext, GitLab CI |
| 6 | [helmfile_instructions](helmfile_instructions.md) | Reference | conventions, values layering, command/FAQ reference |

---

## Companion

- **Slides:** [`helmfile-slides.md`](../07-helmfile-slides.pdf) — the lecture deck these labs accompany.
- **Helm track:** [`../../helm/labs/README.md`](../helm/README.md) — do this first if Helm
  itself is new.
