# Lab 08 — Centralized logs with Loki and Alloy

**Goal:** add a logs pipeline next to `kube-prometheus-stack`: Loki stores the logs, Grafana Alloy collects them, Grafana searches them. Then correlate a metric spike with the logs of the same pod.

**Time:** 45 minutes  
**Prerequisites:** Lab 01 completed (release `monitoring` in namespace `monitoring`). Lab 02 recommended.

**Maps to the deck:** appendix «Логи (EFK) и трейсы (Jaeger)» — the same idea with a lighter stack: Loki indexes only labels, not the log text.

> **Why not Promtail?** The `grafana/promtail` chart is marked `deprecated: true` in its `Chart.yaml`. Grafana Alloy is the supported collector, so this lab uses it.

---

## Версии

```bash
export LOKI_VER=7.3.0     # проверено 2026-10-12 (batchC): образ grafana/loki:3.6.11
export ALLOY_VER=1.13.1   # проверено 2026-10-12 (batchC): appVersion v1.20.1
```

> `проверь, если новее: helm search repo grafana/loki --versions | head -3` и то же для `grafana/alloy`. Если берёте другую версию, запишите её и используйте в обоих командах `helm upgrade` ниже.

## Step 1. Add the Grafana Helm repository

```bash
helm repo add grafana https://grafana.github.io/helm-charts
helm repo update
echo "loki=$LOKI_VER alloy=$ALLOY_VER"
```

> Expected: both variables print. An empty value means the `export` lines above were not run in this shell.

## Step 2. Install Loki in single-binary mode

The chart defaults to a distributed `SimpleScalable` mode that needs object storage. For a lab we switch to one pod with filesystem storage.

```bash
cat > loki-values.yaml <<'EOF'
deploymentMode: SingleBinary
loki:
  auth_enabled: false
  commonConfig:
    replication_factor: 1
  storage:
    type: filesystem
  useTestSchema: true
singleBinary:
  replicas: 1
  persistence:
    size: 2Gi
read:    { replicas: 0 }
write:   { replicas: 0 }
backend: { replicas: 0 }
gateway:      { enabled: false }
chunksCache:  { enabled: false }
resultsCache: { enabled: false }
lokiCanary:   { enabled: false }
test:         { enabled: false }
EOF

helm upgrade --install loki grafana/loki \
  --namespace logging --create-namespace \
  --version $LOKI_VER -f loki-values.yaml

kubectl rollout status statefulset/loki -n logging --timeout=5m
```

> `useTestSchema: true` is a chart shortcut for playing around. A real install needs an explicit `loki.schemaConfig` and object storage (S3/GCS).

> Образ, который тянет chart 7.3.0, — `grafana/loki:3.6.11` (batchC). `helm search` показывал appVersion 3.6.12, поэтому не полагайтесь на appVersion. Проверка: `kubectl get statefulset loki -n logging -o jsonpath='{.spec.template.spec.containers[0].image}'`.

Check that Loki is ready:

```bash
kubectl port-forward svc/loki -n logging 3100:3100 &
until curl -sf localhost:3100/ready; do sleep 2; done
```

> Expected: `ready`. The loop waits for the port-forward to come up (a bare `curl` right after `&` fails with exit code 7) and for the ingester, which can answer `Ingester not ready` for ~15 seconds.

## Step 3. Install Alloy as the collector

Alloy discovers pods through the Kubernetes API, attaches labels, and pushes the lines to Loki.

```bash
cat > alloy-values.yaml <<'EOF'
controller:
  type: deployment
  replicas: 1
alloy:
  configMap:
    content: |
      discovery.kubernetes "pods" {
        role = "pod"
      }

      discovery.relabel "pods" {
        targets = discovery.kubernetes.pods.targets
        rule {
          source_labels = ["__meta_kubernetes_namespace"]
          target_label  = "namespace"
        }
        rule {
          source_labels = ["__meta_kubernetes_pod_name"]
          target_label  = "pod"
        }
        rule {
          source_labels = ["__meta_kubernetes_pod_container_name"]
          target_label  = "container"
        }
        rule {
          source_labels = ["__meta_kubernetes_pod_label_app"]
          target_label  = "app"
        }
      }

      loki.source.kubernetes "pods" {
        targets    = discovery.relabel.pods.output
        forward_to = [loki.write.default.receiver]
      }

      loki.write "default" {
        endpoint {
          url = "http://loki.logging.svc:3100/loki/api/v1/push"
        }
      }
EOF

helm upgrade --install alloy grafana/alloy \
  --namespace logging --create-namespace \
  --version $ALLOY_VER -f alloy-values.yaml

kubectl rollout status deploy/alloy -n logging --timeout=5m
```

Check that Alloy is allowed to read pod logs and is pushing them to Loki. A missing RBAC rule fails silently: the pod is Running but no logs arrive.

```bash
kubectl get clusterrole | grep alloy
kubectl logs deploy/alloy -n logging --tail=20 | grep -iE "error|forbidden|denied" || echo "no errors in Alloy log"
curl -s "localhost:3100/loki/api/v1/label/namespace/values"
```

> Expected: a `clusterrole` for alloy, no `forbidden` lines, and a JSON answer whose `data` contains namespaces such as `kube-system`. An empty `data` array means nothing reached Loki yet — wait 30 seconds and retry; if it stays empty, read the Alloy log in full. The `curl` needs the Loki port-forward from Step 2.

> One Deployment replica is enough here: `loki.source.kubernetes` reads logs through the API server. In production run Alloy as a DaemonSet with `clustering` enabled so the targets are shared between pods instead of duplicated.

Labels are the whole index in Loki. Keep them few and low-cardinality: `namespace`, `pod`, `container`, `app` are fine, a `user_id` label is not.

## Step 4. Connect Loki to Grafana

Add the datasource in a separate values file, layered on top of `kps-values.yaml` from Lab 01. Always pass both files, so the Lab 01 settings are not reset:

```bash
cat > grafana-loki-ds.yaml <<'EOF'
grafana:
  additionalDataSources:
    - name: Loki
      type: loki
      access: proxy
      url: http://loki.logging.svc:3100
EOF

KPS_VER=$(helm get metadata monitoring -n monitoring -o json | jq -r .version)
echo "kube-prometheus-stack=$KPS_VER"

helm upgrade monitoring prometheus-community/kube-prometheus-stack \
  --namespace monitoring --version $KPS_VER \
  -f kps-values.yaml -f grafana-loki-ds.yaml

kubectl rollout status deploy/monitoring-grafana -n monitoring --timeout=5m
kubectl port-forward svc/monitoring-grafana -n monitoring 3000:80
```

Open `http://localhost:3000` → **Connections → Data sources** and check that **Loki** appears (use **Save & test**).

> `проверь helm get metadata --help` on your Helm version, and confirm that `version` in its JSON is the chart version. The command needs `jq`.
>
> The `port-forward` above holds the terminal. Run it in a separate window, or add `&` and stop it with `pkill -f "port-forward svc/monitoring-grafana"` during Cleanup.

---

## Step 5. Deploy a log generator

Two workloads in one namespace: `log-gen` writes a JSON line per second, `log-burst` (later) will cause a spike.

```bash
kubectl create namespace demo-logs
```

Save the manifest below as `log-gen.yaml` (the apply command after it reads that file):

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: log-gen
  namespace: demo-logs
spec:
  replicas: 1
  selector:
    matchLabels:
      app: log-gen
  template:
    metadata:
      labels:
        app: log-gen
    spec:
      containers:
        - name: log-gen
          image: busybox:1.36
          command: ["/bin/sh", "-c"]
          args:
            - |
              while true; do
                R=$(( $(date +%s) % 10 ))
                if [ $R -ge 8 ]; then L=error; S=500; else L=info; S=200; fi
                echo "{\"level\":\"$L\",\"status\":$S,\"path\":\"/cart\",\"msg\":\"request handled\"}"
                sleep 1
              done
          resources:
            requests: { cpu: 10m, memory: 16Mi }
            limits: { memory: 32Mi }
```

```bash
kubectl apply -f log-gen.yaml
kubectl rollout status deploy/log-gen -n demo-logs --timeout=3m
kubectl logs deploy/log-gen -n demo-logs --tail=3
```

## Step 6. Query logs with LogQL

In Grafana open **Explore**, select the **Loki** datasource, and switch the editor to **Code** mode.

**1. Stream selector (labels only, fast):**

```logql
{namespace="demo-logs", app="log-gen"}
```

**2. Line filter (text, scans the chunks):**

```logql
{namespace="demo-logs", app="log-gen"} |= "error"
```

**3. Parse JSON and filter by a field:**

```logql
{namespace="demo-logs", app="log-gen"} | json | level="error"
```

**4. Logs turned into a metric — errors per second:**

```logql
sum(rate({namespace="demo-logs", app="log-gen"} | json | level="error" [1m]))
```

**5. Error share over 5 minutes:**

```logql
sum(count_over_time({namespace="demo-logs", app="log-gen"} | json | level="error" [5m]))
/
sum(count_over_time({namespace="demo-logs", app="log-gen"} [5m]))
```

Discuss:
- Why is the stream selector in `{}` fast and `|= "error"` slower?
- Where would you put `user_id` in this setup: as a label or in the log line?

---

## Step 7. Correlate a metric spike with logs

Start a short burst for 90 seconds. The loop sleeps 0.05 s between lines (about 20 lines per second) and the pod has CPU and memory limits, so the burst does not starve the node. `kubectl run` в kubectl 1.37 не принимает `--limits` (`unknown flag`), поэтому burst запускается Pod-манифестом `log-burst.yaml`:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: log-burst
  namespace: demo-logs
  labels:
    app: log-burst
spec:
  restartPolicy: Never
  containers:
    - name: burst
      image: busybox:1.36
      command: ["/bin/sh", "-c"]
      args:
        - |
          timeout 90 sh -c 'while true; do echo "{\"level\":\"error\",\"status\":503,\"msg\":\"upstream timeout\"}"; sleep 0.05; done'
      resources:
        requests: { cpu: 10m, memory: 16Mi }
        limits: { cpu: 100m, memory: 64Mi }
```

```bash
kubectl apply -f log-burst.yaml
kubectl wait --for=condition=Ready pod/log-burst -n demo-logs --timeout=90s
kubectl logs pod/log-burst -n demo-logs --tail=3
```

> Проверено batchC (2026-10-12): строки `upstream timeout` (level error) появляются в Loki примерно по 20 в секунду.

While it runs, in **Explore** open a **split view** with the same time range:

- Left, Prometheus (CPU of the burst pod):

```promql
rate(container_cpu_usage_seconds_total{namespace="demo-logs", pod="log-burst", container!=""}[1m])
```

- Right, Loki (what the pod was saying):

```logql
sum by (pod) (rate({namespace="demo-logs"} | json | level="error" [30s]))
```

Select the window of the burst on the left graph — Grafana syncs the time range to the right panel. Click a stream and open the raw lines to find `upstream timeout`.

> Честно про метрику: при этой нагрузке CPU-«пик» почти не виден. В batchC `rate(container_cpu_usage_seconds_total{pod="log-burst"}[1m])` достигал лишь ~0.0008 ядра при лимите 100m. Поэтому на левом графике вы можете не увидеть всплеска. Чтобы его получить, увеличьте нагрузку (уменьшите `sleep`, добавьте циклов) — `проверь` на своём кластере. Правый график (логи) при этом работает.

> This is the workflow from the deck: the metric tells **where** it burns, the log tells **what** burned.

## Step 8. Alert from logs (idea, 5 minutes)

A LogQL metric query can drive an alert in the same way as PromQL. In Grafana: **Alerting → Alert rules → New alert rule**, datasource **Loki**, query:

```logql
sum(rate({namespace="demo-logs"} | json | level="error" [1m])) > 5
```

Discuss:
- Which alerts are better as metrics (SLO burn rate, see Lab 06) and which only exist in logs (a rare exception, a security event)?
- What happens to such an alert if the collector stops? (A missing log stream looks like «no errors».)

---

### Cleanup (только если вы останавливаетесь здесь)

> Full teardown order: [module-teardown.md](module-teardown.md).

```bash
pkill -f "port-forward svc/loki" 2>/dev/null
pkill -f "port-forward svc/monitoring-grafana" 2>/dev/null
kubectl delete namespace demo-logs
```

Re-applying the release without `grafana-loki-ds.yaml` does **not** remove the Loki datasource (проверено batchC, 2026-10-12): Grafana keeps the provisioned entry `Loki` (readOnly, health «Unable to connect with Loki»), and the API refuses to delete it (`Cannot delete read-only data source`). The verified way to remove it is to uninstall the `monitoring` release: Grafana goes with it, and so does the datasource. Так что этот cleanup — полный, он рассчитан на остановку в конце лабы:

```bash
pkill -f "port-forward svc/loki" 2>/dev/null
pkill -f "port-forward svc/monitoring-grafana" 2>/dev/null
kubectl delete namespace demo-logs
helm uninstall alloy -n logging
helm uninstall loki -n logging
kubectl delete namespace logging
helm uninstall monitoring -n monitoring
rm -f loki-values.yaml alloy-values.yaml grafana-loki-ds.yaml log-gen.yaml log-burst.yaml
unset LOKI_VER ALLOY_VER KPS_VER
```

> Если вы продолжаете со стеком (Labs 02–07), не удаляйте релиз `monitoring`: datasource Loki останется и будет указывать на удалённый сервис. Способ убрать его без удаления релиза не проверен (`проверь grafana.deleteDatasources` в values chart kube-prometheus-stack). Удаление файла `grafana-loki-ds.yaml` из слоёв, как и `helm upgrade` без него, datasource не убирает.
>
> Лабы редактируют один и тот же `kps-values.yaml` на месте (Lab 04 добавляет туда `alertmanagerConfigMatcherStrategy`), `kps-values.yaml` при cleanup не удаляйте: его используют последующие лабы и teardown. Namespace `monitoring` и CRD удаляются в [module-teardown.md](module-teardown.md).

---

## Done when

- ☐ `kubectl get pods -n logging` shows `loki-0` and `alloy-*` Running
- ☐ `curl -s localhost:3100/loki/api/v1/label/namespace/values` returns namespaces (Alloy is shipping data)
- ☐ Grafana → Data sources → **Loki** passes *Save & test*
- ☐ `{namespace="demo-logs", app="log-gen"}` returns lines in Explore
- ☐ The `| json | level="error"` query returns only error lines
- ☐ In the split view you found the `upstream timeout` lines inside the burst window (CPU-всплеск на левом графике может быть почти незаметен, см. примечание в Step 7)

---

## What you learned

- Loki indexes **labels**, not the text, so it is cheap to run but needs disciplined labels
- Alloy replaces Promtail as the supported collector; the chart for Promtail is deprecated
- LogQL has the same shape as PromQL: selector → filters → range aggregation
- Metrics show where, logs show what — Grafana's split view joins them by time
