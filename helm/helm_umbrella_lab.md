# LAB: Umbrella Charts — Compose a 3-Tier App from Subcharts

This lab builds the **3-tier application from the Overview diagram for real**: a `frontend`
(NGINX), a `backend` (whoami API), and a `database` (PostgreSQL) — each its own subchart, wired
together by one **umbrella** chart. You'll learn how Helm composes charts, shares **global**
values, lets a parent **override** child values, and **conditionally** includes a tier.

> **Verified environment:** Helm v4.2.0, kind v0.31 (Kubernetes v1.35). All images are
> multi-arch and pull on Apple Silicon. Everything runs on Helm 3 too.
>
> **Prereq:** finish the templating lab ([helm_chart_lab.md](helm_chart_lab.md), Part 3) — this
> lab assumes you're comfortable with `.Values`, `include`, and `if`/`with`/`range`.

---

## The idea: one release, three coordinated charts

```text
                         umbrella chart: webapp
        ┌───────────────────────────────────────────────────────────┐
        │  values.yaml ──► global.*  (shared by EVERY subchart)       │
        │              └─► frontend.* / backend.* / database.*        │
        │                                                             │
        │   charts/frontend     charts/backend      charts/database   │
        │   ┌──────────┐        ┌──────────┐        ┌──────────────┐  │
        │   │  nginx   │ ─HTTP─► │  whoami  │        │  postgres    │  │
        │   │ Deploy+  │        │ Deploy+  │        │ Deploy+Svc+  │  │
        │   │ Svc+CM   │        │ Svc      │        │ PVC+Secret   │  │
        │   └──────────┘        └──────────┘        └──────────────┘  │
        │     presentation        application        data (optional)  │
        └───────────────────────────────────────────────────────────┘
        helm install shop ./webapp  →  shop-frontend / shop-backend / shop-database
```

A **subchart** is just a chart inside another chart's `charts/` directory. The parent
("umbrella") chart usually has few or no templates of its own — its job is to **declare,
configure, and wire** the subcharts.

---

## Step 1: Create the umbrella skeleton

```sh
mkdir -p ~/helm-umbrella && cd ~/helm-umbrella
mkdir -p webapp/charts/{frontend,backend,database}/templates
mkdir -p webapp/templates
```

`webapp/Chart.yaml` — declares the three subcharts as dependencies. The `condition:` on
`database` lets a value switch the whole tier on or off:

```yaml
apiVersion: v2
name: webapp
description: A 3-tier app (frontend + backend + database) as an umbrella chart
type: application
version: 0.1.0
appVersion: "1.0.0"
dependencies:
  - name: frontend
    version: "0.1.0"
    repository: "file://charts/frontend"
  - name: backend
    version: "0.1.0"
    repository: "file://charts/backend"
  - name: database
    version: "0.1.0"
    repository: "file://charts/database"
    condition: database.enabled
```

> **`file://` dependencies** point at local subchart directories. Because they already live in
> `charts/`, you do **not** need `helm dependency update` — Helm picks them up directly. (You'd
> only run `helm dependency update` to pull *remote* deps, like Bitnami MariaDB.)

`webapp/values.yaml` — the control panel for the whole stack:

```yaml
# Values under `global` flow to ALL subcharts as `.Values.global.*`
global:
  environment: dev
  imageRegistry: ""          # set e.g. "mirror.gcr.io" to pull through a mirror

# Per-subchart overrides — parent wins over each child's own values.yaml
frontend:
  replicaCount: 1

backend:
  replicaCount: 2            # overrides backend's own default of 1

database:
  enabled: true             # consumed by the `condition:` above
  auth:
    password: changeme
  persistence:
    enabled: true
    size: 200Mi
```

`webapp/templates/NOTES.txt` — the umbrella's only template (post-install instructions):

```tpl
webapp ({{ .Values.global.environment }}) deployed as release {{ .Release.Name }}.

Tiers:
  frontend : {{ .Release.Name }}-frontend  (nginx)
  backend  : {{ .Release.Name }}-backend   (whoami API)
{{- if .Values.database.enabled }}
  database : {{ .Release.Name }}-database   (postgres)
{{- end }}

View the frontend:
  kubectl port-forward svc/{{ .Release.Name }}-frontend 8080:80
```

---

## Step 2: A shared `_helpers.tpl` for every subchart

Each subchart gets the **same** helper file. Note `.Chart.Name` inside a subchart resolves to
*that subchart's* name (`frontend`, `backend`, `database`), so `fullname` becomes
`<release>-frontend`, etc. Save this as `charts/<name>/templates/_helpers.tpl` in all three
(swap the `define` prefix to match the subchart name):

```tpl
{{- define "frontend.fullname" -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- define "frontend.labels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/part-of: webapp
environment: {{ .Values.global.environment | default "dev" }}
{{- end -}}
{{- define "frontend.image" -}}
{{- if .Values.global.imageRegistry }}{{ .Values.global.imageRegistry }}/{{ end }}{{ .Values.image.repository }}:{{ .Values.image.tag }}
{{- end -}}
```

Two teaching points baked in:

- **`environment:` reads `.Values.global.environment`** — proving globals reach every subchart.
- **`*.image` honors `global.imageRegistry`** — an empty registry renders just
  `repo:tag`; set it and every image is prefixed. This is how you point a whole stack at a
  mirror or private registry in one line.

> In `backend`/`database`, change the `define` names to `backend.*` / `database.*`.

---

## Step 3: The `frontend` subchart (presentation tier)

`charts/frontend/Chart.yaml`:

```yaml
apiVersion: v2
name: frontend
description: NGINX presentation tier
type: application
version: 0.1.0
appVersion: "1.27"
```

`charts/frontend/values.yaml`:

```yaml
replicaCount: 1
image:
  repository: nginx
  tag: "1.27-alpine"
service:
  port: 80
```

`charts/frontend/templates/configmap.yaml` — a homepage that *displays* the global environment
and the backend's service name, so wiring is visible in the browser:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ include "frontend.fullname" . }}
  labels:
    {{- include "frontend.labels" . | nindent 4 }}
data:
  index.html: |
    <h1>webapp frontend</h1>
    <p>Environment: <strong>{{ .Values.global.environment | default "dev" }}</strong></p>
    <p>Backend API service: <code>{{ .Release.Name }}-backend</code></p>
```

`charts/frontend/templates/deployment.yaml` — note the `BACKEND_URL` env: the frontend learns
the backend's address from the **release name**, the standard way subcharts find each other
(Kubernetes DNS resolves `<release>-backend` inside the cluster):

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "frontend.fullname" . }}
  labels:
    {{- include "frontend.labels" . | nindent 4 }}
spec:
  replicas: {{ .Values.replicaCount }}
  selector:
    matchLabels:
      app.kubernetes.io/name: {{ .Chart.Name }}
      app.kubernetes.io/instance: {{ .Release.Name }}
  template:
    metadata:
      labels:
        app.kubernetes.io/name: {{ .Chart.Name }}
        app.kubernetes.io/instance: {{ .Release.Name }}
    spec:
      containers:
        - name: frontend
          image: "{{ include "frontend.image" . }}"
          ports:
            - { name: http, containerPort: 80 }
          env:
            - name: BACKEND_URL
              value: "http://{{ .Release.Name }}-backend"
          volumeMounts:
            - { name: html, mountPath: /usr/share/nginx/html/index.html, subPath: index.html }
      volumes:
        - name: html
          configMap: { name: {{ include "frontend.fullname" . }} }
```

`charts/frontend/templates/service.yaml`:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: {{ include "frontend.fullname" . }}
  labels:
    {{- include "frontend.labels" . | nindent 4 }}
spec:
  ports:
    - { port: {{ .Values.service.port }}, targetPort: http, name: http }
  selector:
    app.kubernetes.io/name: {{ .Chart.Name }}
    app.kubernetes.io/instance: {{ .Release.Name }}
```

---

## Step 4: The `backend` subchart (application tier)

A [whoami](https://github.com/traefik/whoami) container — it echoes the request, so you can see
the frontend reaching it. `charts/backend/values.yaml`:

```yaml
replicaCount: 1                 # parent overrides this to 2
image:
  repository: traefik/whoami
  tag: "v1.10"
service:
  port: 80
```

`charts/backend/Chart.yaml` mirrors the frontend's (name `backend`, appVersion `"1.10"`).
`charts/backend/templates/deployment.yaml` — the `--name` arg embeds the global environment so
the echo response proves propagation:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "backend.fullname" . }}
  labels:
    {{- include "backend.labels" . | nindent 4 }}
spec:
  replicas: {{ .Values.replicaCount }}
  selector:
    matchLabels:
      app.kubernetes.io/name: {{ .Chart.Name }}
      app.kubernetes.io/instance: {{ .Release.Name }}
  template:
    metadata:
      labels:
        app.kubernetes.io/name: {{ .Chart.Name }}
        app.kubernetes.io/instance: {{ .Release.Name }}
    spec:
      containers:
        - name: backend
          image: "{{ include "backend.image" . }}"
          args: ["--name={{ .Release.Name }}-backend ({{ .Values.global.environment | default "dev" }})"]
          ports:
            - { name: http, containerPort: 80 }
```

Its `service.yaml` is identical in shape to the frontend's (swap `frontend` → `backend`).

---

## Step 5: The `database` subchart (data tier, with a Secret + PVC)

`charts/database/values.yaml`:

```yaml
image:
  repository: postgres
  tag: "16-alpine"
auth:
  username: app
  database: appdb
  password: ""                 # required — the parent supplies it
persistence:
  enabled: true
  size: 100Mi                  # parent overrides to 200Mi
service:
  port: 5432
```

`charts/database/templates/secret.yaml` — the password is **required**, so a missing value
fails at render time (never ship a DB with a blank password):

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: {{ include "database.fullname" . }}
  labels:
    {{- include "database.labels" . | nindent 4 }}
type: Opaque
stringData:
  POSTGRES_PASSWORD: {{ required "database.auth.password is required" .Values.auth.password | quote }}
```

`charts/database/templates/pvc.yaml` — conditional, same pattern as Part 3:

```yaml
{{- if .Values.persistence.enabled }}
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: {{ include "database.fullname" . }}
  labels:
    {{- include "database.labels" . | nindent 4 }}
spec:
  accessModes: ["ReadWriteOnce"]
  resources:
    requests:
      storage: {{ .Values.persistence.size | quote }}
{{- end }}
```

`charts/database/templates/deployment.yaml` — pulls the password from the Secret, and uses a
`subPath` (Postgres refuses to init in a directory that already contains `lost+found`):

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "database.fullname" . }}
  labels:
    {{- include "database.labels" . | nindent 4 }}
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: {{ .Chart.Name }}
      app.kubernetes.io/instance: {{ .Release.Name }}
  template:
    metadata:
      labels:
        app.kubernetes.io/name: {{ .Chart.Name }}
        app.kubernetes.io/instance: {{ .Release.Name }}
    spec:
      containers:
        - name: postgres
          image: "{{ include "database.image" . }}"
          env:
            - { name: POSTGRES_USER, value: {{ .Values.auth.username | quote }} }
            - { name: POSTGRES_DB,   value: {{ .Values.auth.database | quote }} }
            - { name: PGDATA,        value: /var/lib/postgresql/data/pgdata }
            - name: POSTGRES_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: {{ include "database.fullname" . }}
                  key: POSTGRES_PASSWORD
          ports:
            - { name: postgres, containerPort: 5432 }
          {{- if .Values.persistence.enabled }}
          volumeMounts:
            - { name: data, mountPath: /var/lib/postgresql/data, subPath: pgdata }
          {{- end }}
      {{- if .Values.persistence.enabled }}
      volumes:
        - name: data
          persistentVolumeClaim: { claimName: {{ include "database.fullname" . }} }
      {{- end }}
```

Its `service.yaml` exposes port `5432` (named `postgres`), same shape as the others.

---

## Step 6: Render and inspect before deploying

```sh
cd webapp
helm lint .
helm template shop . | less        # one render contains ALL three subcharts
```

**Verify the three composition mechanics** without touching the cluster:

```sh
# 1) Parent override: backend.replicaCount 1 -> 2
helm template shop . | grep -B6 "replicas: 2"

# 2) Globals reached every subchart (label appears on all tiers)
helm template shop . | grep -c "environment: dev"        # > 0 across frontend/backend/database

# 3) Condition: turn the whole DB tier off
helm template shop . --set database.enabled=false | grep -c "shop-database"   # 0
```

---

## Step 7: Deploy and prove the tiers wire together

```sh
helm install shop ./webapp
kubectl rollout status deploy/shop-frontend --timeout=120s
kubectl rollout status deploy/shop-backend  --timeout=120s
kubectl rollout status deploy/shop-database --timeout=120s
kubectl get deploy,svc,pvc,secret -l app.kubernetes.io/part-of=webapp
```

Expected — note **backend has 2 replicas** (parent override) and the **DB PVC is 200Mi**
(parent override of the child's 100Mi default):

```text
deployment.apps/shop-backend    2/2   ...
deployment.apps/shop-database   1/1   ...
deployment.apps/shop-frontend   1/1   ...
persistentvolumeclaim/shop-database   Bound   ...   200Mi
secret/shop-database            Opaque   1   ...
```

**Prove the wiring is live:**

```sh
# Frontend homepage shows the global env + backend service name
kubectl exec deploy/shop-frontend -- curl -s http://localhost/ | grep -E "Environment|Backend"

# Frontend can actually REACH the backend over in-cluster DNS
kubectl exec deploy/shop-frontend -- curl -s http://shop-backend/ | grep -E "Name:|Hostname:"
# → Name: shop-backend (dev)      ← the global environment propagated into the backend!

# Database is up and accepting connections
kubectl exec deploy/shop-database -- pg_isready -U app
# → accepting connections
```

### Flip the environment for the whole stack

One value re-labels and re-tags every tier at once:

```sh
helm upgrade shop ./webapp --set global.environment=staging
kubectl exec deploy/shop-frontend -- curl -s http://shop-backend/ | grep Name
# → Name: shop-backend (staging)
```

### Cleanup

```sh
helm uninstall shop
kubectl delete pvc -l app.kubernetes.io/part-of=webapp
```

---

## How parent and child values combine (mental model)

| Mechanism | Where it lives | Effect |
|-----------|----------------|--------|
| **Child defaults** | `charts/backend/values.yaml` | Baseline for that subchart |
| **Parent override** | `backend:` block in `webapp/values.yaml` | Wins over the child's value of the same key |
| **`--set` / `-f`** at install | command line | Wins over everything |
| **Globals** | `global:` in parent | Copied into every subchart as `.Values.global.*` |
| **`condition:`** | parent `Chart.yaml` dependency | A boolean value includes/excludes the whole subchart |

Precedence (low → high): **child defaults → parent values → `-f file` → `--set`**.

## What You Learned

- A subchart is a chart in `charts/`; the umbrella **declares, configures, and wires** them
- `global.*` values propagate to every subchart — change a registry or environment once
- A parent's `backend:` / `database:` blocks **override** child defaults
- `condition:` toggles an entire tier on/off from one value
- Subcharts find each other by **`<release>-<subchart>`** Kubernetes DNS names
- One `helm install` deploys a coordinated multi-service application

You composed a real frontend + backend + database stack into a single, configurable release —
exactly the architecture from the course Overview. 🧩
