# Lab 05 — Grafana dashboards as code

**Goal:** ship a Grafana dashboard from a Git-friendly file (not the UI), import a community dashboard, and export your own as JSON.

**Time:** 30 minutes
**Prerequisites:** Lab 01 completed (Grafana running). Lab 02 useful for demo data.

**Maps to the deck:** Act 5 — "dashboard as code"; the same GitOps discipline as `ServiceMonitor`/`PrometheusRule`.

> `kube-prometheus-stack` runs a Grafana **sidecar** that watches for ConfigMaps labelled `grafana_dashboard` and auto-loads them. So a dashboard becomes just another reviewable YAML file.

---

## Step 1. Define a dashboard as a ConfigMap

The sidecar imports any ConfigMap in the namespace carrying the label `grafana_dashboard: "1"`. The dashboard JSON goes in the ConfigMap data:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: dtu-demo-dashboard
  namespace: monitoring
  labels:
    grafana_dashboard: "1"      # <-- the sidecar watches for this label
data:
  dtu-demo.json: |
    {
      "title": "DTU — platform overview",
      "uid": "dtu-demo",
      "schemaVersion": 39,
      "time": { "from": "now-1h", "to": "now" },
      "panels": [
        {
          "type": "timeseries",
          "title": "API server request rate (per code)",
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "gridPos": { "h": 8, "w": 24, "x": 0, "y": 0 },
          "targets": [
            { "refId": "A", "expr": "sum by (code) (rate(apiserver_request_total[5m]))" }
          ]
        },
        {
          "type": "stat",
          "title": "Nodes Ready",
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "gridPos": { "h": 8, "w": 6, "x": 0, "y": 8 },
          "targets": [
            { "refId": "A", "expr": "sum(kube_node_status_condition{condition=\"Ready\",status=\"true\"})" }
          ]
        }
      ]
    }
```

```bash
kubectl apply -f dtu-demo-dashboard.yaml
```

---

## Step 2. Watch the sidecar import it

```bash
kubectl logs -n monitoring deploy/monitoring-grafana -c grafana-sc-dashboard | tail -5
```

> Expected: a log line showing the file was written to `/tmp/dashboards` and Grafana provisioning reloaded. В batchC (2026-10-12) это были строки `Writing /tmp/dashboards/dtu-demo.json` и `Dashboards config reloaded 200 OK`. Then open Grafana → **Dashboards** → **DTU — platform overview**.

If a panel shows *"datasource not found"*, edit the panel and pick your Prometheus datasource — the `uid` differs between installs (a good reason community dashboards use a datasource variable).

---

## Step 3. Import a community dashboard by ID

Grafana.com hosts thousands of dashboards. Import the classic **Node Exporter Full** (`1860`):

- Grafana → **Dashboards → New → Import**
- Enter `1860`, click **Load**
- Select your **Prometheus** datasource, **Import**

> У последней ревизии 1860 нет `__inputs`, поэтому шаг выбора входа `DS_PROMETHEUS` может не появиться: просто выберите свой Prometheus. Импорт через UI в batchC не прогонялся; там импорт сделан через HTTP API (`/api/dashboards/import`). Панели визуально не проверялись.

Explore CPU, memory, disk, and network panels driven by `node-exporter` metrics from Lab 01.

---

## Step 4. Export your dashboard back to code

Any dashboard you build in the UI can be captured as JSON:

- Open a dashboard → **Share → Export → Save to file** (enable *Export for sharing externally* to templatize the datasource)
- Commit that JSON into Git, or wrap it back into a ConfigMap like Step 1
- Если UI-путь не работает, JSON можно забрать через HTTP API: `GET /api/dashboards/uid/<uid>` (в batchC так скачивали дашборд; UI-экспорт не прогонялся)

> This is the round trip: prototype in the UI → export JSON → review in a PR → the sidecar provisions it. No more "someone changed a panel in prod and nobody knows."

---

### Cleanup (только если вы останавливаетесь здесь)

> Full teardown order: [module-teardown.md](module-teardown.md).

```bash
kubectl delete configmap dtu-demo-dashboard -n monitoring
# the 1860 import lives only in Grafana's DB; delete it from the UI if desired
```

---

## Done when

- ☐ "DTU — platform overview" appears in Grafana and both panels render data
- ☐ Node Exporter Full (1860) imported and shows node metrics
- ☐ You exported a dashboard to JSON and understand how to provision it as a ConfigMap

---

## What you learned

- The Grafana sidecar turns a labelled ConfigMap into a provisioned dashboard — dashboards as code
- Community dashboards are reusable via their numeric ID + a datasource pick
- Export → JSON → Git closes the loop on reviewable, versioned dashboards
