# Lab 02 — Add application metrics with `ServiceMonitor`

**Goal:** deploy a sample app that exposes `/metrics`, connect it to Prometheus with `ServiceMonitor`, and add one alert rule.

**Time:** 35 minutes  
**Prerequisites:** Lab 01 completed.

**Maps to the deck:** Act 3 (scraping & `ServiceMonitor`) and Act 4 (alerts).

---

## Step 1. Create a demo namespace

```bash
kubectl create namespace demo-monitoring
```

## Step 2. Deploy a sample metrics app

Apply this manifest:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: demo-metrics
  namespace: demo-monitoring
spec:
  replicas: 1
  selector:
    matchLabels:
      app: demo-metrics
  template:
    metadata:
      labels:
        app: demo-metrics
    spec:
      containers:
        - name: demo-metrics
          image: prom/statsd-exporter:v0.26.1
          ports:
            - containerPort: 9102
              name: http
          readinessProbe:
            httpGet:
              path: /metrics
              port: http
            initialDelaySeconds: 5
          resources:
            requests:
              cpu: 25m
              memory: 32Mi
            limits:
              memory: 64Mi
---
apiVersion: v1
kind: Service
metadata:
  name: demo-metrics
  namespace: demo-monitoring
  labels:
    app: demo-metrics
spec:
  selector:
    app: demo-metrics
  ports:
    - name: http
      port: 9102
      targetPort: http
```

```bash
kubectl apply -f demo-metrics.yaml
kubectl rollout status deploy/demo-metrics -n demo-monitoring --timeout=5m   # first image pull can take minutes
kubectl get pods,svc -n demo-monitoring
```

---

## Step 3. Create a `ServiceMonitor`

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: demo-metrics
  namespace: monitoring
  labels:
    release: monitoring
spec:
  namespaceSelector:
    matchNames:
      - demo-monitoring
  selector:
    matchLabels:
      app: demo-metrics
  endpoints:
    - port: http
      interval: 15s
      path: /metrics
```

```bash
kubectl apply -f servicemonitor-demo.yaml
kubectl get servicemonitor -n monitoring
```

---

## Step 4. Confirm the target is scraped

> ⏳ Allow **1–3 minutes** after `kubectl apply`: the operator must regenerate the scrape config and Prometheus must reload it. If the target is missing immediately, wait before assuming a misconfiguration.

In Prometheus UI:
- open **Status -> Targets**
- find the `demo-metrics` target

Or query:

```promql
up{namespace="demo-monitoring"}
```

> Expected: a single series with value `1` and `job="demo-metrics"`.

If it does not appear, inspect:
- `Service` labels
- `ServiceMonitor` selector
- release label expected by the chart

---

## Step 5. Add an alert rule

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: demo-metrics-rules
  namespace: monitoring
  labels:
    release: monitoring
spec:
  groups:
    - name: demo-metrics.rules
      rules:
        - alert: DemoMetricsTargetDown
          expr: up{job="demo-metrics"} == 0 or absent(up{job="demo-metrics"})
          for: 2m
          labels:
            severity: warning
            namespace: demo-monitoring
          annotations:
            summary: Demo metrics target is down or absent
            description: Prometheus cannot scrape the demo target (scrape failing or target removed) for 2 minutes
```

```bash
kubectl apply -f demo-rules.yaml
kubectl get prometheusrule -n monitoring
```

> **Why `or absent(...)`?** Scaling to 0 removes the target, so `up` returns *nothing* — and `up == 0` matches nothing. `absent(up{...})` returns `1` when the series is gone, catching that case. `up == 0` alone only covers a failing scrape on a still-present target.

---

## Step 6. Trigger the alert

Scale the deployment to zero:

```bash
kubectl scale deployment demo-metrics -n demo-monitoring --replicas=0
```

Watch:
- **Alerts** page in Prometheus
- rule evaluation state

> Because of `for: 2m`, the alert first shows as **Pending** and only flips to **Firing** after the condition holds for 2 minutes. That delay is expected, not a bug.

Restore it:

```bash
kubectl scale deployment demo-metrics -n demo-monitoring --replicas=1
```

---

### Cleanup (только если вы останавливаетесь здесь)

> Removes this lab's demo app and rule. Labs 03–08 use them. Full teardown order: [module-teardown.md](module-teardown.md).

```bash
kubectl delete namespace demo-monitoring
kubectl delete servicemonitor demo-metrics -n monitoring
kubectl delete prometheusrule demo-metrics-rules -n monitoring
```

---

## Done when

- ☐ `demo-metrics` pod is Running and Ready
- ☐ `up{namespace="demo-monitoring"}` returns `1`
- ☐ `DemoMetricsTargetDown` shows under **Alerts**
- ☐ Scaling to 0 drives it Pending → Firing; scaling back clears it

---

## What you learned

- `ServiceMonitor` is the Kubernetes-native way to add scrape targets
- Labels and namespace selectors determine whether discovery works
- `PrometheusRule` turns scrape data into operational alerts
