# Lab 03b — Break & fix (troubleshooting drills)

**Goal:** turn the troubleshooting checklist from Lab 03 into muscle memory by *causing* three real failures and fixing them.

**Time:** 40 minutes
**Prerequisites:** Lab 01 completed (monitoring stack running). Lab 02 deployed (`demo-metrics`) for Exercise 2.

**Maps to the deck:** Act 5 (production pitfalls — discovery, cardinality, the `ServiceMonitor` release-label gotcha).

> Each exercise follows the same loop: **observe → break → diagnose → fix → verify**. Resist jumping to the fix — practise the diagnosis.

## Версии

- `avalanche` — `quay.io/prometheuscommunity/avalanche:v0.6.0`. Проверено 2026-10-12 (batchC): работает только с `--gauge-metric-count`, `--metric-count` вместе с ним не допускается. `проверь, если новее: --help у нового тега`.
- Стек (Prometheus, Grafana, Alertmanager) ставится из Lab 01 с `$KPS_CHART_VER` = 87.2.0 (проверено 2026-10-12, batchC).

---

## Exercise 1 — Make `kube-proxy` scrape (fix a DOWN target)

In Lab 01 you saw `kube-controller-manager`, `kube-scheduler`, `kube-etcd`, and `kube-proxy` as **DOWN** on kind. Here you fix `kube-proxy` for real — it is the easiest of the four because it is just a bind-address setting.

### Observe

```promql
up{job="kube-proxy"}
```

Returns `0`. The target error in **Status → Targets** is `connection refused` on port `10249`.

> If you installed Lab 01 with the optional `--set kubeProxy.enabled=false` (the "hide" variant), there is no `kube-proxy` target to fix. Reinstall the stack without that flag (`helm upgrade ... -f kps-values.yaml`, see Lab 01), or skip Exercise 1.

### Diagnose

`kube-proxy` exposes its metrics on the address in its ConfigMap. On kind it defaults to empty, which means `127.0.0.1` — unreachable from Prometheus:

```bash
kubectl -n kube-system get configmap kube-proxy \
  -o jsonpath='{.data.config\.conf}' | grep metricsBindAddress
# metricsBindAddress: ""
```

### Fix

Point the metrics endpoint at all interfaces, then restart the DaemonSet:

```bash
kubectl -n kube-system get configmap kube-proxy -o yaml \
  | sed 's/metricsBindAddress: ""/metricsBindAddress: "0.0.0.0:10249"/' \
  | kubectl apply -f -

kubectl -n kube-system rollout restart daemonset kube-proxy
kubectl -n kube-system rollout status  daemonset kube-proxy --timeout=3m
```

### Verify

After 1–2 minutes the target turns green:

```promql
up{job="kube-proxy"}      # expected: 1 per node
```

### Discuss

- Why is `127.0.0.1` the secure default, and why does it break in-cluster scraping?
- The other three (`controller-manager`, `scheduler`, `etcd`) need host-level flag changes you cannot make on kind. Why are control-plane components harder to monitor than workloads?

---

## Exercise 2 — The `ServiceMonitor` that is never discovered

The #1 real-world `ServiceMonitor` failure: the wrong `release` label, so the operator's selector ignores it.

### Break

Re-apply the Lab 02 `ServiceMonitor` with a wrong label (everything else identical):

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: demo-metrics-broken
  namespace: monitoring
  labels:
    release: wrong-release          # <-- the bug
spec:
  namespaceSelector:
    matchNames: [demo-monitoring]
  selector:
    matchLabels: { app: demo-metrics }
  endpoints:
    - port: http
      interval: 15s
      path: /metrics
```

```bash
kubectl apply -f servicemonitor-broken.yaml
```

### Observe

Wait 1–3 minutes, then check — the target never appears. Смотрите не `up`, а **Status → Targets**: scrape pool `serviceMonitor/monitoring/demo-metrics-broken/0` не появляется (scrape pool `serviceMonitor/monitoring/demo-metrics/0` из Lab 02 остаётся).

> `up{namespace="demo-monitoring"}` здесь не поможет: ServiceMonitor из Lab 02 скрейпит тот же pod с теми же лейблами, поэтому серия одинаковая до и после. Альтернатива — на время упражнения удалить ServiceMonitor из Lab 02.

### Diagnose

Walk the checklist. The pod is up, the Service selects it, `/metrics` works — so it is discovery. Compare the label the operator requires against what you set:

```bash
# what label does the Prometheus CR require on ServiceMonitors?
kubectl -n monitoring get prometheus -o jsonpath='{.items[0].spec.serviceMonitorSelector}{"\n"}'
# {"matchLabels":{"release":"monitoring"}}

# what label does the broken one carry?
kubectl -n monitoring get servicemonitor demo-metrics-broken --show-labels
```

`release=wrong-release` ≠ `release=monitoring` → never selected.

### Fix

```bash
kubectl -n monitoring label servicemonitor demo-metrics-broken release=monitoring --overwrite
```

### Verify

Status → Targets: pool `serviceMonitor/monitoring/demo-metrics-broken/0` появляется и его target `UP` в течение ~2 min.

```promql
up{namespace="demo-monitoring"}     # Lab 02 target уже даёт 1; сам по себе этот запрос не доказывает починку
```

### Cleanup (только если вы останавливаетесь здесь)

> Full teardown order: [module-teardown.md](module-teardown.md).

```bash
kubectl -n monitoring delete servicemonitor demo-metrics-broken
```

---

## Exercise 3 — Trigger (and contain) a cardinality explosion

Make the deck's "remember this word — cardinality" tangible: generate thousands of synthetic series and watch Prometheus' own health metric react.

### Break

Deploy a load generator that produces high-cardinality series (`avalanche` is purpose-built for this), plus a `ServiceMonitor` to scrape it. Флаги ниже проверены на `avalanche:v0.6.0` (batchC, 2026-10-12). Вариант с `--metric-count=50` падает с ошибкой конфигурации (`--metric-count` deprecated и конфликтует с дефолтным `--gauge-metric-count=500`), поэтому используется `--gauge-metric-count=50`. `проверь --help` (`docker run --rm quay.io/prometheuscommunity/avalanche:v0.6.0 --help`), если берёте другой тег:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: avalanche
  namespace: demo-monitoring
spec:
  replicas: 1
  selector: { matchLabels: { app: avalanche } }
  template:
    metadata: { labels: { app: avalanche } }
    spec:
      containers:
        - name: avalanche
          image: quay.io/prometheuscommunity/avalanche:v0.6.0
          args:
            - --gauge-metric-count=50
            - --series-count=200
            - --label-count=10
            - --port=9001
          ports:
            - { containerPort: 9001, name: http }
---
apiVersion: v1
kind: Service
metadata:
  name: avalanche
  namespace: demo-monitoring
  labels: { app: avalanche }
spec:
  selector: { app: avalanche }
  ports: [{ name: http, port: 9001, targetPort: http }]
---
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: avalanche
  namespace: monitoring
  labels: { release: monitoring }
spec:
  namespaceSelector: { matchNames: [demo-monitoring] }
  selector: { matchLabels: { app: avalanche } }
  endpoints: [{ port: http, interval: 15s }]
```

```bash
kubectl apply -f avalanche.yaml
```

### Observe

Record the baseline first, then watch it climb after the scrape starts (1–3 min):

```promql
prometheus_tsdb_head_series
```

### Diagnose

Find what is dominating the series count — the same query as Lab 03 Part D:

```promql
topk(10, count by(__name__)({__name__!=""}))
```

`avalanche_*` в этот топ-10 **не попадают** (проверено batchC): у каждой avalanche-метрики около 200 серий, а 10-е место в списке — около 920 серий. Чтобы найти их, отфильтруйте по имени: `count by(__name__)({__name__=~"avalanche_.*"})`. В реальном инциденте сравнивайте `prometheus_tsdb_head_series` до и после появления приложения и ищите лейбл с большим числом значений.

### Fix (contain)

Remove the generator and confirm the series count drops back:

```bash
kubectl -n monitoring delete servicemonitor avalanche
kubectl -n demo-monitoring delete deploy,svc avalanche
```

```promql
prometheus_tsdb_head_series     # drops after the head block is compacted; проверь, не ждите compaction: head GC can take hours
```

> В batchC значение оставалось на уровне ~77k ещё ~3 минуты после удаления. Это ожидаемо: head compaction запаздывает.

### Discuss

- Which label in *your* real services could behave like avalanche's synthetic labels?
- How would `metricRelabelings` (drop a label before ingest) contain this without removing the app?
- What alert on `prometheus_tsdb_head_series` would warn you *before* Prometheus OOMs?

---

## What you learned

- Most "Prometheus is broken" tickets are discovery or labeling problems, not Prometheus bugs
- The operator only adopts `ServiceMonitor`/`PrometheusRule` objects whose labels match its selectors — the `release` label is the usual culprit
- Cardinality is a runtime risk you can measure (`prometheus_tsdb_head_series`) and locate (`topk by __name__`) before it takes the server down
- Control-plane components on kind are intentionally hard to scrape — a realistic taste of monitoring managed infrastructure
