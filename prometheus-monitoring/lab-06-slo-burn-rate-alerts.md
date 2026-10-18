# Lab 06 — SLOs and burn-rate alerts

**Goal:** express an availability **SLO** as recording rules and alert on **error-budget burn rate** instead of raw thresholds.

**Time:** 35 minutes
**Prerequisites:** Lab 01 completed. Lab 03 (PromQL) recommended.

**Maps to the deck:** Act 4 — SLOs turn metrics into promises; burn-rate alerts are quieter and more meaningful than "CPU > 80%".

> We use the Kubernetes API server as the service under test — its `apiserver_request_total` counter is scraped out of the box, so no app deploy is needed.

---

## Concepts (90-second recap)

- **SLI** — what we measure: ratio of *good* requests (non-5xx).
- **SLO** — the promise: e.g. **99.9%** of requests succeed over 30 days.
- **Error budget** — the allowed failure: `1 - 0.999 = 0.1%`.
- **Burn rate** — how fast you're spending that budget. Burn rate `1` = exactly on pace to exhaust the budget in the window; `14.4` = you'd burn a 30-day budget in ~2 days.

---

## Step 1. Record the SLI at multiple windows

Multi-window burn-rate alerting needs the error ratio over short *and* long windows. Record them once so dashboards and alerts stay fast:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: apiserver-slo-recording
  namespace: monitoring
  labels: { release: monitoring }
spec:
  groups:
    - name: apiserver.slo.recording
      rules:
        - record: apiserver:request_errors:ratio_rate5m
          expr: |
            sum(rate(apiserver_request_total{code=~"5.."}[5m]))
              / sum(rate(apiserver_request_total[5m]))
        - record: apiserver:request_errors:ratio_rate30m
          expr: |
            sum(rate(apiserver_request_total{code=~"5.."}[30m]))
              / sum(rate(apiserver_request_total[30m]))
        - record: apiserver:request_errors:ratio_rate1h
          expr: |
            sum(rate(apiserver_request_total{code=~"5.."}[1h]))
              / sum(rate(apiserver_request_total[1h]))
        - record: apiserver:request_errors:ratio_rate6h
          expr: |
            sum(rate(apiserver_request_total{code=~"5.."}[6h]))
              / sum(rate(apiserver_request_total[6h]))
```

```bash
kubectl apply -f apiserver-slo-recording.yaml
```

Verify (allow 1–3 min for reload):

```promql
apiserver:request_errors:ratio_rate5m
```

> On a healthy cluster this is ~`0`. That's correct — you're not burning budget. The point is to alert when it isn't.

---

## Step 2. Multi-window burn-rate alerts

SLO `99.9%` → error budget `0.001`. **Burn rate = error ratio / 0.001.** Two pairs of windows catch fast and slow burns while avoiding flapping:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: apiserver-slo-alerts
  namespace: monitoring
  labels: { release: monitoring }
spec:
  groups:
    - name: apiserver.slo.alerts
      rules:
        # Fast burn: ~2% of the 30-day budget in 1 hour -> page
        - alert: APIServerErrorBudgetFastBurn
          expr: |
            apiserver:request_errors:ratio_rate5m / 0.001 > 14.4
              and apiserver:request_errors:ratio_rate1h / 0.001 > 14.4
          for: 2m
          labels: { severity: critical }
          annotations:
            summary: API server is burning error budget fast
            description: ">14.4x burn over 5m and 1h windows"
        # Slow burn: ~5% of budget in 6 hours -> ticket
        - alert: APIServerErrorBudgetSlowBurn
          expr: |
            apiserver:request_errors:ratio_rate30m / 0.001 > 6
              and apiserver:request_errors:ratio_rate6h / 0.001 > 6
          for: 15m
          labels: { severity: warning }
          annotations:
            summary: API server is burning error budget slowly
            description: ">6x burn over 30m and 6h windows"
```

```bash
kubectl apply -f apiserver-slo-alerts.yaml
```

---

## Step 3. Inspect the burn rate

See the live burn-rate number even while it's healthy:

```promql
apiserver:request_errors:ratio_rate5m / 0.001
```

Discuss:
- Why require **both** a short and a long window to fire? (Short alone = flapping on blips; long alone = slow to detect.)
- Why is `14.4` the fast-burn multiplier? (`14.4 × 1h = ~2%` of a 30-day budget — the page-worthy threshold from the Google SRE workbook.)
- How would you graph "budget remaining this month" for stakeholders?

---

### Cleanup (только если вы останавливаетесь здесь)

> Full teardown order: [module-teardown.md](module-teardown.md).

```bash
kubectl delete prometheusrule apiserver-slo-recording apiserver-slo-alerts -n monitoring
```

---

## Done when

- ☐ The four recording rules return values (Step 1)
- ☐ Both burn-rate alerts load under **Alerts** (Inactive on a healthy cluster — expected)
- ☐ You can explain what burn rate `> 14.4` means in budget terms

---

## What you learned

- An SLO is a recording-rule ratio plus a target; the error budget is `1 - target`
- Burn-rate alerts fire on *how fast you're spending the budget*, not on raw resource thresholds
- Multi-window (short AND long) alerting is the standard cure for both flapping and slow detection
