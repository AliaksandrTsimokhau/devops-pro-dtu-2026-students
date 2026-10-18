# Lab 30 — HPA: Horizontal Pod Autoscaler

**Цель:** установить metrics-server, создать HPA, нагрузить app, посмотреть autoscaling.

**Время:** 30 минут
**Prerequisites:** Lab 08.

---

## Шаг 1. Установка metrics-server

```bash
kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml

# Для kind / Docker Desktop нужен флаг --kubelet-insecure-tls
kubectl patch -n kube-system deployment metrics-server --type=json \
  -p '[{"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--kubelet-insecure-tls"}]'

# ждём именно завершения rollout, а не отдельных Pod-ов:
kubectl -n kube-system rollout status deployment/metrics-server --timeout=120s
```

> ⚠️ Не используйте `kubectl wait -l k8s-app=metrics-server` сразу после `patch`:
> селектор поймает **и** новый Pod, **и** старый (до патча), который ещё
> терминируется в процессе rollout, и команда упадёт с `error: timed out
> waiting for the condition` — хотя metrics-server на самом деле поднялся.
> `rollout status` ждёт корректно.

Проверка:
```bash
kubectl top nodes
kubectl top pods --all-namespaces
```

---

## Шаг 2. Workload с requests/limits

`app.yaml`:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cpu-burner
spec:
  replicas: 1
  selector:
    matchLabels: {app: cpu-burner}
  template:
    metadata:
      labels: {app: cpu-burner}
    spec:
      containers:
      - name: app
        image: registry.k8s.io/hpa-example
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
          limits:
            cpu: 200m
            memory: 256Mi
        ports:
        - containerPort: 80
---
apiVersion: v1
kind: Service
metadata: {name: cpu-burner}
spec:
  type: ClusterIP
  selector: {app: cpu-burner}
  ports: [{port: 80, targetPort: 80}]
```

```bash
kubectl apply -f app.yaml
kubectl get pods -l app=cpu-burner
```

---

## Шаг 3. HPA

`hpa.yaml`:
```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: cpu-burner
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: cpu-burner
  minReplicas: 1
  maxReplicas: 10
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 50
```

```bash
kubectl apply -f hpa.yaml
kubectl get hpa
# TARGETS: <unknown>/50% → через 30–60 сек: 0%/50%
```

> `<unknown>` в первые ~минуту — это норма: metrics-server ещё не собрал первый
> сэмпл CPU. Если `<unknown>` держится дольше 2 минут — проверьте Шаг 1
> (`kubectl top pods` должен отдавать данные).

---

## Шаг 4. Генерируем нагрузку

В отдельном терминале:
```bash
kubectl run -it --rm load-generator --image=busybox:1.37 -- /bin/sh -c \
  "while true; do wget -q -O- http://cpu-burner.default.svc.cluster.local; done"
```

В исходном терминале наблюдаем:
```bash
watch kubectl get hpa,pods -l app=cpu-burner
```

Через 1–2 минуты HPA увеличит количество реплик (CPU подскочит до ~200%/50% →
4+ Pod-а). Остановите нагрузку (`Ctrl+C` в окне load-generator).

> Scale **down** не моментальный: по умолчанию HPA выжидает
> `stabilizationWindowSeconds: 300` (5 минут) после падения нагрузки, прежде чем
> уменьшать реплики — защита от flapping. Это нормально, не баг. Управляется
> через `behavior` (Шаг 7).

---

## Шаг 5. HPA на memory (практика)

> Все задачи ниже переиспользуют `cpu-burner` из Шагов 2–4. Если удалили —
> пересоздайте (`kubectl apply -f app.yaml`). Применение нового HPA с тем же
> именем `cpu-burner` просто заменяет предыдущий.

В отличие от CPU, для памяти обычно берут `AverageValue` (абсолют), а не проценты.

`hpa-memory.yaml`:
```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata: {name: cpu-burner}
spec:
  scaleTargetRef: {apiVersion: apps/v1, kind: Deployment, name: cpu-burner}
  minReplicas: 1
  maxReplicas: 4
  metrics:
  - type: Resource
    resource:
      name: memory
      target: {type: AverageValue, averageValue: 50Mi}
```

```bash
kubectl apply -f hpa-memory.yaml
sleep 45
kubectl get hpa cpu-burner
# TARGETS: memory: ~39Mi/50Mi   REPLICAS 1   ← порог выше расхода → не масштабирует
```

🧪 **Задача — спровоцировать scale up по памяти.** Опустите цель ниже текущего
расхода и понаблюдайте:
```bash
kubectl patch hpa cpu-burner --type=json \
  -p '[{"op":"replace","path":"/spec/metrics/0/resource/target/averageValue","value":"20Mi"}]'
sleep 60
kubectl get hpa cpu-burner
# memory: ~42Mi/20Mi   REPLICAS 4   ← уехал в МАКСИМУМ
```

> ⚠️ **Гл. урок:** memory-HPA на независимых репликах часто **уезжает в max** —
> каждый новый Pod расходует столько же памяти, средний расход не падает, и HPA
> добавляет ещё. Память (в отличие от RPS/CPU) **не перераспределяется** между
> репликами. Memory-HPA уместен там, где нагрузка реально делится, или как
> «вторая» метрика-предохранитель (Шаг 6), а не основной триггер.

✅ **Checkpoint:** при цели 50Mi реплик 1 (`memory: ~39Mi/50Mi`); при 20Mi —
реплик 4. Вы увидели, как `AverageValue` управляет масштабированием.

---

## Шаг 6. Несколько метрик (практика)

HPA считает желаемое число реплик по **каждой** метрике и берёт **максимум**.
Соберём HPA по CPU **и** памяти и посмотрим, какая метрика «победит».

`hpa-multi.yaml`:
```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata: {name: cpu-burner}
spec:
  scaleTargetRef: {apiVersion: apps/v1, kind: Deployment, name: cpu-burner}
  minReplicas: 1
  maxReplicas: 6
  metrics:
  - type: Resource
    resource: {name: cpu,    target: {type: Utilization, averageUtilization: 50}}
  - type: Resource
    resource: {name: memory, target: {type: AverageValue, averageValue: 300Mi}}  # запас, не триггерит
```

```bash
kubectl apply -f hpa-multi.yaml

# В отдельном окне — CPU-нагрузка:
kubectl run hpa-load --image=busybox:1.37 --restart=Never -- /bin/sh -c \
  "while true; do wget -q -O- http://cpu-burner.default.svc.cluster.local; done"

# Наблюдаем
watch -n2 'kubectl get hpa cpu-burner'
# cpu: 56%/50%, memory: ~52Mi/300Mi   REPLICAS 6
#  ↑ CPU превысил порог → масштабирует он; память в запасе и молчит
```

Кто именно «ведёт» масштабирование — видно в describe:
```bash
kubectl describe hpa cpu-burner | grep -A3 Conditions
# ScalingActive ... "calculate a replica count from cpu resource utilization"
```

🧪 **Задача — пусть «победит» память.** Не снимая нагрузку, опустите memory-цель
ниже расхода (`~52Mi`) и сравните, какая метрика теперь даёт большее число реплик:
```bash
kubectl patch hpa cpu-burner --type=json \
  -p '[{"op":"replace","path":"/spec/metrics/1/resource/target/averageValue","value":"30Mi"}]'
```

✅ **Checkpoint:** под CPU-нагрузкой HPA масштабируется по CPU (память в запасе);
в `describe` видно, что расчёт идёт по `cpu`. Остановите нагрузку:
`kubectl delete pod hpa-load --now`.

> Несколько метрик = «ИЛИ»: растём, если **любая** превышена; берём наибольшее
> предложение. Это страховка (например, и по CPU, и по длине очереди).

---

## Шаг 7. `behavior` — управление агрессивностью (практика)

По умолчанию scale **up** быстрый, а scale **down** ждёт 5 минут
(`stabilizationWindowSeconds: 300`) — защита от flapping (Шаг 4). Через
`behavior` это настраивается. Сделаем scale down **быстрым**, чтобы увидеть его
вживую, а не ждать 5 минут.

`hpa-behavior.yaml`:
```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata: {name: cpu-burner}
spec:
  scaleTargetRef: {apiVersion: apps/v1, kind: Deployment, name: cpu-burner}
  minReplicas: 1
  maxReplicas: 6
  metrics:
  - type: Resource
    resource: {name: cpu, target: {type: Utilization, averageUtilization: 50}}
  behavior:
    scaleUp:
      stabilizationWindowSeconds: 0                       # вверх — моментально
      policies: [{type: Pods, value: 4, periodSeconds: 15}]
      selectPolicy: Max
    scaleDown:
      stabilizationWindowSeconds: 15                      # вниз — через 15с (а не 300)
      policies: [{type: Percent, value: 50, periodSeconds: 15}]
```

```bash
kubectl apply -f hpa-behavior.yaml

# 1) дайте нагрузку (как в Шаге 6) → быстрый рост до max=6
kubectl run hpa-load --image=busybox:1.37 --restart=Never -- /bin/sh -c \
  "while true; do wget -q -O- http://cpu-burner.default.svc.cluster.local; done"

# 2) снимите нагрузку и СМОТРИТЕ быстрый scale down
kubectl delete pod hpa-load --now
watch -n5 'kubectl get hpa,deploy cpu-burner'
# REPLICAS / READY: 6 → 5 → 2 → 1   за ~минуту (а не за 5 минут!)
```

🧪 **Задача — сравните с дефолтом.** Поднимите `scaleDown.stabilizationWindowSeconds`
обратно до `300`, снова нагрузите/снимите нагрузку и убедитесь: реплики держатся
~5 минут, прежде чем упасть. Это и есть «анти-flapping» по умолчанию.

✅ **Checkpoint:** с окном `15` реплики падают за ~минуту (видели `6→5→2→1`);
с `300` — держатся 5 минут. Вы управляете тем, насколько резко HPA реагирует.

> `policies` ограничивают **темп**: `Percent 50%/15s` = «убирать не больше
> половины за 15с»; `Pods 4/15s` = «добавлять до 4 Pod-ов за 15с». `selectPolicy:
> Max` (по умолчанию) берёт самую агрессивную из policies; `Disabled` —
> запретить scale в эту сторону вовсе.

---

## 👀 Наблюдение и диагностика

**Смотреть масштабирование вживую** (в отдельном окне держите во время Шага 4):
```bash
watch -n2 'kubectl get hpa,pods -l app=cpu-burner'
# TARGETS поднимется ~200%/50%, REPLICAS поедет 1 → 4 → …
```
Альтернатива без браузера — **k9s** (`:hpa`, `:pods`); веб-вариант —
**Kubernetes Dashboard**. См. [appendix-accessing-uis.md](appendix-accessing-uis.md).

✅ **Checkpoint:** под нагрузкой `kubectl get hpa` показывает `TARGETS` выше
порога (напр. `200%/50%`), а `REPLICAS` растёт (1 → несколько). После остановки
нагрузки реплики снижаются (с задержкой ~5 мин).

🔧 **Troubleshooting:**
- `TARGETS: <unknown>` дольше ~2 мин → не работает metrics-server (Шаг 1);
  проверьте `kubectl top pods`.
- реплики не растут → у Pod-а нет `resources.requests.cpu` (HPA не может
  посчитать %), либо нагрузка не доходит до сервиса.
- «не уменьшается сразу» → это норма: `stabilizationWindowSeconds: 300` (Шаг 7).

---

## Cleanup

```bash
kubectl delete hpa cpu-burner --ignore-not-found
kubectl delete pod hpa-load load-generator --ignore-not-found --now   # из Шагов 4,6,7
kubectl delete -f app.yaml --ignore-not-found
```

---

## Что вы узнали

- HPA требует **metrics-server** (или Prometheus Adapter для custom metrics)
- `metrics` — utilization (%) или averageValue (абсолют)
- **Memory-HPA на независимых репликах склонен уезжать в max** — память не
  перераспределяется между Pod-ами (Шаг 5)
- Multiple metrics → берётся **максимум** из посчитанного по каждой (Шаг 6)
- `behavior` (`policies` + `stabilizationWindowSeconds`) — контроль темпа и
  агрессивности scale up/down; дефолтный scale-down ждёт 5 мин (Шаг 7)
- Production: stabilization window предотвращает flapping
- HPA + Cluster Autoscaler = multi-dimensional autoscaling (Pod-ы + nodes)
