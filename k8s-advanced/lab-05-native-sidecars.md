# Lab 05 — Native Sidecar Containers (init + restartPolicy: Always)

**Цель:** использовать **нативные sidecar-контейнеры** (initContainer с
`restartPolicy: Always`, GA с Kubernetes 1.33) вместо «обычного» second-container
паттерна — с корректным порядком старта и завершения.

**Время:** 20 минут
**Prerequisites:** Lab 04 (multi-container Pod).

> «Новый» способ K8s (как in-place resize в Lab 31). Требует кластер 1.33+
> (здесь v1.35).

---

## BLUF — чем плох «старый» sidecar и что чинит нативный

**Старый способ** (Lab 04): sidecar — просто второй контейнер в `containers[]`.
Проблемы:
- **нет гарантии порядка** — app-контейнер может стартовать раньше, чем sidecar
  (прокси/агент) готов → первые запросы падают;
- **при завершении** Pod-а sidecar и app получают SIGTERM одновременно → app
  может потерять сеть/логи (прокси уже умер);
- в **Job** sidecar (`sleep`-loop) **никогда не завершится** → Job висит вечно.

**Нативный sidecar** = `initContainers[]` с `restartPolicy: Always`:
- стартует и становится готов **до** app-контейнеров (порядок init-ов);
- **не завершается** (в отличие от обычного init) — живёт рядом с app;
- при завершении Pod-а — останавливается **после** app-контейнеров;
- в Job — **не блокирует** завершение Job-а.

---

## Шаг 1. Нативный sidecar

`sidecar.yaml`:
```yaml
apiVersion: v1
kind: Pod
metadata: {name: sidecar-demo}
spec:
  initContainers:
  - name: log-shipper                 # ← НАТИВНЫЙ sidecar
    image: busybox:1.37
    restartPolicy: Always             # ← ключевое отличие от обычного init
    command: ["/bin/sh","-c","while true; do echo shipping logs...; sleep 5; done"]
  - name: real-init                   # обычный init — ДОЛЖЕН завершиться
    image: busybox:1.37
    command: ["/bin/sh","-c","echo init done"]
  containers:
  - name: app
    image: nginx:1.27-alpine
```

```bash
kubectl apply -f sidecar.yaml
kubectl wait --for=condition=ready pod/sidecar-demo --timeout=60s

kubectl get pod sidecar-demo
# READY 2/2   STATUS Running   ← работающий sidecar учитывается в готовности
```

Порядок старта (по timestamp-ам):
```bash
kubectl get pod sidecar-demo -o jsonpath='{range .status.initContainerStatuses[*]}{.name}: started={.started}{"\n"}{end}'
# log-shipper: started=true       ← sidecar жив (running)
# real-init:   started=false      ← обычный init: terminated/Completed

kubectl get pod sidecar-demo \
  -o jsonpath='sidecar={.status.initContainerStatuses[0].state.running.startedAt}{"\n"}app={.status.containerStatuses[0].state.running.startedAt}{"\n"}'
# sidecar=...:19Z   ← запустился РАНЬШЕ
# app=...:21Z       ← app стартовал ПОСЛЕ готовности sidecar-а
```

> Порядок гарантирован: `log-shipper` (sidecar) → `real-init` (обычный init,
> отработал и вышел) → `app`. App не стартует, пока нативный sidecar не запущен.

```bash
kubectl delete pod sidecar-demo --now
```

---

## Шаг 2. Старый vs нативный — сравнение

```yaml
# СТАРЫЙ способ (Lab 04): порядок и завершение НЕ гарантированы
spec:
  containers:
  - {name: app, image: nginx:1.27-alpine}
  - {name: proxy, image: envoyproxy/envoy}     # может стартовать после app

# НАТИВНЫЙ: proxy готов до app, останавливается после app
spec:
  initContainers:
  - name: proxy
    image: envoyproxy/envoy
    restartPolicy: Always
  containers:
  - {name: app, image: nginx:1.27-alpine}
```

| Аспект | Старый (2-й container) | Нативный (init + Always) |
|---|---|---|
| Порядок старта | не гарантирован | sidecar **раньше** app |
| Завершение | одновременно с app | **после** app |
| Поддержка probes | да | да (свои probes у sidecar) |
| Работа в **Job** | Job висит вечно | Job **завершается** корректно |

---

## Шаг 3. Нативный sidecar в Job (главный выигрыш)

Старая боль: положить логосборщик-`sleep`-loop вторым контейнером в Job —
Job никогда не «завершится» (sidecar не выходит). Нативный sidecar это решает:

```yaml
apiVersion: batch/v1
kind: Job
metadata: {name: job-with-sidecar}
spec:
  template:
    spec:
      restartPolicy: Never
      initContainers:
      - name: metrics-pusher           # нативный sidecar
        image: busybox:1.37
        restartPolicy: Always
        command: ["/bin/sh","-c","while true; do echo push metrics; sleep 2; done"]
      containers:
      - name: worker
        image: busybox:1.37
        command: ["/bin/sh","-c","echo working; sleep 10; echo done"]
```

```bash
kubectl apply -f - <<'EOF'
# (манифест выше)
EOF
kubectl wait --for=condition=complete job/job-with-sidecar --timeout=60s
kubectl get job job-with-sidecar
# COMPLETIONS 1/1   ← Job ЗАВЕРШИЛСЯ: K8s сам остановил sidecar после worker-а
kubectl delete job job-with-sidecar --now
```

> Когда основной контейнер (`worker`) завершается, Kubernetes **сам** посылает
> SIGTERM нативным sidecar-ам и даёт Job-у завершиться. Со старым подходом
> (`sleep`-loop во втором `containers[]`) Job висел бы вечно.

---

## Cleanup

```bash
kubectl delete pod sidecar-demo --ignore-not-found --now
kubectl delete job job-with-sidecar --ignore-not-found --now
```

---

## Что вы узнали

- **Нативный sidecar** = `initContainers[]` + `restartPolicy: Always`
  (GA в Kubernetes 1.33).
- Гарантии: стартует **до** app-контейнеров, **не завершается**, тушится
  **после** app при остановке Pod-а.
- Чинит 3 беды старого подхода: порядок старта, порядок завершения, зависание
  Job-ов с sidecar-ами.
- У нативного sidecar свои probes; работающий sidecar учитывается в `READY`.
- Use cases: service-mesh прокси (Envoy/Istio), сборщики логов/метрик, агенты —
  всё, что должно подняться раньше app и пережить его завершение упорядоченно.
