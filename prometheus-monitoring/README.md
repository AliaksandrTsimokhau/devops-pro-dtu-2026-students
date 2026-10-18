# Monitoring labs — Prometheus, Kubernetes & VictoriaMetrics

Hands-on companion to the deck **"Мониторинг: от полёта вслепую к наблюдаемости"**. The labs follow the same hero's-journey arc: the audience is the engineer (Аня), and Prometheus is the **штурман** (navigator) who gives them instruments in the dark.

All labs were validated on a local **kind** cluster (k8s 1.35, Helm 4, podman) unless noted otherwise.

---

## Suggested order

| # | Lab | What you practise | Time | Deck Acts |
|---|---|---|---|---|
| 01 | [Install `kube-prometheus-stack`](lab-01-kube-prometheus-stack.md) | Deploy the stack; read Targets & Rules | 30 min | 1, 5 |
| 02 | [`ServiceMonitor` & alerts](lab-02-servicemonitor-and-alerts.md) | Add a scrape target + a `PrometheusRule` | 35 min | 3, 4 |
| 03 | [PromQL & troubleshooting](lab-03-promql-and-troubleshooting.md) | Core queries, histograms, cardinality, recording rules | 30 min | 2–5 |
| 03b | [Break & fix](lab-03b-break-and-fix.md) | Fix kube-proxy scrape, a broken `ServiceMonitor`, a cardinality explosion | 40 min | 5 |
| 04 | [Alertmanager routing](lab-04-alertmanager-routing.md) | Route an alert to a webhook; silence it | 35 min | 4 |
| 05 | [Grafana dashboards as code](lab-05-grafana-dashboards-as-code.md) | Provision a dashboard via ConfigMap; import/export | 30 min | 5 |
| 06 | [SLOs & burn-rate alerts](lab-06-slo-burn-rate-alerts.md) | Express an SLO; multi-window burn-rate alerting | 35 min | 4 |
| 07 | [VictoriaMetrics migration](lab-07-victoriametrics-migration.md) | Parallel-run VM, auto-convert CRDs, compare cost | 60 min | 6 |

**Core path** (≈2.5 h): 01 → 02 → 03.
**Production reality** (≈2 h): 03b → 04 → 06.
**Operations & scale** (≈1.5 h): 05 → 07.

---

## Prerequisites

- A Kubernetes cluster. No cluster? Create one with kind:
  - Docker: `kind create cluster --name dtu`
  - podman: `KIND_EXPERIMENTAL_PROVIDER=podman kind create cluster --name dtu`
- `kubectl`, `helm` (v3 or v4), and port-forward access.

---

## Conventions

- The monitoring stack is installed as Helm release **`monitoring`** in namespace **`monitoring`** (Lab 01). Most resource and service names derive from that release name.
- Custom `ServiceMonitor` / `PrometheusRule` / `AlertmanagerConfig` objects must carry the label **`release: monitoring`** — that's what the operator's selectors match. Forgetting it is the #1 "why isn't it picked up?" cause (drilled in Lab 03b).
- After applying a `ServiceMonitor` or rule, allow **1–3 minutes** for the operator to reconcile and Prometheus to reload before assuming a misconfiguration.
- On kind, the `kube-controller-manager`, `kube-scheduler`, `kube-etcd`, and `kube-proxy` targets are DOWN by default — expected, and fixed for kube-proxy in Lab 03b.

---

## Full reset

```bash
helm uninstall monitoring -n monitoring
kubectl delete ns monitoring
# if you ran Lab 07:
helm uninstall vmks -n vm && kubectl delete ns vm
```
