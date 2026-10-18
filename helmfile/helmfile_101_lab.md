# LAB: Your First Helmfile

> Cluster: the name is the one you chose in module 02 Lab 01. Below, `$CLUSTER` is derived from the current kubectl context.

You already know how to `helm install` one chart at a time. The moment you have **two or more**
releases — an app plus its database, or a stack of platform components — you start juggling a
pile of `helm upgrade --install` commands, each with its own flags, namespace, and values.

**Helmfile turns that pile of commands into one declarative file.** This lab builds the smallest
useful `helmfile.yaml` — one local chart + one remote chart — and drives it through the everyday
loop: `deps → template → diff → sync → list → destroy`. Everything here is **verified on a local
`kind` cluster**.

> **Verified environment:** Helmfile **v1.5.2** (lab text) and **v1.8.1** (a later check: the same steps
> passed), Helm **v4.2.0** and **v4.3.0**, kind v0.31 (Kubernetes v1.35) and kind v0.33 (Kubernetes v1.37),
> context `kind-<your cluster name>`. Your version may differ: run `helmfile version` and compare the output
> steps with it. Reach Services with `kubectl port-forward`
> (NodePort isn't routable from the host on kind).

```text
        helmfile.yaml                          one command
   ┌────────────────────┐                   ┌──────────────┐
   │ release: web        │  ── helmfile ──►  │ helm upgrade │ ──► web      (local chart)
   │ release: podinfo    │      sync         │   --install  │ ──► podinfo  (remote chart)
   └────────────────────┘                   └──────────────┘
     desired state, in git              Helmfile calls Helm under the hood
```

---

## Prerequisites

```sh
helmfile version                 # v1.x
helm version                     # v3.x or v4.x
CLUSTER=$(kubectl config current-context | sed 's/^kind-//')
kubectl config current-context   # expect: kind-$CLUSTER
kubectl get nodes                # all Ready

# helm-diff plugin: required by `helmfile diff` (Step 5)
helm plugin list | grep -i diff || helm plugin install https://github.com/databus23/helm-diff --verify=false
# Helm 4: без --verify=false установка из GitHub падает ("does not support verification"); проверь: флаги helm plugin install и формат установки на Helm 4 по release notes helm-diff
# alternative: `helmfile init` (check with `helmfile init --help` whether it installs helm-diff in your version)
```

> Helmfile **does not replace Helm** — it *orchestrates* it. Every release below becomes a
> `helm upgrade --install` under the hood.

---

## Step 1: A local chart to deploy

Any chart works. Scaffold the standard nginx starter chart:

```sh
mkdir -p ~/helmfile-101 && cd ~/helmfile-101
helm create web
```

That gives you `web/` — a complete chart (Deployment + Service + ServiceAccount) you'll
reference **by path**.

---

## Step 2: Write `helmfile.yaml`

Create `helmfile.yaml` next to `web/`:

```yaml
# Remote chart repositories (classic Helm HTTP repos)
repositories:
  - name: podinfo
    url: https://stefanprodan.github.io/podinfo

releases:
  # 1) A LOCAL chart — referenced by path
  - name: web
    chart: ./web
    namespace: hf-101

  # 2) A REMOTE chart — pulled from a repo, pinned to a version
  - name: podinfo
    chart: podinfo/podinfo
    version: 6.7.1
    namespace: hf-101
```

Three things to notice:

- **`repositories:`** declares Helm repos *inside* the helmfile — no separate `helm repo add`.
- **Local vs remote** — `chart: ./web` is a path; `chart: podinfo/podinfo` is `<repo>/<chart>`.
- **`version:`** pins the remote chart. Always pin remote charts; never track "latest".

> **Namespaces are auto-created.** Helmfile passes `--create-namespace` to Helm by default
> (Helm ≥ 3.2), so you don't need to `kubectl create ns hf-101` first.

---

## Step 3: Resolve dependencies — `helmfile deps`

```sh
helmfile deps
```

```text
Downloading podinfo from repo https://stefanprodan.github.io/podinfo
```

This downloads the remote chart(s) into the local cache so the next commands work offline. Run
it again any time you bump a `version:`.

---

## Step 4: Render locally — `helmfile template`

Before touching the cluster, render every release to plain Kubernetes YAML:

```sh
helmfile template | less        # the full manifests for ALL releases
helmfile template | grep '^kind:' | sort | uniq -c
```

```text
   2 kind: Deployment
   2 kind: Service
   ... (podinfo + web objects, ~9 documents total)
```

`template` touches **nothing** in the cluster — it's your fastest debug loop for "what will this
actually produce?"

---

## Step 5: Preview the change — `helmfile diff`

```sh
helmfile diff
```

On a clean cluster every resource shows up green (to be created). `diff` is your **plan** step —
the helmfile equivalent of `terraform plan`. (It uses the `helm-diff` plugin, installed in Prerequisites.)

---

## Step 6: Deploy everything — `helmfile sync`

```sh
helmfile sync
```

```text
UPDATED RELEASES:
NAME      NAMESPACE   CHART             VERSION   DURATION
podinfo   hf-101      podinfo/podinfo   6.7.1     0s
web       hf-101      ./web             0.1.0     0s
```

One command installed **both** releases. Confirm:

```sh
helmfile list                          # both releases, INSTALLED=true
kubectl get pods,svc -n hf-101         # nginx + podinfo running
```

```text
NAME                          READY   STATUS    RESTARTS   AGE
pod/podinfo-...               1/1     Running   0          30s
pod/web-...                   1/1     Running   0          30s
```

> **`helmfile list` vs `kubectl get`:** `helmfile list` shows the *releases* you manage (their
> names, namespaces, chart versions, labels); `kubectl get` shows the *objects* Helm created.

Reach podinfo from your host (kind NodePort isn't routable on macOS):

```sh
kubectl port-forward -n hf-101 svc/podinfo 9898:9898 &
curl -s localhost:9898 | head -5      # podinfo JSON greeting
kill %1
```

---

## Step 7: Confirm convergence — `diff` again

```sh
helmfile diff
```

This time `diff` reports **no changes** — desired state matches the cluster. That idempotency is
the whole point: re-running is safe.

> **`sync` vs `apply`.** `helmfile sync` force-runs `helm upgrade` on *every* release each time.
> `helmfile apply` runs `diff` first and upgrades **only** the releases that actually changed —
> the safer day-to-day command (you'll use it in the next labs).

---

## Step 8: Tear down — `helmfile destroy`

```sh
helmfile destroy                       # helm uninstall for every release
kubectl delete ns hf-101 --ignore-not-found
# helmfile добавляет repo podinfo в ГЛОБАЛЬНЫЙ конфиг Helm (и обновляет все repo пользователя).
# Уберите его после лабы:
helm repo remove podinfo
# Либо изолируйте конфиг Helm на время лабы (проверь: переменная HELM_CONFIG_HOME в вашей версии Helm):
# export HELM_CONFIG_HOME=$HOME/.config/helm-lab
```

---

## Command Reference

| Command | Purpose |
|---------|---------|
| `helmfile deps` | Download/refresh remote charts into the cache |
| `helmfile template` | Render all releases to YAML — no cluster touched |
| `helmfile diff` | Preview what `sync`/`apply` would change (the "plan") |
| `helmfile sync` | Install/upgrade **all** releases (force `helm upgrade`) |
| `helmfile apply` | `diff` + deploy **only changed** releases (safer default) |
| `helmfile list` | List managed releases (names, ns, versions, labels) |
| `helmfile status` | Show Helm status of current releases |
| `helmfile destroy` | `helm uninstall` every release |

## What You Learned

- A `helmfile.yaml` describes **many releases as one declarative file** — Helmfile calls Helm
- Mix **local** (`chart: ./path`) and **remote** (`chart: repo/name` + `version:`) charts freely
- The everyday loop: **`deps → template → diff → sync`** (and `apply` for safe re-runs)
- `helmfile diff` is your plan step; `sync`/`apply` are idempotent — re-running is safe
- One command deploys (and one destroys) the whole stack

Next: **[Environments & DRY](helmfile_environments_lab.md)** — deploy this same stack to
*staging* and *prod* from one file, with zero duplication. 🚀
