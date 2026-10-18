# Lab 14 — Health Probes: liveness, readiness, startup

**Цель:** настроить liveness / readiness / startup пробы и **на практике увидеть
разницу**: liveness рестартит контейнер, readiness убирает Pod из endpoints
Service-а, startup защищает медленный старт.

**Время:** 25 минут
**Prerequisites:** Lab 08 (Deployment), Lab 10 (Service).

> 🧭 Логически эта тема идёт сразу после Deployment/Service — пробы нужны
> **любому** prod-воркложу.

---

## BLUF — три пробы, три разных эффекта

| Проба | Вопрос | Провал → что делает kubelet |
|---|---|---|
| **liveness** | «контейнер жив или завис?» | **рестартит** контейнер |
| **readiness** | «готов принимать трафик?» | **убирает** Pod из endpoints Service (НЕ рестартит) |
| **startup** | «приложение ещё стартует?» | пока не пройдёт — **отключает** liveness/readiness (защита медленного старта) |

Частая ошибка: liveness на медленную проверку → kubelet рестартит здоровый, но
занятый Pod (cascade-рестарты). Правило: liveness — про «завис», readiness — про
«временно занят/не готов».

---

## Шаг 1. Liveness — рестарт зависшего контейнера

`liveness.yaml`:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: liveness-demo
spec:
  containers:
  - name: app
    image: busybox:1.37
    args: ["/bin/sh","-c","touch /tmp/healthy; sleep 3600"]
    livenessProbe:
      exec:
        command: ["cat","/tmp/healthy"]   # есть файл = жив
      initialDelaySeconds: 3
      periodSeconds: 3
      failureThreshold: 1
```

```bash
kubectl apply -f liveness.yaml
kubectl wait --for=condition=ready pod/liveness-demo --timeout=60s
kubectl get pod liveness-demo -o jsonpath='restarts={.status.containerStatuses[0].restartCount}{"\n"}'
# restarts=0

# Ломаем пробу — удаляем файл
kubectl exec liveness-demo -- rm -f /tmp/healthy

# Через ~3-6 сек kubelet убъёт и перезапустит контейнер
kubectl get pod liveness-demo -w        # RESTARTS станет 1, потом Running
```

Доказательства:
```bash
kubectl get pod liveness-demo -o jsonpath='restarts={.status.containerStatuses[0].restartCount}{"\n"}'
# restarts=1

kubectl describe pod liveness-demo | grep -A1 -i 'liveness probe failed\|will be restarted'
# Liveness probe failed: cat: can't open '/tmp/healthy': No such file or directory
# Container app failed liveness probe, will be restarted
```

> ⚠️ Новый контейнер опять выполнит `touch /tmp/healthy` → снова станет healthy.
> Так и работает self-healing: рестарт возвращает контейнер в рабочее состояние.

```bash
kubectl delete pod liveness-demo --now
```

---

## Шаг 2. Readiness — управление трафиком (НЕ рестарт)

`readiness.yaml`:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ready-demo
spec:
  replicas: 2
  selector: {matchLabels: {app: ready-demo}}
  template:
    metadata: {labels: {app: ready-demo}}
    spec:
      containers:
      - name: app
        image: nginx:1.27-alpine
        readinessProbe:
          exec:
            command: ["cat","/tmp/ready"]
          initialDelaySeconds: 2
          periodSeconds: 2
        lifecycle:
          postStart:
            exec:
              command: ["/bin/sh","-c","touch /tmp/ready"]   # старт = готов
---
apiVersion: v1
kind: Service
metadata: {name: ready-demo}
spec:
  selector: {app: ready-demo}
  ports: [{port: 80, targetPort: 80}]
```

```bash
kubectl apply -f readiness.yaml
kubectl rollout status deploy/ready-demo --timeout=60s

# Оба Pod-а в endpoints (ready=true)
kubectl get endpointslices -l kubernetes.io/service-name=ready-demo \
  -o jsonpath='{range .items[*].endpoints[*]}{.addresses[0]} ready={.conditions.ready}{"\n"}{end}'
# 10.244.x.x ready=true
# 10.244.y.y ready=true
```

Ломаем readiness на ОДНОМ Pod-е:
```bash
P=$(kubectl get pods -l app=ready-demo -o jsonpath='{.items[0].metadata.name}')
kubectl exec $P -- rm -f /tmp/ready

# Через ~2-4 сек этот Pod выпадает из endpoints
kubectl get endpointslices -l kubernetes.io/service-name=ready-demo \
  -o jsonpath='{range .items[*].endpoints[*]}{.addresses[0]} ready={.conditions.ready}{"\n"}{end}'
# 10.244.x.x ready=false   ← трафик сюда больше НЕ идёт
# 10.244.y.y ready=true

kubectl get pods -l app=ready-demo -o custom-columns=\
'NAME:.metadata.name,READY:.status.containerStatuses[0].ready,RESTARTS:.status.containerStatuses[0].restartCount'
# READY=false  RESTARTS=0   ← Pod НЕ перезапущен, просто исключён из балансировки
```

> 🔑 Ключевая разница: liveness **рестартит**, readiness **только убирает из
> трафика**. Pod остаётся жив (`RESTARTS=0`) и вернётся в endpoints, как только
> проба снова станет зелёной (`kubectl exec $P -- touch /tmp/ready`).

```bash
kubectl delete deploy,svc ready-demo --now
```

---

## Шаг 3. Startup — для медленно стартующих приложений

Проблема: приложение поднимается 60+ сек (прогрев кэша, JVM). Если повесить
агрессивный liveness — kubelet убьёт Pod **во время старта**, до готовности.
Решение — `startupProbe`: пока она не пройдёт, liveness/readiness **не считаются**.

`startup.yaml`:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: http-probes
spec:
  containers:
  - name: web
    image: nginx:1.27-alpine
    ports: [{containerPort: 80}]
    startupProbe:                 # ← даём до 30×2=60 сек на старт
      httpGet: {path: /, port: 80}
      failureThreshold: 30
      periodSeconds: 2
    readinessProbe:
      httpGet: {path: /, port: 80}
      periodSeconds: 5
    livenessProbe:                # начнёт работать ТОЛЬКО после startupProbe
      httpGet: {path: /, port: 80}
      periodSeconds: 10
```

```bash
kubectl apply -f startup.yaml
kubectl wait --for=condition=ready pod/http-probes --timeout=90s
kubectl get pod http-probes -o custom-columns='READY:.status.containerStatuses[0].ready,RESTARTS:.status.containerStatuses[0].restartCount'
# READY=true  RESTARTS=0
kubectl delete pod http-probes --now
```

> Формула «бюджета старта»: `failureThreshold × periodSeconds`. Здесь 30×2 = 60
> сек. Лучше дать startupProbe большой бюджет, чем растягивать
> `initialDelaySeconds` у liveness (тот тратит время даже у быстрых рестартов).

---

## Шаг 4. Типы проб

Любая из трёх проб может проверять одним из способов:

```yaml
# 1) httpGet — HTTP 200-399 = успех (самый частый для web)
livenessProbe:
  httpGet: {path: /healthz, port: 8080, scheme: HTTP}

# 2) tcpSocket — TCP-коннект открывается = успех (БД, брокеры)
livenessProbe:
  tcpSocket: {port: 5432}

# 3) exec — команда с exit 0 = успех (любая кастомная логика)
livenessProbe:
  exec: {command: ["sh","-c","pg_isready -U postgres"]}

# 4) grpc — нативная gRPC health-проба (GA с Kubernetes 1.27)
livenessProbe:
  grpc: {port: 9000}
```

Общие тюнинги (для всех типов):
| Поле | Смысл |
|---|---|
| `initialDelaySeconds` | пауза перед первой пробой |
| `periodSeconds` | как часто проверять |
| `timeoutSeconds` | таймаут одной пробы |
| `failureThreshold` | сколько подряд провалов = «не ок» |
| `successThreshold` | сколько успехов для возврата (readiness, обычно 1) |

---

## Cleanup

```bash
kubectl delete pod liveness-demo http-probes --ignore-not-found --now
kubectl delete deploy,svc ready-demo --ignore-not-found --now
```

---

## Что вы узнали

- **liveness** — «завис?» → kubelet **рестартит** контейнер (self-healing).
- **readiness** — «готов к трафику?» → Pod добавляется/убирается из endpoints
  Service (`RESTARTS=0`, не перезапускается).
- **startup** — «ещё стартует?» → отключает liveness/readiness до первого успеха;
  спасает медленные приложения от ложных рестартов.
- 4 механизма проверки: `httpGet`, `tcpSocket`, `exec`, `grpc` (1.27+).
- Тюнинг: `initialDelaySeconds / periodSeconds / timeoutSeconds / failureThreshold / successThreshold`.
- **Анти-паттерн:** тяжёлая/медленная проверка в liveness → каскадные рестарты
  здоровых Pod-ов. Тяжёлое — в readiness, «жив ли процесс» — в liveness.
- Без readiness rolling update Lab 09 отправляет трафик в неготовый Pod →
  ошибки во время деплоя.
