# Lab 03 — PromQL and troubleshooting

**Goal:** use PromQL for common operational questions and diagnose typical Prometheus problems.

**Time:** 30 minutes  
**Prerequisites:** Lab 01 completed. Lab 02 recommended.

**Maps to the deck:** Acts 2–5 (metric types, PromQL, SLO thinking, production pitfalls).

---

## Part A — Core PromQL queries

Run the following queries and explain what each one answers.

### 1. Target health

```promql
up
```

### 2. Node CPU usage

```promql
100 - (avg by(instance) (rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)
```

### 3. Memory availability

```promql
# fraction of memory available (0–1). Multiply by 100 for a percentage.
node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes
```

### 4. Pod restarts

```promql
increase(kube_pod_container_status_restarts_total[1h])
```

---

## Part B — Histogram-based latency

The Kubernetes API server exposes a histogram that is scraped out of the box, so you can practice `histogram_quantile()` on **real data** without deploying anything:

```promql
# p95 API server request latency over 5 minutes
histogram_quantile(
  0.95,
  sum by (le) (rate(apiserver_request_duration_seconds_bucket[5m]))
)
```

The exact same shape works for any application histogram, e.g. an app exposing `http_request_duration_seconds_bucket`:

```promql
histogram_quantile(
  0.95,
  sum by (le) (rate(http_request_duration_seconds_bucket[5m]))
)
```

Discuss:
- Why does `histogram_quantile()` need buckets?
- Why is histogram aggregation safer than summary aggregation across replicas?

---

## Part C — Troubleshooting checklist

When a target is missing or down, check in this order:

1. Is the pod running?
2. Does the service select the right pods?
3. Does the app expose `/metrics`?
4. Does `ServiceMonitor` match the service labels?
5. Is the target visible in **Status -> Targets**?
6. Are there scrape errors or timeouts?

Useful commands:

```bash
kubectl get pods,svc,endpoints -A | grep demo-metrics
kubectl describe servicemonitor -n monitoring demo-metrics
kubectl logs -n demo-monitoring deploy/demo-metrics
```

---

## Part D — Cardinality awareness

Check internal Prometheus metrics:

```promql
prometheus_tsdb_head_series
```

```promql
topk(10, count by(__name__)({__name__!=""}))
```

Discuss:
- Which metrics dominate series count?
- Which labels in your environment could explode cardinality?
- What should never become a label?

---

## Part E — Recording rule exercise

Create a recording rule that stores cluster node readiness:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: platform-recording-rules
  namespace: monitoring
  labels:
    release: monitoring
spec:
  groups:
    - name: platform.recording
      rules:
        - record: cluster:node_ready:sum
          expr: sum(kube_node_status_condition{condition="Ready",status="true"})
```

Apply it:

```bash
kubectl apply -f platform-recording-rules.yaml
```

Verify (allow **1–3 minutes** for the operator to reconcile and Prometheus to reload the rule):

```promql
cluster:node_ready:sum
```

---

### Cleanup (только если вы останавливаетесь здесь)

> Full teardown order: [module-teardown.md](module-teardown.md).

```bash
kubectl delete prometheusrule platform-recording-rules -n monitoring
```

---

## Done when

- ☐ You can explain in one sentence what each Part A query answers
- ☐ `histogram_quantile()` returns a p95 latency value (Part B)
- ☐ You found the top series-count metrics and named one risky label (Part D)
- ☐ `cluster:node_ready:sum` returns your node count (Part E)

> Next: **Lab 03b — break & fix** turns these queries into live troubleshooting.

---

## What you learned

- PromQL is most useful when tied to operational questions
- Histogram-based queries support latency and SLO-style analysis
- Troubleshooting Prometheus is usually a discovery and labeling problem
- Internal Prometheus metrics help detect scale and cardinality issues
