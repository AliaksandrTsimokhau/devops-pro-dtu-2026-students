# LAB: Ordering, Health & Hooks

Some releases must come up **in order**: the database before the app that connects to it, the
CRDs before the operator that uses them. And you want a broken dependency to **stop the rollout**,
not leave a half-deployed stack. Plain `helm` has no concept of "release B after release A" —
**this is one of the biggest reasons teams adopt Helmfile.**

This lab wires three Helmfile features together:

- **`needs:`** — declare that one release deploys only after another succeeds (a DAG)
- **`helmDefaults`** — global health gates (`wait`, `timeout`, `cleanupOnFail`) for every release
- **`hooks:`** — run commands around a release's lifecycle (`presync`, `postsync`, …)

Everything is **verified on a local `kind` cluster**.

> **Verified environment:** Helmfile **v1.5.2**, Helm **v4.2.0**, kind v0.31, context
> `kind-<your-cluster>`.

```text
  helmfile sync                                  needs: hf-order/db
  ┌──────────────────────────────────────────────────────────────┐
  │  presync hook ─► deploy db ─► WAIT until ready ─► postsync hook │
  │                                                       │         │
  │                            (db healthy?)  ── yes ──►  deploy app │
  │                                           ── no  ──►  STOP. app  │
  │                                                       never runs │
  └──────────────────────────────────────────────────────────────┘
```

**Prereq:** Labs 1–2.

---

## Step 1: Project layout

```sh
mkdir -p ~/helmfile-order && cd ~/helmfile-order
helm create web                       # plays the role of the "app"
```

We'll use the remote `podinfo` chart as a stand-in "database" (any chart that becomes healthy
works) and the local `web` chart as the dependent "app".

---

## Step 2: The helmfile — `helmfile.yaml.gotmpl`

```yaml
repositories:
  - name: podinfo
    url: https://stefanprodan.github.io/podinfo

# Global defaults applied to EVERY release
helmDefaults:
  wait: true            # block until resources report Ready
  timeout: 120          # seconds to wait before declaring failure
  cleanupOnFail: true   # delete resources a failed release created
  createNamespace: true

releases:
  - name: db
    chart: podinfo/podinfo
    version: 6.7.1
    namespace: hf-order
    hooks:
      - events: ["presync"]
        showlogs: true
        command: "sh"
        args: ["-c", "echo '>>> HOOK presync: deploying db first'"]
      - events: ["postsync"]
        showlogs: true
        command: "sh"
        args: ["-c", "echo '>>> HOOK postsync: db is ready'"]

  - name: app
    chart: ./web
    namespace: hf-order
    needs:
      - hf-order/db        # app deploys ONLY after db succeeds
```

Key fields:

- **`helmDefaults.wait: true`** — Helmfile (via `helm --wait`) blocks until each release's pods
  report Ready. Without it, `sync` returns the instant the manifests are *applied*, not healthy.
- **`needs: [hf-order/db]`** — the dependency edge. Format is **`<namespace>/<name>`** (use just
  `<name>` if the release has no namespace). Helmfile builds a DAG and orders the rollout.
- **`hooks:`** — `command` + `args` run locally on the machine doing the sync; `showlogs: true`
  surfaces their output. `presync`/`postsync` fire before/after that release's `helm upgrade`.

---

## Step 3: Sync and watch the order

```sh
helmfile deps
helmfile sync
```

```text
hook[presync] logs | >>> HOOK presync: deploying db first
Upgrading release=db, chart=podinfo/podinfo, namespace=hf-order
db   ... deployed
hook[postsync] logs | >>> HOOK postsync: db is ready
Upgrading release=app, chart=web, namespace=hf-order
app  ... deployed

UPDATED RELEASES:
NAME   NAMESPACE   CHART             VERSION   DURATION
db     hf-order    podinfo/podinfo   6.7.1     2s
app    hf-order    ./web             0.1.0     1s
```

Read the order top-to-bottom: **presync hook → db deployed (and waited healthy) → postsync hook
→ app deployed**. `app` never started until `db` was Ready, because `wait: true` + `needs`
together gate it.

```sh
kubectl get deploy -n hf-order        # db + app, both Available
```

---

## Step 4: Prove the safety net — a broken dependency stops the rollout

This is the payoff. Break `db`'s image so it can never become healthy, and watch Helmfile
**refuse to deploy `app`**. Use a short timeout so you don't wait long:

```yaml
# helmfile-fail.yaml.gotmpl — same as above, but:
helmDefaults:
  wait: true
  timeout: 25
  cleanupOnFail: true
  createNamespace: true
releases:
  - name: db
    chart: podinfo/podinfo
    version: 6.7.1
    namespace: hf-order
    values:
      - image:
          tag: this-tag-does-not-exist   # will never pull → never Ready
  - name: app
    chart: ./web
    namespace: hf-order
    needs:
      - hf-order/db
```

```sh
helmfile -f helmfile-fail.yaml.gotmpl sync
```

```text
Upgrading release=db, chart=podinfo/podinfo, namespace=hf-order
FAILED RELEASES:
  ... resource Deployment/hf-order/db-podinfo not ready ... context deadline exceeded
```

Notice what's **missing**: there is no `Upgrading release=app` line. Because `db` failed its
health check, `needs` short-circuited the DAG and **`app` was never attempted**. On a fresh
cluster `app` simply wouldn't exist. `cleanupOnFail` also rolled back the resources `db`'s failed
upgrade created.

> **Why this matters:** a bad database migration or image typo halts the deploy at the dependency
> instead of bringing up an app that points at a broken backend. The pipeline goes red; the
> cluster doesn't go half-broken.

---

## Step 5: Clean up

```sh
helmfile destroy
kubectl delete ns hf-order --ignore-not-found
```

---

## Notes & gotchas

- **`needs` is honored by `sync` and `apply`.** It orders the DAG; releases with no edge between
  them still deploy in parallel.
- **`needs` format:** `<namespace>/<name>` when the release sets a namespace, else `<name>`.
  A typo here = `release "…" depends on nonexistent release` at parse time.
- **`wait` is what makes `needs` meaningful.** Without `wait: true`, "db succeeded" means "manifest
  applied," not "pods Ready" — so `app` could start against a not-yet-ready db.
- **Hook `command` runs on the sync host/runner**, not in the cluster. Common real uses:
  `kubectl apply` a CRD, run a DB migration job, post a Slack message, patch a resource.
- **Hook events:** `prepare`, `presync`, `postsync`, `cleanup` (and `postuninstall`). `presync`/
  `postsync` are the everyday pair.

## Command / field Reference

| Field | Purpose |
|-------|---------|
| `helmDefaults.wait: true` | Block until resources are Ready (enables `needs` to mean "healthy") |
| `helmDefaults.timeout: N` | Seconds before a release is declared failed |
| `helmDefaults.cleanupOnFail: true` | Delete resources a failed upgrade created |
| `helmDefaults.createNamespace` | Auto-create target namespaces (default true) |
| `releases[].needs: [ns/name]` | Deploy this release only after the named release succeeds |
| `releases[].hooks[].events` | When the hook fires: `presync`, `postsync`, `cleanup`, … |
| `releases[].hooks[].command/args` | Command (on the runner) to execute for the hook |
| `hooks[].showlogs: true` | Surface the hook command's output in `sync` logs |

## What You Learned

- `needs:` turns a flat release list into an **ordered DAG** — db before app, CRDs before operator
- `helmDefaults.wait` makes "succeeded" mean **healthy**, not just "applied"
- A failed dependency + `needs` **stops the rollout** instead of half-deploying the stack
- `cleanupOnFail` reverts the mess a failed upgrade leaves behind
- `hooks:` run commands around a release (migrations, CRDs, notifications) with `showlogs`

Next: **[Remote Charts & Values](helmfile_remote_lab.md)** — pull charts and values straight
from git and an OCI registry, pinned by version. 🌐
