# Lab 04 — Alertmanager routing and silences

**Goal:** make an alert actually *reach a human*. Route the Lab 02 alert to a webhook receiver, watch it arrive, then silence it.

**Time:** 35 minutes
**Prerequisites:** Lab 01 + Lab 02 completed. The `demo-metrics` deployment and `DemoMetricsTargetDown` alert from Lab 02 must exist.

**Maps to the deck:** Act 4 — an alert is only useful if it triggers action; Alertmanager is the delivery layer.

> Until now alerts only lit up the Prometheus **Alerts** page. Alertmanager is what turns a firing alert into a notification (Slack, PagerDuty, webhook…).

---

## Step 1. Deploy a webhook "inbox"

A tiny echo server stands in for Slack/PagerDuty — it logs every notification it receives:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: alert-sink
  namespace: monitoring
spec:
  replicas: 1
  selector: { matchLabels: { app: alert-sink } }
  template:
    metadata: { labels: { app: alert-sink } }
    spec:
      containers:
        - name: echo
          image: mendhak/http-https-echo:31
          env:
            - { name: HTTP_PORT, value: "8080" }
          ports:
            - { containerPort: 8080, name: http }
---
apiVersion: v1
kind: Service
metadata:
  name: alert-sink
  namespace: monitoring
spec:
  selector: { app: alert-sink }
  ports: [{ name: http, port: 8080, targetPort: http }]
```

```bash
kubectl apply -f alert-sink.yaml
kubectl rollout status deploy/alert-sink -n monitoring --timeout=5m
```

---

## Step 2. Let a namespaced config route cluster-wide alerts

By default the operator's `alertmanagerConfigMatcherStrategy` is **OnNamespace**: it auto-injects a `namespace="<config's namespace>"` matcher into every route from an `AlertmanagerConfig`. Our `AlertmanagerConfig` will live in `monitoring`, but the `DemoMetricsTargetDown` alert carries `namespace="demo-monitoring"` — so OnNamespace would silently drop it.

Switch the strategy to `None` so routing is driven purely by the matchers you write. Set it in the values file, not with `kubectl patch`: the release is managed by Helm, and a patch would be overwritten by the next `helm upgrade`. Add the key under the existing `alertmanager.alertmanagerSpec` block of `kps-values.yaml` (from Lab 01):

```yaml
alertmanager:
  alertmanagerSpec:
    alertmanagerConfigMatcherStrategy:
      type: None
```

Apply the file to the release. Keep `-f kps-values.yaml` on every upgrade:

```bash
helm upgrade --install monitoring prometheus-community/kube-prometheus-stack \
  --namespace monitoring --version 87.2.0 \
  -f kps-values.yaml
```

> `проверь через helm show values prometheus-community/kube-prometheus-stack | grep -n -A4 alertmanagerConfigMatcherStrategy`: confirm the key path and the accepted `type` values for your chart version.

> This is the single most common reason "my AlertmanagerConfig does nothing." Understanding it is half the lab.

---

## Step 3. Route the alert to the webhook

```yaml
apiVersion: monitoring.coreos.com/v1alpha1   # note: v1alpha1, not v1
kind: AlertmanagerConfig
metadata:
  name: demo-webhook
  namespace: monitoring
  labels: { release: monitoring }
spec:
  route:
    receiver: webhook
    groupBy: ["alertname"]
    groupWait: 10s
    groupInterval: 1m
    repeatInterval: 5m
    matchers:
      - name: alertname
        value: DemoMetricsTargetDown
  receivers:
    - name: webhook
      webhookConfigs:
        - url: http://alert-sink.monitoring.svc:8080/
```

```bash
kubectl apply -f alertmanagerconfig-demo.yaml
```

---

## Step 4. Trigger and watch it arrive

Break the demo target (from Lab 02) and tail the webhook inbox:

```bash
kubectl scale deployment demo-metrics -n demo-monitoring --replicas=0
# wait ~2-3 min: alert goes Pending -> Firing -> Alertmanager delivers
kubectl logs -n monitoring deploy/alert-sink -f
```

> Expected: a JSON payload logged by `alert-sink` containing `"status": "firing"` and `"alertname": "DemoMetricsTargetDown"`. Echo-сервер печатает JSON с пробелом после двоеточия (проверено batchC, 2026-10-12), поэтому `grep '"status":"firing"'` ничего не найдёт; ищите `firing` без пробела. That JSON is exactly what a Slack/webhook integration would receive.

Restore the target and watch a `"status":"resolved"` payload arrive:

```bash
kubectl scale deployment demo-metrics -n demo-monitoring --replicas=1
```

---

## Step 5. Silence the alert

Break it again, then add a silence via `amtool` inside the Alertmanager pod:

```bash
kubectl scale deployment demo-metrics -n demo-monitoring --replicas=0

kubectl -n monitoring exec -it alertmanager-monitoring-kube-prometheus-alertmanager-0 -c alertmanager -- \
  amtool silence add alertname=DemoMetricsTargetDown \
  --duration=1h --comment="lab maintenance" \
  --alertmanager.url=http://localhost:9093
```

> While the silence is active, the alert still **fires in Prometheus** but Alertmanager stops **notifying**. Verify: no new payload reaches `alert-sink`. This is how you mute noise during planned work without disabling the rule.

> В неинтерактивной оболочке (например, в скрипте или агенте) `exec -it` печатает `Unable to use a TTY - input is not a terminal`, но silence всё равно создаётся (проверено batchC). В обычном терминале этого предупреждения нет.

List and expire it:

```bash
kubectl -n monitoring exec alertmanager-monitoring-kube-prometheus-alertmanager-0 -c alertmanager -- \
  amtool silence query --alertmanager.url=http://localhost:9093
```

Погасить silence раньше срока (проверено batchC): возьмите ID из `silence query` и выполните

```bash
kubectl -n monitoring exec alertmanager-monitoring-kube-prometheus-alertmanager-0 -c alertmanager -- \
  amtool silence expire <ID> --alertmanager.url=http://localhost:9093
```

---

### Cleanup (только если вы останавливаетесь здесь)

> The matcher strategy is set in `kps-values.yaml`, so this section does not revert it. Full teardown order, including reverting the strategy if you keep the stack: [module-teardown.md](module-teardown.md).

```bash
kubectl scale deployment demo-metrics -n demo-monitoring --replicas=1
kubectl delete alertmanagerconfig demo-webhook -n monitoring
kubectl delete deploy,svc alert-sink -n monitoring
```

---

## Done when

- ☐ `alert-sink` logs a `"status": "firing"` payload for `DemoMetricsTargetDown`
- ☐ Restoring the app produces a `"status": "resolved"` payload
- ☐ A silence stops notifications while the alert still fires in Prometheus

---

## What you learned

- Alertmanager, not Prometheus, decides *who gets notified and how*
- `AlertmanagerConfig` is the namespaced, CRD-native way to define routing — but the `OnNamespace` matcher strategy is a classic silent trap
- Silences mute notifications for planned work without touching the alert rule
