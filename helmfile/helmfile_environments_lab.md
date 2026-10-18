# LAB: Environments & DRY — staging vs prod from one file

In Lab 1 you deployed a stack with one `helmfile.yaml`. Real life has **the same stack in several
environments** — staging, prod, per-customer clusters — each with different replica counts,
feature toggles, and namespaces. Copy-pasting a helmfile per environment is how configs drift.

**Helmfile's `environments:` block keeps one source of truth** and layers environment-specific
values on top. This lab runs the same two releases into a `staging` and a `prod` namespace on the
**same `kind` cluster**, with different replicas and a feature flag that turns a whole release on
or off per environment. **Verified on kind.**

> **Verified environment:** Helmfile **v1.5.2**, Helm **v4.2.0**, kind v0.31, context
> `kind-<your-cluster>`. We simulate two environments as two namespaces (`hf-staging`, `hf-prod`) on
> one cluster — the same mechanics apply across real clusters via `kubeContext`.

```text
                 environments/staging.yaml      environments/prod.yaml
                 web.replicas: 1                 web.replicas: 3
                 podinfo.enabled: false          podinfo.enabled: true
                          │                                │
   helmfile.yaml.gotmpl ──┼──  -e staging ──► ns hf-staging: web(1)
   (one definition)       └──  -e prod    ──► ns hf-prod:    web(3) + podinfo
```

**Prereq:** Lab 1 ([Your First Helmfile](helmfile_101_lab.md)).

---

## Step 1: Project layout

```sh
mkdir -p ~/helmfile-env/environments && cd ~/helmfile-env
helm create web                       # the same local chart as Lab 1
```

```text
helmfile-env/
├── helmfile.yaml.gotmpl        # note the .gotmpl extension — see Step 3
├── environments/
│   ├── staging.yaml
│   └── prod.yaml
└── web/                        # helm create scaffold
```

---

## Step 2: Per-environment values

`environments/staging.yaml` — small, no podinfo:

```yaml
web:
  replicas: 1
podinfo:
  enabled: false
```

`environments/prod.yaml` — scaled up, podinfo on:

```yaml
web:
  replicas: 3
podinfo:
  enabled: true
```

These files hold **only what differs** between environments. The release definitions stay
identical.

---

## Step 3: The helmfile — `helmfile.yaml.gotmpl`

```yaml
repositories:
  - name: podinfo
    url: https://stefanprodan.github.io/podinfo

environments:
  staging:
    values:
      - environments/staging.yaml
  prod:
    values:
      - environments/prod.yaml
---
releases:
  - name: web
    chart: ./web
    namespace: "hf-{{ .Environment.Name }}"
    labels:
      tier: frontend
    values:
      - replicaCount: {{ .Values.web.replicas }}

  - name: podinfo
    chart: podinfo/podinfo
    version: 6.7.1
    namespace: "hf-{{ .Environment.Name }}"
    labels:
      tier: backend
    installed: {{ .Values | get "podinfo.enabled" false }}
```

Four DRY mechanics to notice:

- **`{{ .Environment.Name }}`** — the namespace is derived from the environment, so one
  definition produces `hf-staging` or `hf-prod`. No duplication.
- **`.Values`** — inside the helmfile, `.Values` means the **environment values** (the merged
  `staging.yaml`/`prod.yaml`), *not* chart values. (`.StateValues` is an explicit alias for it.)
- **`installed: {{ .Values | get "podinfo.enabled" false }}`** — a **feature flag**. When `false`,
  Helmfile *uninstalls* that release (or never installs it). Whole tiers toggle per environment.
- **`labels:`** — tag releases so you can target subsets with `--selector` (Step 6).

> ### Why `.gotmpl`?
> In **Helmfile v1**, a plain `helmfile.yaml` can't carry a template expression as an inline
> value (e.g. `replicaCount: {{ .Values.web.replicas }}`) — you'll hit:
> `yaml: invalid map key ... Add the .gotmpl file extension`. Renaming to
> **`helmfile.yaml.gotmpl`** tells Helmfile to fully render the file as a Go template first.
> Helmfile auto-discovers `helmfile.yaml.gotmpl`, so commands don't change.

---

## Step 4: See the difference *before* deploying

The same `template` command, two environments, two results:

```sh
helmfile deps

helmfile -e staging template | grep -m1 'replicas:'   # →   replicas: 1
helmfile -e prod    template | grep -m1 'replicas:'   # →   replicas: 3
```

Check the feature flag with `list`:

```sh
helmfile -e staging list      # podinfo  INSTALLED=false   web INSTALLED=true
helmfile -e prod    list      # podinfo  INSTALLED=true    web INSTALLED=true
```

One file; the environment flag flips both the scale and which releases exist.

---

## Step 5: Deploy both environments

```sh
helmfile -e staging sync
helmfile -e prod    sync
```

Verify each landed in its own namespace with the right shape:

```sh
kubectl get deploy -n hf-staging    # web only, 1 replica
kubectl get deploy -n hf-prod       # web (3 replicas) + podinfo
```

```text
# hf-staging
NAME   READY   UP-TO-DATE   AVAILABLE
web    1/1     1            1
# hf-prod
NAME      READY   UP-TO-DATE   AVAILABLE
podinfo   1/1     1            1
web       3/3     3            3
```

The feature flag worked: **podinfo exists in prod but not in staging**, from a single definition.

---

## Step 6: Target a subset with `--selector` (`-l`)

Labels let you operate on part of an environment — exactly how CI pipelines split work:

```sh
helmfile -e prod -l tier=backend list      # only podinfo
helmfile -e prod -l tier=backend diff      # would only touch the backend tier
helmfile -e prod -l name=web   sync        # re-sync just the web release
```

> In a CI pipeline you'd run `helmfile -e prod -l tier=frontend sync` in one job and
> `-l tier=backend` in another — same file, parallel deploys.

---

## Step 7: Clean up

```sh
helmfile -e staging destroy
helmfile -e prod    destroy
kubectl delete ns hf-staging hf-prod --ignore-not-found
```

---

## Real clusters: `kubeContext`

On one kind cluster we separated environments by namespace. Across **real** clusters, pin each
environment to a kube-context so a `-e prod` can never hit the staging cluster:

```yaml
environments:
  staging:
    kubeContext: kind-staging
    values: [environments/staging.yaml]
  prod:
    kubeContext: prod-eks
    values: [environments/prod.yaml]
```

`kubeContext` makes the target cluster part of the declarative state — a critical guardrail.

## What You Learned

- One `helmfile.yaml.gotmpl` + an `environments:` block = many environments, **zero duplication**
- `{{ .Environment.Name }}` derives namespaces (and more) per environment
- Inside a helmfile, `.Values` (= `.StateValues`) means **environment** values, not chart values
- `installed: {{ .Values | get "x.enabled" false }}` toggles whole releases per environment
- `--selector`/`-l` targets release subsets — the basis for split CI jobs
- Pin `kubeContext` per environment so deploys can't hit the wrong cluster
- Helmfile v1 needs the **`.gotmpl`** extension for inline template expressions

Next: **[Ordering, Health & Hooks](helmfile_ordering_lab.md)** — control the **order** releases
deploy in (`needs:`), wait for health (`helmDefaults`), and run `hooks:` around a sync. 🧩
