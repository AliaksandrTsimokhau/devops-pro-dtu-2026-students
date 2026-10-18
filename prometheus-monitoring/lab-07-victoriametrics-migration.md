# Lab 07 — VictoriaMetrics migration (bonus lab)

> Bonus lab. The module capstone is in module 08.

**Goal:** run VictoriaMetrics alongside Prometheus, auto-convert existing `ServiceMonitor`s, query the same data with PromQL, and reason about a safe migration.

**Time:** 60 minutes
**Prerequisites:** Labs 01–02 completed (monitoring stack + a `ServiceMonitor` to convert).

**Maps to the deck:** Act 6 — "штурман получает новые приборы": the hero doesn't re-learn flying, the instruments just get better where it hurt (memory, retention, cardinality).

> ✅ **Validated on kind** (chart `victoria-metrics-k8s-stack` **0.85.4**, VM **v1.146.0**, k8s 1.35, podman). The service names and ports below are from that run. If you pin a different chart version, re-check them with `kubectl get svc -n vm`.

---

## Step 1. Add the VictoriaMetrics charts

```bash
helm repo add vm https://victoriametrics.github.io/helm-charts/
helm repo update vm
helm search repo vm/victoria-metrics-k8s-stack --versions | head -3   # pick a version to pin
```

---

## Step 2. Install VM next to Prometheus (parallel run)

Install the VM operator + a single-node VM database + a scraping agent into their own namespace. Keep Prometheus running — this is the "run both in parallel" migration stage.

```bash
kubectl create namespace vm --dry-run=client -o yaml | kubectl apply -f -

helm upgrade --install vmks vm/victoria-metrics-k8s-stack \
  --namespace vm \
  --version 0.85.4 \
  --set grafana.enabled=false \
  --set defaultDashboards.enabled=false \
  --set defaultRules.create=false \
  --set 'prometheus-node-exporter.enabled=false' \
  --set 'kube-state-metrics.enabled=false'
```

> We disable the bundled Grafana/exporters to avoid clashing with the Lab 01 stack — we only want VM's **operator + VMSingle + VMAgent** here.
>
> There are two migration paths. This lab uses the first: **VMAgent scrapes the same targets** in parallel with Prometheus. The second keeps Prometheus as the scraper and forwards its samples to VM with **`remote_write`**. Use the second when you want one scrape config and no duplicate scraping.

Wait for rollout (cold image pulls can take minutes):

```bash
kubectl get pods -n vm -w
```

---

## Step 3. Confirm the auto-conversion of `ServiceMonitor`s

The VM operator watches prometheus-operator CRDs and converts them to VM equivalents (`ServiceMonitor` → `VMServiceScrape`). This is what makes migration low-effort — you don't rewrite scrape configs.

```bash
# VM equivalents created automatically from existing prometheus-operator objects:
kubectl get vmservicescrape -A
kubectl get vmrule -A
```

> Expected (validated): on the Lab 01 stack this auto-creates **~23 `VMServiceScrape`** and **~71 `VMRule`** objects, all with `STATUS: operational`. If you completed Lab 02, look for a `demo-metrics` `VMServiceScrape`.

CRD name mapping (deck Act 6):

| Prometheus Operator | VictoriaMetrics Operator |
|---|---|
| `Prometheus` | `VMSingle` / `VMCluster` |
| `ServiceMonitor` | `VMServiceScrape` |
| `PodMonitor` | `VMPodScrape` |
| `PrometheusRule` | `VMRule` |
| `Alertmanager` | `VMAlertmanager` |

---

## Step 4. Query the same data in VM (PromQL works unchanged)

Port-forward VM's query UI (vmui) and run the *same* PromQL you used in Lab 03:

```bash
kubectl -n vm port-forward svc/vmsingle-vmks-victoria-metrics-k8s-stack 8428:8428
# open http://localhost:8428/vmui
# (single-node VM; a VMCluster install exposes vmselect instead — check `kubectl get svc -n vm`)
```

Run in vmui:

```promql
up
sum by (code) (rate(apiserver_request_total[5m]))
histogram_quantile(0.95, sum by (le) (rate(apiserver_request_duration_seconds_bucket[5m])))
```

> MetricsQL is a **superset** of PromQL — every Lab 03 query runs as-is. That's the whole point: the team doesn't relearn the query language.

> ⏳ VM starts scraping the moment it installs, so `up` returns immediately (validated: 23 series). But `rate(...[5m])` needs a few minutes of VM history before it fills in — if a rate query looks empty right after install, wait and retry.

---

## Step 5. Compare the cost

With both systems scraping the same targets, compare memory footprint:

```promql
# in Prometheus (Lab 01) and in vmui — compare the numbers
process_resident_memory_bytes{job=~".*prometheus.*"}
process_resident_memory_bytes{job=~".*victoriametrics.*|.*vmsingle.*"}
```

Discuss:
- On this tiny cluster the gap is small; at millions of series VM typically uses far less RAM. Why does that matter for the Act 5 cardinality pain?
- VM offers long-term retention "out of the box" — which Act 5 pitfall does that close?

---

## Step 6. Reason about the cut-over (don't actually do it)

The deck's migration arc: **parallel → dual-read/compare → switch Grafana → retire Prometheus**, with rollback possible while both run.

- You are at **parallel** now.
- Next you'd point a Grafana datasource at VM and compare the same dashboards on both sources.
- Only after they match for long enough do you switch Grafana's default and decommission Prometheus.

> No sane team migrates prod in one nightly big-bang. Parallel running is the safety net.

---

### Cleanup (только если вы останавливаетесь здесь)

> Delete the converted VM objects **before** you uninstall the operator, while it still runs. Your original `ServiceMonitor` objects are not touched. Full teardown order: [module-teardown.md](module-teardown.md).

```bash
kubectl get vmservicescrape,vmrule,vmpodscrape -A
kubectl delete vmservicescrape,vmrule,vmpodscrape -A --all
helm uninstall vmks -n vm
kubectl delete namespace vm
# VM CRDs may remain cluster-wide; remove if desired:
kubectl get crd | grep victoriametrics
```

---

## Done when

- ☐ VM operator, VMSingle, and VMAgent pods are Running in `vm`
- ☐ `kubectl get vmservicescrape -A` shows objects auto-converted from your `ServiceMonitor`s
- ☐ Your Lab 03 PromQL queries return data in vmui unchanged
- ☐ You can describe the parallel → switch migration with a rollback path

---

## What you learned

- VictoriaMetrics is a near-drop-in upgrade: same scrape protocol, PromQL-superset queries, CRD analogues
- The operator's auto-conversion of `ServiceMonitor`/`PrometheusRule` is what makes migration incremental
- Migration safety comes from running both in parallel and comparing before cutting over — never a big-bang
