# LAB: Remote Charts & Values from git / OCI

So far every chart was either local (`./web`) or from a classic Helm HTTP repo. In a real org the
charts and the *values* often live somewhere central — a platform team's git repo, an OCI
registry shared with your container images. Vendoring copies into every app repo causes drift and
makes upgrades a manual chore.

**Helmfile can point a release straight at a remote source — git, OCI, or plain HTTP — and pin it
to a version.** This lab pulls a chart three ways and merges a values file fetched over the
network. Everything is **verified on a local `kind` cluster**.

> **Verified environment:** Helmfile **v1.5.2**, Helm **v4.2.0**, kind v0.31, context
> `kind-<your-cluster>`. Uses public sources (GitHub, GHCR) — no credentials needed.

```text
   releases[].chart:                          releases[].values:
   ├─ git::https://…@charts/podinfo?ref=6.7.1   ├─ http(s)://…/values-prod.yaml
   ├─ oci://ghcr.io/…/podinfo  (version: 6.7.1) ├─ git::https://…@envs/prod.yaml?ref=…
   └─ repo/chart  (classic HTTP repo)           └─ ./local-overrides.yaml
            │                                              │
            └──────────── helmfile sync ───────────────────┘ → kind
```

**Prereq:** Labs 1–3.

---

## Where remote URLs are accepted

Helmfile uses **hashicorp/go-getter** (the same library as Terraform). Remote URLs work in four
places — most importantly `chart:` and `values:`:

| Field | What you can pull remotely |
|-------|----------------------------|
| `releases[].chart` | A chart from git / OCI / HTTP |
| `releases[].values` | A values file from git / HTTP |
| `helmfiles[].path` | A whole sub-helmfile hosted elsewhere |
| `bases[]` | Shared config layered in from a remote source |

go-getter URL shape: `<protocol>::<url>@<subpath>?<query>` — e.g. `git::`, `@charts/podinfo`
(sub-directory), `?ref=v1.2.3` (pin a tag/branch/SHA).

---

## Step 1: A chart from a git repository

`git::` clones a repo and uses a chart inside it. Create `helmfile.yaml.gotmpl`:

```yaml
releases:
  - name: podinfo-git
    chart: git::https://github.com/stefanprodan/podinfo.git@charts/podinfo?ref=6.7.1
    namespace: hf-remote
```

- `@charts/podinfo` — the chart lives in `charts/podinfo/` **inside** the repo (note the `@`, not
  `/` — getting this wrong = "clone succeeded but no chart found").
- `?ref=6.7.1` — **pinned** to a tag. Without `?ref=`, you track the branch HEAD — one bad merge
  upstream silently breaks every consumer.

Render it (Helmfile clones into its cache, then templates):

```sh
helmfile template | grep -E 'kind:|app.kubernetes.io/version'
# → Service / Deployment ... version "6.7.1"
```

No `helm repo add`, no vendored copy — the chart is fetched on demand and version-pinned in git.

---

## Step 2: A chart from an OCI registry

OCI is the modern way to distribute charts (Helm 3.8+) — same registry as your Docker images.
The public **GHCR** copy of podinfo needs no auth:

```yaml
releases:
  - name: podinfo-oci
    chart: oci://ghcr.io/stefanprodan/charts/podinfo
    version: 6.7.1
    namespace: hf-remote
```

- `chart:` is the OCI **repository**; the chart name is the last path segment.
- **`version:`** works exactly like a classic Helm repo — it's the OCI tag.

Deploy it to kind for real:

```sh
helmfile sync
kubectl get pods -n hf-remote          # podinfo-oci-... Running
helmfile destroy
```

```text
Upgrading release=podinfo-oci, chart=.../helmfile/oci__ghcr_io/.../podinfo/6.7.1/podinfo
Release "podinfo-oci" does not exist. Installing it now.
NAME: podinfo-oci
```

Works with GHCR, GitLab Registry, ECR, Harbor, Artifactory — anything OCI.

---

## Step 3: Values fetched from a remote source

Not just charts — **values files** can be remote too. The classic pattern: a platform team owns
versioned values in one repo; app teams reference them by tag.

```yaml
releases:
  - name: podinfo-oci
    chart: oci://ghcr.io/stefanprodan/charts/podinfo
    version: 6.7.1
    namespace: hf-remote
    values:
      # remote first (base), local last (wins) — Helmfile merges in order
      - https://config.example.com/podinfo/values-prod.yaml
      - git::https://github.com/myorg/platform-config.git@podinfo/prod.yaml?ref=v2025.06
      - ./local-overrides.yaml
```

Helmfile downloads each remote file and merges it like any local values file — **last one wins**.

> **Verified locally** by serving a values file over HTTP (`python3 -m http.server`) and pointing
> a release's `values:` at `http://localhost:5566/values-prod.yaml`. The override took effect:
> `replicaCount: 4` and a custom `PODINFO_UI_MESSAGE` appeared in the rendered manifest. Any
> reachable `http(s)://` or `git::` URL works the same way.

---

## Step 4: Authentication for private sources

Public sources need nothing. For private repos/registries, **never commit a token** — inject it:

```yaml
# HTTPS + token (git credential or inline env-var expansion)
chart: git::https://oauth2:${GIT_TOKEN}@github.com/myorg/repo.git@chart?ref=v1.0

# SSH (relies on ssh-agent, or pass a key)
chart: git::ssh://git@github.com/myorg/repo.git@chart?ref=main
```

```sh
# OCI — log in once; Helm/Helmfile reuse the stored credential
helm registry login ghcr.io -u "$USER" -p "$TOKEN"
```

In CI use the platform's token (`CI_JOB_TOKEN`, `secrets.GITHUB_TOKEN`) via `${...}` expansion.

---

## Step 5 (optional): a self-hosted insecure OCI registry

If you run a **plaintext** OCI registry for local testing (e.g. a throwaway `registry:2`), Helm
needs `--plain-http`. Stand one up with **podman** (the runtime behind `kind` on this setup):

```sh
podman run -d -p 5111:5000 --name hfreg docker.io/library/registry:2

helm package ./web
helm push web-0.1.0.tgz oci://localhost:5111/charts --plain-http
```

Reference it from Helmfile, passing `--plain-http` through to Helm:

```yaml
helmDefaults:
  args:
    - "--plain-http"        # ONLY for local/insecure registries — never in prod
releases:
  - name: web
    chart: oci://localhost:5111/charts/web
    version: 0.1.0
    namespace: hf-remote
```

```sh
podman rm -f hfreg          # cleanup
```

> `--plain-http` disables TLS — strictly for local/insecure registries. Real registries (GHCR,
> GitLab, ECR) are HTTPS; drop the flag.

---

## Caveats & tips

- **Always pin `?ref=` (git) / `version:` (OCI).** Unpinned = track HEAD/latest = "works today,
  broken tomorrow." This is the #1 remote-chart mistake.
- **Caching:** go-getter caches under `~/Library/Caches/helmfile/` (Linux: `~/.cache/helmfile/`).
  Clear it if you suspect stale content; in CI, prefer a clean cache.
- **`@subpath` uses `@`, not `/`.** Missing it = clone succeeds, chart not found.
- **`helmfile fetch`** (v1.5+) pre-downloads remote artefacts — useful for air-gapped deploys.
- **`helmfile diff`** shows exactly what changes when you bump a `?ref=` or `version:` — diff
  before you merge the bump.

## What You Learned

- Point `chart:` at **git** (`git::…@subpath?ref=`), **OCI** (`oci://…` + `version:`), or HTTP —
  no vendoring, no `helm repo add`
- Pull **values files** remotely too; Helmfile merges them in order (last wins)
- **Pin everything** — `?ref=` for git, `version:` for OCI — or you're tracking a moving target
- Authenticate private sources via env-var expansion / `helm registry login` — never commit tokens
- `--plain-http` (via `helmDefaults.args`) is the escape hatch for local insecure OCI registries

You can now consume charts and config from anywhere your org keeps them — version-pinned and
reproducible. 🌐
