# LAB: Release Lifecycle, Upgrades & Rollback

Charts are only half of Helm. The other half is **operating releases over time** — upgrading
safely, inspecting what's deployed, and recovering instantly when an upgrade goes wrong. This
lab drills the day-2 commands you'll run constantly in production and CI.

> **Verified environment:** Helm v4.2.0, kind v0.31 (Kubernetes v1.35). Works on Helm 3 too,
> except `--rollback-on-failure` (Helm 4) is spelled `--atomic` on Helm 3 — see Step 5.

A Helm **release** is a named, **versioned** install. Every `install`, `upgrade`, and
`rollback` creates a new immutable **revision**; Helm keeps the history so you can see what
changed and jump back to any point.

```text
 rev1        rev2          rev3          rev4              rev5         rev6
install ──► upgrade  ──►  upgrade  ──►  rollback 1  ──►  upgrade✗ ──► rollback 4 (auto)
(r=1)       (r=3)         (img 1.26)    (back to r=1)    (bad img)    (recovered)
        each arrow = a new revision; nothing is ever edited in place
```

---

## Setup: a throwaway chart to operate on

Any chart works. Scaffold a minimal one:

```sh
mkdir -p ~/helm-lifecycle && cd ~/helm-lifecycle
helm create web
```

We'll drive its `replicaCount` and `image.tag` to simulate real change.

---

## Step 1: Install — revision 1

```sh
helm install site ./web --set image.tag=1.27-alpine
helm list                      # STATUS deployed, REVISION 1
helm status site               # full status + the rendered NOTES
```

> **`helm list` vs `kubectl get`:** `helm list` shows *releases* (logical apps with history);
> `kubectl get` shows the *objects* they created. You manage the release; Helm manages the
> objects.

---

## Step 2: Upgrade — revisions 2 and 3

Each `helm upgrade` records a new revision. Make two changes:

```sh
# rev2 — scale up
helm upgrade site ./web --set image.tag=1.27-alpine --set replicaCount=3

# rev3 — change the image
helm upgrade site ./web --set image.tag=1.26-alpine --set replicaCount=3
```

> **Mind `--set` on upgrade.** This applies to Helm 3 and Helm 4 alike: values you set in a previous
> upgrade are **not** remembered. The upgrade starts from the chart defaults plus the `--set`/`-f`
> you pass *this time*, unless you add `--reuse-values`. That's why both flags are repeated above.
> The no-flags case is the nuance: when you pass neither `--set` nor `-f`, whether earlier values are
> reused depends on the version. проверь: `helm upgrade --help` and the Helm release notes for your version.
> Prefer a values file (`-f prod.yaml`) so the full desired state is explicit every time.

---

## Step 3: Inspect history and any past revision

```sh
helm history site
```

```text
REVISION  STATUS      CHART      DESCRIPTION
1         superseded  web-0.1.0  Install complete
2         superseded  web-0.1.0  Upgrade complete
3         deployed    web-0.1.0  Upgrade complete
```

Helm stores the **full inputs and output** of every revision — inspect them without redeploying:

```sh
helm get values   site --revision 2      # what --set/-f produced rev 2
helm get manifest site --revision 1      # the exact YAML rev 1 applied
helm get notes    site                   # the rendered NOTES.txt
helm get all      site                   # everything about the current release
```

This is invaluable for "what actually changed between rev 2 and rev 3?" investigations.

---

## Step 4: Roll back

Something looks wrong on rev 3? Jump back to rev 1 — instantly, with no chart or values in hand:

```sh
helm rollback site 1
helm history site            # a NEW rev 4 appears: "Rollback to 1"
```

A rollback is itself a **forward** revision (rev 4), so it's auditable and itself reversible.
Confirm the cluster matches rev 1 again:

```sh
kubectl get deploy site-web \
  -o jsonpath='image={.spec.template.spec.containers[0].image} replicas={.spec.replicas}{"\n"}'
# → image=nginx:1.27-alpine replicas=1
```

---

## Step 5: Safe upgrades — auto-rollback on failure

In production you don't want a broken upgrade to leave the release half-applied. Helm can wait
for resources to become healthy and **automatically roll back** if they don't.

Trigger a deliberately broken upgrade (an image tag that can't be pulled):

```sh
helm upgrade site ./web \
  --set image.tag=this-tag-does-not-exist \
  --rollback-on-failure --timeout=40s
```

> **Helm 3:** use `--atomic` instead. In Helm 4 `--atomic` still works but is **deprecated** in
> favor of `--rollback-on-failure`. Both imply `--wait` (Helm blocks until resources are ready
> or the timeout hits).

Helm waits, sees the new pods never become ready, and reverts:

```text
Error: UPGRADE FAILED: release site failed, and has been rolled back due to
rollback-on-failure being set: ... not ready ... context deadline exceeded
```

```sh
helm history site
```

```text
4   superseded  ...  Rollback to 1
5   failed      ...  Upgrade "site" failed: ... not ready ...
6   deployed    ...  Rollback to 4          ← Helm auto-recovered
```

The release is healthy on a good revision, and the **running image was never broken**:

```sh
kubectl get deploy site-web -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}'
# → nginx:1.27-alpine   (the good image; the bad one never took over)
kubectl get pods -l app.kubernetes.io/instance=site
# → 1/1 Running
```

> **This is the single most important upgrade flag for CI/CD.** Always deploy with
> `--rollback-on-failure --timeout=<budget>` (or `--atomic` on Helm 3) so a bad release reverts
> itself instead of paging you at 3am.

---

## Step 6 (optional): preview changes with `helm diff`

> The helm-diff plugin is optional: the rest of the lab works without it. проверь: `helm plugin install` flags and plugin behaviour on Helm 4 (helm-diff README and Helm release notes).

The community **helm-diff** plugin shows a `kubectl diff`-style preview of what an upgrade
*would* change — before you run it. It pulls and runs third-party code, so install it only if
you trust the source and your environment allows plugins:

```sh
helm plugin install https://github.com/databus23/helm-diff --verify=false
helm diff upgrade site ./web --set image.tag=1.27-alpine --set replicaCount=2     # red/green diff, applies nothing
```

If plugins aren't permitted, you can approximate it with built-ins. Pass the same `--set` flags as the
real upgrade, otherwise the chart default image (from `appVersion`) shows up as a false change:

```sh
diff <(helm get manifest site) <(helm template site ./web --set image.tag=1.27-alpine --set replicaCount=2)
```

---

## Cleanup

```sh
helm uninstall site          # removes all revisions and objects
```

> `helm uninstall --keep-history site` keeps the revision history (status `uninstalled`) so you
> can `helm rollback` it back to life later.

---

## Command Reference

| Command | Purpose |
|---------|---------|
| `helm install <name> <chart>` | Create release (rev 1) |
| `helm upgrade <name> <chart>` | New revision with changes |
| `helm upgrade --install` | Install if absent, else upgrade (CI idiom) |
| `helm upgrade --rollback-on-failure --timeout=Ns` | Wait for health; auto-revert on failure (Helm 4; `--atomic` on Helm 3) |
| `helm upgrade --reuse-values` | Keep previous values, apply only new `--set` |
| `helm history <name>` | List all revisions |
| `helm rollback <name> <rev>` | Revert to a revision (as a new forward revision) |
| `helm status <name>` | Current status + NOTES |
| `helm get values/manifest/notes/all <name> [--revision N]` | Inspect a release or past revision |
| `helm uninstall <name> [--keep-history]` | Delete the release |

## What You Learned

- Releases are **versioned**; every change is an immutable, auditable revision
- Upgrades don't remember prior `--set` by default — make desired state explicit (`-f`)
- Inspect any past revision's values and manifest without redeploying
- `helm rollback` recovers instantly and is itself a forward, reversible revision
- `--rollback-on-failure` / `--atomic` makes upgrades self-healing — the must-have CI flag
- Preview changes with `helm diff` (plugin) or a `helm get manifest` vs `helm template` diff

You can now operate Helm releases over their full lifecycle — and recover from a bad deploy in
one command. ♻️
