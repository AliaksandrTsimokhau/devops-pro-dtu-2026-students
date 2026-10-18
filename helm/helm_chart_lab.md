# LAB: Create Your First Helm Chart

> Cluster: the name is the one you chose in module 02 Lab 01. Below, `$CLUSTER` is derived from the current kubectl context.

In this lab you create, deploy, customize, package, and share your first Helm chart, then add a dependency — all verified against a local **kind** cluster with **Helm 3/4**.

> **Verified environment:** Helm v4.2.0, kind v0.31 (Kubernetes v1.35), 1 control-plane + 3 workers.
> Everything below also works on Helm 3.

---

## Overview

A typical cloud-native application uses a **3-tier architecture**:

- **Frontend** (web UI) — the presentation tier
- **Backend** (API server) — the application tier (business logic)
- **Database** (MySQL, PostgreSQL, MariaDB) — the data tier

This split promotes scalability, flexibility, and maintainability — key traits of cloud-native apps.

Here is how the tiers talk to each other in Kubernetes, packaged by Helm:

```text
        ┌──────────────────────── Helm release ────────────────────────┐
        │                                                               │
  user  │   ┌──────────┐      ┌──────────┐      ┌──────────────┐        │
 ─────► │   │ Frontend │ ───► │ Backend  │ ───► │   Database   │        │
        │   │  (web UI)│      │  (API)   │      │  (MariaDB)   │        │
        │   └──────────┘      └──────────┘      └──────────────┘        │
        │     Deployment        Deployment        StatefulSet           │
        │     + Service         + Service         + Service + PVC       │
        └───────────────────────────────────────────────────────────────┘
            presentation        application            data tier
```

Helm packages these Kubernetes objects (Deployments, Services, ConfigMaps, Secrets, PVCs)
into one **chart**, so you can deploy, configure, and manage the whole stack as a single unit.

---

## Use Cases for Helm

- Find and reuse popular software packaged as Kubernetes charts
- Share your own applications as charts
- Create reproducible deployments of your Kubernetes applications
- Manage many Kubernetes objects as one templated unit
- Version, roll out, and roll back releases

---

## Prerequisites

```sh
helm version      # expect v3.x or v4.x
CLUSTER=$(kubectl config current-context | sed 's/^kind-//')
kubectl config current-context   # expect: kind-$CLUSTER
kubectl get nodes # all nodes Ready
```

> **Helm install:** see the [Helm install guide](https://helm.sh/docs/intro/install/).
> **Heads-up for Helm 2 veterans:** there is no Tiller, no `helm serve`, and no
> `helm search local` anymore — those steps below are updated for Helm 3/4.

---

# Part 1 — The Easiest Approach

## Reuse Existing Kubernetes YAML

If you already have working `deployment.yaml` and `service.yaml`, you can turn them into a
chart by copying them into the chart's `templates/` directory — no templating required. Helm
will apply them verbatim.

This works, but the real power of Helm comes from **template variables**, which let one chart
produce different manifests per environment.

---

## Customizing with Template Variables

A real `helm create` deployment template looks like this (Helm 3/4 syntax):

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "mychart.fullname" . }}
  labels:
    {{- include "mychart.labels" . | nindent 4 }}
spec:
  replicas: {{ .Values.replicaCount }}
  selector:
    matchLabels:
      {{- include "mychart.selectorLabels" . | nindent 6 }}
```

### Three kinds of values

| Expression | Source |
|------------|--------|
| `{{ include "mychart.fullname" . }}` | Helper template from `_helpers.tpl` |
| `{{ .Release.Name }}` | Built-in object, set by Helm at install time |
| `{{ .Values.replicaCount }}` | User config from `values.yaml` (override with `--set`) |

> **Note:** modern Helm uses `include "..."` (which supports piping to `nindent`), not the
> old Helm 2 `template "..."` action. Helper names are namespaced by chart, e.g.
> `mychart.fullname`.

For more, see the [Helm Template Guide](https://helm.sh/docs/chart_template_guide/).

---

# Part 2 — Build It Step by Step

## Step 1: Generate Your First Chart

```sh
helm create mychart
```

This scaffolds a working NGINX chart:

```text
mychart/
├── Chart.yaml              # chart metadata + version
├── values.yaml             # default configuration
├── .helmignore
├── charts/                 # subcharts (empty for now)
└── templates/
    ├── _helpers.tpl        # reusable template partials
    ├── deployment.yaml
    ├── service.yaml
    ├── serviceaccount.yaml
    ├── ingress.yaml
    ├── httproute.yaml      # Gateway API route (newer scaffolds)
    ├── hpa.yaml            # HorizontalPodAutoscaler
    ├── NOTES.txt           # printed after install
    └── tests/
        └── test-connection.yaml
```

> `helm create` also creates an empty `charts/` directory; `helm dependency update` downloads subcharts into it.

### Inspect the rendered manifests

`--dry-run` renders templates **without** touching the cluster. Use `--dry-run=client`
(`--dry-run` alone is deprecated):

```sh
helm install example ./mychart --dry-run=client --debug
```

### See how `.Values` change the output

`service.port` is defined in `values.yaml`. Override it and watch the rendered Service change:

```sh
helm install example ./mychart --dry-run=client --debug --set service.port=8080
```

> The default scaffold uses `service.port` (and a **named** target port `http`), not the old
> Helm 2 `service.externalPort` / `service.internalPort` keys.

### Key files

- **`values.yaml`** — defaults you override with `--set` or `-f myvalues.yaml`
- **`_helpers.tpl`** — partials reused across templates (`mychart.fullname`, `mychart.labels`, …)
- **`NOTES.txt`** — usage instructions printed after a successful install
- **`Chart.yaml`** — name, version, appVersion, and dependencies

---

## Step 2: Deploy Your First Chart

The default service type is `ClusterIP`. Install the chart (NGINX) and wait for it:

```sh
helm install example ./mychart
kubectl rollout status deploy/example-mychart --timeout=90s
kubectl get pods,svc -l app.kubernetes.io/instance=example
```

### Access it (this is where kind differs from cloud)

`helm install` prints `NOTES.txt`. The default `ClusterIP` NOTES tell you to **port-forward** —
that is the reliable way to reach the app on kind:

```sh
kubectl port-forward svc/example-mychart 8080:80
# open http://127.0.0.1:8080
```

> **Why not NodePort on kind?**
> `helm install example ./mychart --set service.type=NodePort` works, and the NOTES print a
> `http://$NODE_IP:$NODE_PORT` URL — **but that URL is not reachable from your Mac**. The node
> IP (e.g. `10.89.0.9`) lives on the container runtime's internal network. To expose a NodePort
> on kind you must map it at cluster-creation time with `extraPortMappings`. For this lab,
> **`kubectl port-forward` works regardless of service type** — prefer it.

---

## Step 3: Deploy a Custom Application

Swap NGINX for a real app — the classic Helm tutorial todo app. Edit `values.yaml`:

```yaml
image:
  repository: prydonius/todo
  tag: "1.0.0"
  pullPolicy: IfNotPresent
```

Validate, then roll out the change to the existing release with `upgrade --install`
(it installs if the release is new, upgrades if it already exists — the idiom you'll use in CI):

```sh
helm lint ./mychart
helm upgrade --install example ./mychart
kubectl rollout status deploy/example-mychart --timeout=180s
```

View it:

```sh
kubectl port-forward svc/example-mychart 8080:80
# open http://127.0.0.1:8080  → the todo app UI
```

> **Expect a slow first pull (~1–2 min).** `prydonius/todo:1.0.0` is an old image; the
> deployment will sit in `ContainerCreating` while it downloads, then become `Running`.
> The app serves on port 80, so no port change is needed. (You may see a harmless
> `io_setup() failed` notice in the logs — it still returns HTTP 200.)

> **Tip — override without editing files:** instead of editing `values.yaml`, you can pass
> `--set image.repository=prydonius/todo --set image.tag=1.0.0`.

---

## Step 4: Package and Share Your Chart

> Helm 2's `helm serve` and `helm search local` were **removed** in Helm 3. Use the flow below.

### Package the chart

```sh
helm package ./mychart          # → mychart-0.1.0.tgz
helm install example-pkg ./mychart-0.1.0.tgz   # new release name — `example` is still live from Step 3
```

### Option A — Classic HTTP chart repository

A chart repo is just a directory of `.tgz` files plus an `index.yaml`:

```sh
mkdir -p repo && cp mychart-0.1.0.tgz repo/
helm repo index repo --url http://localhost:8879

# serve it locally (any static server works)
python3 -m http.server 8879 --directory repo &

helm repo add mylocal http://localhost:8879
helm repo update mylocal
helm search repo mylocal              # replaces `helm search local`
helm install fromrepo mylocal/mychart
```

### Option B — OCI registry (the modern standard)

Helm 3.8+ stores charts in any OCI registry (Docker Hub, GHCR, ECR, GCR, Harbor).

> **Optional step.** Push to GHCR needs an account, a namespace you can write to and `helm registry login`.
> Without them, use the local registry alternative below (the push is then not needed).

```sh
# push to an OCI registry (example uses GHCR; log in first: helm registry login ghcr.io)
helm push mychart-0.1.0.tgz oci://ghcr.io/<your-org>/charts

# install directly from the registry — no `helm repo add` needed
helm install example-oci oci://ghcr.io/<your-org>/charts/mychart --version 0.1.0
```

**Local alternative (no account):** run a throwaway OCI registry on localhost and push to it.

```sh
# порт 5000 на macOS занят AirPlay (ControlCenter): используем 5001
podman run -d --rm --name lab-registry -p 5001:5000 docker.io/library/registry:2
helm push mychart-0.1.0.tgz oci://localhost:5001/charts --plain-http    # проверь: флаг --plain-http в вашей версии Helm
helm install example-oci oci://localhost:5001/charts/mychart --version 0.1.0 --plain-http   # проверь: флаг --plain-http
podman stop lab-registry
```

> This is exactly how the MariaDB dependency in Step 5 is pulled
> (`oci://registry-1.docker.io/bitnamicharts`).

---

## Step 5: Add a Dependency (optional)

> **Optional step.** The Bitnami MariaDB dependency depends on an external OCI registry and on
> Bitnami's image-repository changes, so it can fail for reasons outside this lab. If it does,
> use the **fallback** at the end of this step (a local `file://` subchart, the same pattern as the
> [umbrella lab](helm_umbrella_lab.md)).

Add a database subchart. Append to `Chart.yaml`:

```yaml
dependencies:
  - name: mariadb
    version: 21.0.3
    repository: oci://registry-1.docker.io/bitnamicharts
```

Pull the dependency into `charts/`:

```sh
helm dependency update ./mychart      # creates charts/mariadb-21.0.3.tgz + Chart.lock
```

Install the chart **with** its database:

```sh
helm upgrade --install example ./mychart \
  --set mariadb.image.repository=bitnamilegacy/mariadb \
  --set global.security.allowInsecureImages=true \
  --set mariadb.auth.rootPassword=labrootpw \
  --set mariadb.auth.password=labpw
```

Watch both tiers come up (the MariaDB image is large — allow ~1–2 min for the pull):

```sh
kubectl get pods -l app.kubernetes.io/instance=example -w
# example-mychart-...      1/1 Running
# example-mariadb-0        1/1 Running
```

> **⚠ Why the two extra `--set` flags?**
> In 2025 Bitnami stopped publishing free, version-pinned images to `docker.io/bitnami/*`
> and moved them to **`docker.io/bitnamilegacy/*`**. Without the override the MariaDB pod
> fails with `ImagePullBackOff` (`docker.io/bitnami/mariadb:...: not found`).
> - `mariadb.image.repository=bitnamilegacy/mariadb` → pull from the still-public legacy repo.
> - `global.security.allowInsecureImages=true` → the chart now refuses substituted images
>   unless you explicitly acknowledge it.
>
> For production, use Bitnami Secure Images (subscription) or build/host your own image.

> **Fallback: local `file://` subchart (no external registry).** If you already ran
> `helm dependency update` for MariaDB, first remove `mychart/charts/mariadb-*.tgz` and `mychart/Chart.lock`.
> Then:
>
> ```sh
> helm create mychart/charts/cache          # local subchart; its default nginx image comes from its values.yaml
> ```
>
> Replace the `mariadb` entry in `mychart/Chart.yaml` with:
>
> ```yaml
> dependencies:
>   - name: cache
>     version: "0.1.0"                      # проверь: version в mychart/charts/cache/Chart.yaml
>     repository: "file://charts/cache"
> ```
>
> Like the umbrella lab, `file://` subcharts already live in `charts/`, so `helm dependency update` is not needed.
> Install with `helm upgrade --install example ./mychart` (no `--set mariadb.*` flags) and check with
> `kubectl get pods -l app.kubernetes.io/instance=example`.

> **Why set passwords explicitly?** Bitnami charts auto-generate a random DB password on first
> install and store it in a Secret. On any later `helm upgrade` the chart **requires** you to
> pass that same password back (`PASSWORDS ERROR: You must provide your current passwords...`).
> Setting `mariadb.auth.rootPassword` / `mariadb.auth.password` up front makes the release
> reproducible and lets you re-run the command safely. **Never commit real passwords** — in
> production source them from a Secret manager.

---

## Cleanup

```sh
helm uninstall example example-pkg fromrepo example-oci --ignore-not-found
kubectl delete pvc -l app.kubernetes.io/instance=example --ignore-not-found   # MariaDB PVC (if any)
helm repo remove mylocal 2>/dev/null
kill %1 2>/dev/null   # stop the local `python3 -m http.server` from Step 4 (if still running)
```

---

## What You Learned

- Scaffold a chart with `helm create` and read its real structure
- Render templates safely with `--dry-run=client --debug`
- Override configuration via `values.yaml` and `--set`
- Access workloads on kind with `kubectl port-forward` (and why NodePort doesn't "just work")
- Package and share charts via a classic HTTP repo **and** an OCI registry
- Add a subchart dependency — and work around the 2025 Bitnami image change

Congratulations — you created, deployed, packaged, shared, and extended your first Helm chart. 🎉

---

# Part 3 — From Hardcoded YAML to a Templated Chart

> **Goal:** understand *why* Helm templating exists and *how* to use it, by taking a small
> working app and converting it — one parameter at a time — from copy-paste YAML into a single
> reusable chart that powers many environments.

`helm create` (Part 2) hands you a finished chart. Here you build one **by hand** so every
template construct is something you added on purpose. The app is deliberately tiny:

- a **ConfigMap** holding an HTML homepage,
- a **Deployment** (NGINX) that serves that homepage,
- a **Service**,
- a **PVC** for uploads.

Because the homepage lives in the ConfigMap, **every value you template is something you can
see in the browser** — templating stops being abstract.

> **The payoff (what you're building toward):** one chart that renders two *different* live
> sites from the same templates:
>
> ```text
> helm install dev  ./hello-site                          → "Hello from Helm",  env DEV,  1 replica, PVC on
> helm install prod ./hello-site --set site.environment=prod \
>      --set site.title="Production Site" --set replicaCount=2 \
>      --set persistence.enabled=false                    → "Production Site",  env PROD, 2 replicas, PVC off
> ```

Work in a scratch directory:

```sh
mkdir -p ~/helm-templating && cd ~/helm-templating
```

---

## Stage 0 — Plain, hardcoded Kubernetes YAML

No Helm yet. Create `raw/` with three files. This is the kind of YAML most people start with.

`raw/configmap.yaml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: hello-site
data:
  index.html: |
    <!DOCTYPE html>
    <html><body>
      <h1>Hello from Helm</h1>
      <p>Environment: DEV</p>
    </body></html>
```

`raw/deployment.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: hello-site
spec:
  replicas: 1
  selector:
    matchLabels: { app: hello-site }
  template:
    metadata:
      labels: { app: hello-site }
    spec:
      containers:
        - name: nginx
          image: "nginx:1.27-alpine"
          ports:
            - containerPort: 80
          volumeMounts:
            - name: html
              mountPath: /usr/share/nginx/html/index.html
              subPath: index.html
      volumes:
        - name: html
          configMap: { name: hello-site }
```

`raw/service.yaml`:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: hello-site
spec:
  selector: { app: hello-site }
  ports:
    - port: 80
      targetPort: 80
```

Deploy it the plain way and look at it:

```sh
kubectl apply -f raw/
kubectl rollout status deploy/hello-site --timeout=90s
kubectl port-forward svc/hello-site 8080:80   # open http://127.0.0.1:8080
```

**Now feel the pain.** Want a second copy for `prod` with a different title and 2 replicas?
You must **copy all three files** and hand-edit names (or everything collides), labels,
replica count, and the HTML — in multiple places, kept in sync by hand. That doesn't scale.

Clean up before continuing:

```sh
kubectl delete -f raw/
```

---

## Stage 1 — Wrap the YAML in a chart (no templating yet)

The smallest possible chart: the **same** YAML, moved into `templates/`, plus a `Chart.yaml`.

```sh
mkdir -p hello-site/templates
cp raw/*.yaml hello-site/templates/
```

`hello-site/Chart.yaml`:

```yaml
apiVersion: v2
name: hello-site
description: A tiny NGINX site to learn Helm templating
type: application
version: 0.1.0
appVersion: "1.27"
```

```sh
helm template demo ./hello-site     # renders the YAML unchanged
helm install demo ./hello-site
```

Helm already gives you something `kubectl apply` didn't: **release tracking, history, and
one-command rollback/uninstall** (`helm uninstall demo`). But the YAML is still 100% hardcoded.
Next we make it *parametric*.

```sh
helm uninstall demo
```

---

## Stage 2 — Replace hardcoded values with `.Values`

The first templating move: pull the values that change between environments into `values.yaml`
and reference them with `{{ .Values.* }}`.

`hello-site/values.yaml`:

```yaml
replicaCount: 1
image:
  repository: nginx
  tag: "1.27-alpine"
  pullPolicy: IfNotPresent
service:
  type: ClusterIP
  port: 80
```

Edit `templates/deployment.yaml` — swap the literals for value references:

```yaml
spec:
  replicas: {{ .Values.replicaCount }}
  ...
        - name: nginx
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
          imagePullPolicy: {{ .Values.image.pullPolicy }}
```

And `templates/service.yaml`:

```yaml
spec:
  type: {{ .Values.service.type }}
  ports:
    - port: {{ .Values.service.port }}
      targetPort: 80
```

`{{ }}` is a **Go template action**. `.Values` is the parsed `values.yaml`; the leading dot is
"the current scope". Verify a value flows through without deploying:

```sh
helm template demo ./hello-site --set replicaCount=3 | grep -E "replicas:|image:"
# replicas: 3
# image: "nginx:1.27-alpine"
```

One chart, many configurations — but the **resource names are still hardcoded** (`hello-site`),
so two releases would still collide. That's Stage 3.

---

## Stage 3 — Use built-in objects for dynamic names

Helm injects **built-in objects** at render time. Use them so each release gets unique,
predictable names and labels:

| Object | Example value |
|--------|---------------|
| `.Release.Name` | the name you pass to `helm install <name>` |
| `.Release.Namespace` | target namespace |
| `.Chart.Name` / `.Chart.Version` / `.Chart.AppVersion` | from `Chart.yaml` |
| `.Release.Service` | `Helm` |

Replace every hardcoded `hello-site` name. For example, in **all** templates change
`name: hello-site` to:

```yaml
metadata:
  name: {{ .Release.Name }}-{{ .Chart.Name }}
```

and the selector/label `app: hello-site` to `app: {{ .Release.Name }}-{{ .Chart.Name }}`
(keep the Deployment selector, Pod labels, Service selector, and ConfigMap reference matching —
selectors are immutable, so they must agree).

```sh
helm template dev  ./hello-site | grep "name:" | head -3   # dev-hello-site
helm template prod ./hello-site | grep "name:" | head -3   # prod-hello-site
```

Now `dev` and `prod` produce **non-colliding** resources from one chart. But we're repeating
`{{ .Release.Name }}-{{ .Chart.Name }}` and the label block everywhere — Stage 5 fixes that
duplication. First, the visual payoff: templating the homepage.

---

## Stage 4 — Template the ConfigMap content (functions & pipelines)

This is where templating becomes tangible. Add a `site:` block to `values.yaml`:

```yaml
site:
  title: "Hello from Helm"
  environment: "dev"
  message: "Welcome to my templated site!"
  links:
    - name: "Helm docs"
      url: "https://helm.sh/docs/"
    - name: "Kubernetes docs"
      url: "https://kubernetes.io/docs/"
```

Rewrite `templates/configmap.yaml` to build the page from those values:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .Release.Name }}-{{ .Chart.Name }}
data:
  index.html: |
    <!DOCTYPE html>
    <html>
      <head><title>{{ .Values.site.title }}</title></head>
      <body>
        <h1>{{ .Values.site.title }}</h1>
        <p>{{ .Values.site.message }}</p>
        <p>Environment: <strong>{{ .Values.site.environment | upper }}</strong></p>
        <p>Served by release: <code>{{ .Release.Name }}</code> in namespace <code>{{ .Release.Namespace }}</code></p>
      </body>
    </html>
```

Two new ideas:

- **Pipelines (`|`)** send a value through a function: `{{ .Values.site.environment | upper }}`
  renders `DEV`. Pipelines chain left-to-right, like a shell pipe.
- **Functions** — Helm ships [Sprig](https://masterminds.github.io/sprig/): `upper`, `lower`,
  `quote`, `default`, `trunc`, `nindent`, `toYaml`, and ~100 more.

```sh
helm template dev ./hello-site --show-only templates/configmap.yaml | sed -n '/index.html/,/<\/html>/p'
```

`--show-only` renders just one file — invaluable while iterating.

---

## Stage 5 — Eliminate duplication with named templates (`_helpers.tpl`)

`{{ .Release.Name }}-{{ .Chart.Name }}` and the label block are copy-pasted across all three
templates (and the PVC you add in Stage 6 would make four).
DRY them up with **named templates** you `define` once and `include` everywhere.

Create `templates/_helpers.tpl` (files starting with `_` render no manifests — they hold
partials):

```tpl
{{- define "hello-site.fullname" -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "hello-site.labels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end -}}

{{- define "hello-site.selectorLabels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
```

Now every template uses them. The metadata block becomes uniform:

```yaml
metadata:
  name: {{ include "hello-site.fullname" . }}
  labels:
    {{- include "hello-site.labels" . | nindent 4 }}
```

Four crucial details:

- **`include "name" .`** runs the named template; the trailing **`.`** passes the current scope
  so the partial can see `.Release`, `.Chart`, `.Values`. Forgetting it is the #1 beginner bug.
- Prefer **`include`** over the built-in `template` action — only `include` can be piped
  (`| nindent`).
- **`nindent 4`** indents the partial's output by 4 spaces *and* adds a leading newline, so the
  multi-line label block lands at the correct YAML depth.
- **`{{-`** trims preceding whitespace/newline; **`-}}`** trims trailing. This keeps the
  rendered YAML clean — mismatched indentation is the most common templating error.

In the Deployment, the selector and Pod labels use `hello-site.selectorLabels` (the stable
subset). Re-render: the labels are now the standard `app.kubernetes.io/*` set, and every template gets them from one place:

```sh
helm template demo ./hello-site --show-only templates/service.yaml
```

---

## Stage 6 — Control flow: `if`, `with`, `range`, and guards

Real charts make decisions. Add the final values:

```yaml
persistence:
  enabled: true
  size: 100Mi
resources: {}
```

**`if` — make the PVC optional.** Create `templates/pvc.yaml` wrapped in a conditional, so
turning persistence off produces *no* PVC at all:

```yaml
{{- if .Values.persistence.enabled }}
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: {{ include "hello-site.fullname" . }}
  labels:
    {{- include "hello-site.labels" . | nindent 4 }}
spec:
  accessModes: ["ReadWriteOnce"]
  resources:
    requests:
      storage: {{ .Values.persistence.size | quote }}
{{- end }}
```

Mount it in the Deployment **only when enabled**:

```yaml
          volumeMounts:
            - name: html
              mountPath: /usr/share/nginx/html/index.html
              subPath: index.html
            {{- if .Values.persistence.enabled }}
            - name: uploads
              mountPath: /usr/share/nginx/html/uploads
            {{- end }}
      volumes:
        - name: html
          configMap:
            name: {{ include "hello-site.fullname" . }}
        {{- if .Values.persistence.enabled }}
        - name: uploads
          persistentVolumeClaim:
            claimName: {{ include "hello-site.fullname" . }}
        {{- end }}
```

**`range` — loop over a list.** Add a links section to the ConfigMap homepage:

```yaml
        {{- with .Values.site.links }}
        <h2>Links</h2>
        <ul>
        {{- range . }}
          <li><a href="{{ .url }}">{{ .name }}</a></li>
        {{- end }}
        </ul>
        {{- end }}
```

- **`with`** rebinds the scope: inside `{{ with .Values.site.links }}`, the dot `.` *is* the
  links list, and the whole block is skipped if it's empty/absent.
- **`range`** iterates; inside it, `.` is the current item (so `.url`, `.name`).

**`with` + `toYaml` — pass through arbitrary structures.** In the Deployment container:

```yaml
          {{- with .Values.resources }}
          resources:
            {{- toYaml . | nindent 12 }}
          {{- end }}
```

With `resources: {}` this renders nothing; set `--set resources.limits.cpu=100m` and the whole
map is emitted as YAML. `toYaml` serializes any value — no per-field templating needed.

**Guards — `default` and `required`.** Make the image tag fall back to the chart's appVersion,
and the environment mandatory:

```yaml
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag | default .Chart.AppVersion }}"
```

```yaml
        <p>Environment: <strong>{{ required "site.environment is required!" .Values.site.environment | upper }}</strong></p>
```

`required` aborts the render with your message if the value is missing — fail fast at
template time, not at `kubectl apply` time:

```sh
helm template dev ./hello-site --set site.environment= 2>&1 | grep -i error
# Error: ... site.environment is required!
```

**Bonus — auto-roll Pods when config changes.** A classic gotcha: editing a ConfigMap doesn't
restart Pods. Add a checksum annotation to the Pod template so Helm rolls the Deployment
whenever the rendered ConfigMap changes:

```yaml
  template:
    metadata:
      annotations:
        checksum/config: {{ include (print $.Template.BasePath "/configmap.yaml") . | sha256sum }}
```

---

## Stage 7 — Deploy the finished chart: one chart, two sites

Lint, then install two releases from the **same** chart with different values:

```sh
helm lint ./hello-site

helm install dev ./hello-site
helm install prod ./hello-site \
  --set site.environment=prod \
  --set site.title="Production Site" \
  --set replicaCount=2 \
  --set persistence.enabled=false

kubectl get deploy,svc,cm,pvc -l app.kubernetes.io/name=hello-site
```

Expected — note `prod` has **2 replicas and no PVC**, `dev` has 1 replica with a bound PVC:

```text
deployment.apps/dev-hello-site    1/1   ...
deployment.apps/prod-hello-site   2/2   ...
persistentvolumeclaim/dev-hello-site   Bound   ...   100Mi    # only dev
```

See the templated homepages differ, live:

```sh
kubectl port-forward svc/dev-hello-site  8081:80 &
kubectl port-forward svc/prod-hello-site 8082:80 &
curl -s http://127.0.0.1:8081 | grep -E "<h1>|Environment"   # Hello from Helm / DEV
curl -s http://127.0.0.1:8082 | grep -E "<h1>|Environment"   # Production Site / PROD
```

> **This is the whole point of Helm.** Two environments, zero copy-pasted YAML, every
> difference expressed as a value. Add `staging` by writing one more `-f staging-values.yaml`
> — not by forking three manifests.

### Cleanup

```sh
pkill -f "port-forward svc/(dev|prod)-hello-site"   # stop both background port-forwards
helm uninstall dev prod
kubectl delete pvc -l app.kubernetes.io/name=hello-site
```

---

## Templating Cheat-Sheet

| Construct | What it does |
|-----------|--------------|
| `{{ .Values.x }}` | Insert a value from `values.yaml` / `--set` |
| `{{ .Release.Name }}`, `{{ .Chart.Name }}` | Built-in objects injected by Helm |
| `{{ a \| upper }}` | Pipeline — send `a` through a function |
| `{{ x \| default y }}` | Fallback when `x` is empty |
| `{{ required "msg" x }}` | Abort render if `x` is missing |
| `{{ toYaml . \| nindent N }}` | Serialize a structure, indent N spaces |
| `{{- ... -}}` | Trim whitespace before / after the action |
| `{{ include "name" . }}` | Run a named template (pipe-able) |
| `{{ if X }}…{{ end }}` | Conditional block |
| `{{ with X }}…{{ end }}` | Rebind scope (`.` = X); skip if empty |
| `{{ range X }}…{{ end }}` | Loop; `.` = current item |
| `_helpers.tpl` | Holds `define`d partials; renders no manifests |

## What You Learned (Part 3)

- Why hardcoded YAML doesn't scale, and how Helm solves it
- The render pipeline: `.Values`, built-in objects, functions, pipelines
- DRY templates with `define` / `include` and `nindent`
- Control flow with `if`, `with`, `range`, and the `default` / `required` guards
- Whitespace control with `{{-` / `-}}`
- Driving many environments from one chart — the full power of Helm

You started with three rigid YAML files and finished with a single chart that renders an
entire fleet of environments. 🚀
