# Lab — Tracing with Jaeger v2

**Goal:** run Jaeger v2 in Kubernetes with in-memory storage, send traces from a demo app over OTLP, and follow one request across services in the Jaeger UI.

**Time:** 30 minutes  
**Prerequisites:** a Kubernetes cluster (kind is fine), `kubectl`. Optional: the Lab 01 stack, for the Grafana datasource step.

**Maps to the deck:** appendix «Логи (EFK) и трейсы (Jaeger)».

## Версии

```bash
export JAEGER_TAG=2.22.0   # проверено 2026-10-12 (batchC): jaegertracing/jaeger:2.22.0 и jaegertracing/example-hotrod:2.22.0
```

> Образы в манифестах ниже записаны с этим тегом; при смене тега меняйте оба образа. `проверь, если новее: curl -s 'https://hub.docker.com/v2/repositories/jaegertracing/jaeger/tags?page_size=20' | jq -r '.results[].name'`, и выбирайте тег 2.x (Jaeger v2) для обоих образов.
>
> **Storage:** this lab keeps traces in memory. Restarting the pod deletes them. Jaeger v1 images (agent, collector, query as separate images, UDP ports 6831/14268) are not used here.

---

## Step 1. Deploy Jaeger

Save as `jaeger.yaml`. The Service is `ClusterIP` and is reached through `kubectl port-forward`, so the manifest works on kind without a LoadBalancer.

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: observability
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: jaeger
  namespace: observability
spec:
  replicas: 1
  selector:
    matchLabels: { app: jaeger }
  template:
    metadata:
      labels: { app: jaeger }
    spec:
      containers:
        - name: jaeger
          image: jaegertracing/jaeger:2.22.0
          ports:
            - { containerPort: 16686, name: ui }
            - { containerPort: 4317, name: otlp-grpc }
            - { containerPort: 4318, name: otlp-http }
---
apiVersion: v1
kind: Service
metadata:
  name: jaeger
  namespace: observability
spec:
  selector: { app: jaeger }
  ports:
    - { name: ui, port: 16686, targetPort: ui }
    - { name: otlp-grpc, port: 4317, targetPort: otlp-grpc }
    - { name: otlp-http, port: 4318, targetPort: otlp-http }
```

```bash
kubectl apply -f jaeger.yaml
kubectl rollout status deploy/jaeger -n observability --timeout=3m
kubectl logs deploy/jaeger -n observability | head -50
```

> Expected: the pod is Running, and the log shows the OTLP receivers on `4317` and `4318` and the UI on `16686`. В batchC (2026-10-12) лог содержал `No '--config' flags detected, using default All-in-One configuration with memory storage.` и одно безвредное предупреждение о старом формате `service.telemetry.resource`. `проверь` the exact log lines for your image tag: the logs show the active configuration.

## Step 2. Open the UI

```bash
kubectl port-forward svc/jaeger -n observability 16686:16686
```

Open `http://localhost:16686`. The **Service** dropdown is empty until the first trace arrives.

> Jaeger v2 отдаёт API под `/api/v3/...`. Путь `/api/services` (API v1) возвращает `404`, и это ожидаемо для 2.22.0. Список сервисов: `curl -s localhost:16686/api/v3/services` (через port-forward из этого шага).

---

## Step 3. Deploy a demo app (HotROD)

HotROD is Jaeger's demo ride-sharing app. It is instrumented with OpenTelemetry and sends its traces to Jaeger over OTLP/HTTP. Save as `hotrod.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: hotrod
  namespace: observability
spec:
  replicas: 1
  selector:
    matchLabels: { app: hotrod }
  template:
    metadata:
      labels: { app: hotrod }
    spec:
      containers:
        - name: hotrod
          image: jaegertracing/example-hotrod:2.22.0
          args: ["all"]
          env:
            - name: OTEL_EXPORTER_OTLP_ENDPOINT
              value: http://jaeger.observability.svc:4318
          ports:
            - { containerPort: 8080, name: http }
---
apiVersion: v1
kind: Service
metadata:
  name: hotrod
  namespace: observability
spec:
  selector: { app: hotrod }
  ports:
    - { name: http, port: 8080, targetPort: http }
```

```bash
kubectl apply -f hotrod.yaml
kubectl rollout status deploy/hotrod -n observability --timeout=3m
```

> Проверено batchC (2026-10-12): `args: ["all"]` и переменная OTLP работают для 2.22.0; `podman run --help` (`docker run --rm jaegertracing/example-hotrod:2.22.0 --help`) показывает команду `all`. Если traces не пришли, смотрите логи: `kubectl logs deploy/hotrod -n observability`.

Generate traffic. In a second terminal:

```bash
kubectl port-forward svc/hotrod -n observability 8080:8080
```

Open `http://localhost:8080`, pick a customer, and click **Dispatch** several times.

---

## Step 4. Find a trace

In the Jaeger UI (`http://localhost:16686`):

1. Select a service in **Service**. Сервиса с именем `hotrod` в батче нет (проверено batchC): там `frontend, customer, route, driver, redis-manual, mysql, jaeger`. Для этого шага выбирайте `frontend`.
2. Click **Find Traces**.
3. Open one trace. Each row is a span. Find the slowest span and the service it belongs to. В batchC самый медленный span в выборке из 5 трейсов — `frontend GET /dispatch`, около 774 ms.

Discuss:
- Which span is the parent of the others? How does one trace ID connect spans from different services?
- Jaeger keeps every trace in this lab. In production you set sampling, so which requests are kept, and why?
- Where would you add the attribute that lets you search by `customer` in the UI?

---

## Step 5 (optional, известная проблема). Jaeger datasource in Grafana

> **Известная проблема (batchC, 2026-10-12):** с Jaeger 2.22.0 datasource `type: jaeger` в Grafana получает `404 Not Found` (health: `request failed: 404 Not Found`). Grafana обращается к пути API v1 `/api/services`, а Jaeger 2.x его не отдаёт. Поэтому **Save & test** в batchC не прошёл. `проверь` на более новых версиях Grafana и Jaeger; рабочего обхода не проверено. Шаги 1–4 от этого не зависят.

Requires the Lab 01 stack. Add the Jaeger datasource in a separate values file layered on `kps-values.yaml`, so the other settings are kept. Save as `jaeger-ds.yaml`:

```yaml
grafana:
  additionalDataSources:
    - name: Jaeger
      type: jaeger
      access: proxy
      url: http://jaeger.observability.svc:16686
```

```bash
KPS_VER=$(helm get metadata monitoring -n monitoring -o json | jq -r .version)
helm upgrade monitoring prometheus-community/kube-prometheus-stack \
  --namespace monitoring --version $KPS_VER \
  -f kps-values.yaml -f jaeger-ds.yaml
```

> If you also keep the Loki datasource from Lab 08, add `-f grafana-loki-ds.yaml` to the command, so that its layer is still passed. `проверь` that `type: jaeger` is the datasource type your Grafana version uses. В batchC после upgrade **Save & test** не прошёл (см. известную проблему выше).

In Grafana, open **Explore**, select **Jaeger**, and search for the `frontend` service (если datasource заработал).

---

### Cleanup (только если вы останавливаетесь здесь)

> Jaeger is not part of [module-teardown.md](module-teardown.md). This cleanup removes it on its own. If you used Step 5, see the note after the code block: the datasource needs the same handling as the Loki one in Lab 08.

```bash
pkill -f "port-forward svc/jaeger" 2>/dev/null
pkill -f "port-forward svc/hotrod" 2>/dev/null
kubectl delete namespace observability
```

If you used Step 5, the Jaeger datasource is **not** removed by re-applying the release without `jaeger-ds.yaml` (проверено batchC, 2026-10-12): Grafana keeps it as a read-only provisioned entry, the same as Loki in Lab 08. Проверенный способ убрать его — удалить релиз `monitoring` (см. cleanup в [lab-08-loki-logs.md](lab-08-loki-logs.md)). Затем удалите локальный файл:

```bash
rm -f jaeger-ds.yaml
```

---

## Done when

- ☐ `kubectl get pods -n observability` shows `jaeger` and `hotrod` Running
- ☐ The Jaeger UI lists the `frontend` service (and the others: `customer`, `route`, `driver`) after you click **Dispatch**
- ☐ You opened a trace and found the slowest span

---

## What you learned

- Jaeger v2 receives OTLP on `4317` and `4318`; the UI is on `16686`
- A trace is a tree of spans with one trace ID across services
- Traces are for one request's path; metrics show the aggregate, logs show the details
