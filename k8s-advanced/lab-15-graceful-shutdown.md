# Lab 15 — Graceful shutdown: preStop hook и terminationGracePeriod

**Цель:** понять полный жизненный цикл завершения Pod-а и добиться **zero-downtime**
выката: `preStop` hook + `terminationGracePeriodSeconds` + readiness, чтобы не
терять запросы при удалении/обновлении Pod-а.

**Время:** 25 минут
**Prerequisites:** Lab 09 (Rolling Update), Lab 14 (Probes).

---

## BLUF — почему рвутся соединения на деплое

При удалении Pod-а параллельно происходят два процесса:
1. Pod убирается из **endpoints** Service (перестаёт получать новый трафик) —
   но это **асинхронно**, занимает доли секунды.
2. Контейнеру сразу шлётся **SIGTERM**.

Если приложение умрёт по SIGTERM **раньше**, чем kube-proxy/балансировщики узнают
об удалении из endpoints, — in-flight запросы оборвутся. Решение: **`preStop`
hook** задерживает SIGTERM на пару секунд, давая endpoints разойтись.

---

## Последовательность завершения (важно знать наизусть)

```
kubectl delete pod
   ├─ Pod → Terminating, убирается из Service endpoints   (асинхронно)
   └─ для каждого контейнера:
        1. выполняется preStop hook         ← блокирует, пока не завершится
        2. контейнеру шлётся SIGTERM
        3. ждём до terminationGracePeriodSeconds
        4. если ещё жив → SIGKILL (принудительно)
```

⏱ Бюджет `terminationGracePeriodSeconds` (default **30с**) считается с момента
delete и включает **и preStop, и время на реакцию на SIGTERM**.

---

## Шаг 1. preStop задерживает завершение

`graceful.yaml`:
```yaml
apiVersion: v1
kind: Pod
metadata: {name: graceful}
spec:
  terminationGracePeriodSeconds: 40
  containers:
  - name: web
    image: nginx:1.27-alpine
    lifecycle:
      preStop:
        exec:
          command: ["/bin/sh","-c","echo preStop-start; sleep 15; echo preStop-done"]
```

```bash
kubectl apply -f graceful.yaml
kubectl wait --for=condition=ready pod/graceful --timeout=60s

# Засекаем, сколько займёт удаление
time kubectl delete pod graceful
# pod "graceful" deleted
# real ~17s   ← ≈15с preStop + пара секунд на SIGTERM/выход nginx
```

> Удаление заняло ~17с, а не мгновенно: `preStop` (`sleep 15`) **блокирует**
> отправку SIGTERM. Эти 15 секунд endpoints успевают разойтись по всем
> kube-proxy / ingress / LB — новый трафик в Pod уже не идёт, а старые запросы
> докручиваются.

---

## Шаг 2. Grace period — это жёсткий потолок

Если `preStop` + завершение **не укладываются** в `terminationGracePeriodSeconds`,
kubelet шлёт **SIGKILL** не дожидаясь:

```yaml
apiVersion: v1
kind: Pod
metadata: {name: grace-exceeded}
spec:
  terminationGracePeriodSeconds: 8       # бюджет всего 8 сек
  containers:
  - name: web
    image: nginx:1.27-alpine
    lifecycle:
      preStop:
        exec:
          command: ["/bin/sh","-c","sleep 30"]   # хочет 30 сек — не дадут
```

```bash
kubectl apply -f - <<'EOF'
# (манифест выше)
EOF
kubectl wait --for=condition=ready pod/grace-exceeded --timeout=60s

time kubectl delete pod grace-exceeded
# real ~8-11s   ← НЕ 30с: на 8-й секунде прилетел SIGKILL, preStop оборван
```

> ⚠️ Вывод: `terminationGracePeriodSeconds` должен быть **больше** суммы
> (preStop + реальное время graceful-выхода приложения). Иначе вы получаете
> жёсткий kill и теряете незавершённые запросы/коннекты.

```bash
kubectl delete pod grace-exceeded --ignore-not-found --now
```

---

## Шаг 3. Zero-downtime rolling update — рецепт

Чтобы выкат (Lab 09) не ронял запросы, нужны **три** вещи вместе:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata: {name: zerodt}
spec:
  replicas: 3
  selector: {matchLabels: {app: zerodt}}
  strategy:
    rollingUpdate: {maxUnavailable: 0, maxSurge: 1}   # 1) сначала подними новый
  template:
    metadata: {labels: {app: zerodt}}
    spec:
      terminationGracePeriodSeconds: 30
      containers:
      - name: web
        image: nginx:1.27-alpine
        readinessProbe:                                # 2) трафик только когда готов
          httpGet: {path: /, port: 80}
          periodSeconds: 2
        lifecycle:
          preStop:                                     # 3) дать endpoints разойтись
            exec:
              command: ["/bin/sh","-c","sleep 5"]
```

```bash
kubectl apply -f - <<'EOF'
# (манифест выше)
EOF
kubectl rollout status deploy/zerodt --timeout=90s
```

Почему именно так:
- `maxUnavailable: 0` — старый Pod удаляется только после готовности нового.
- `readinessProbe` — новый Pod получает трафик лишь когда реально готов.
- `preStop: sleep 5` — старый Pod держится ~5с после выхода из endpoints, чтобы
  балансировщики успели перестать слать в него запросы.

```bash
kubectl delete deploy zerodt --now
```

---

## Шаг 4. Приложение должно слушать SIGTERM

`preStop` помогает с балансировкой, но **сам код** обязан корректно реагировать
на SIGTERM: закрыть listener, дослать ответы, закрыть коннекты к БД.

```
SIGTERM → перестать принимать новые соединения
        → дождаться завершения активных запросов (drain)
        → закрыть пулы (БД, кеши) → exit 0
```

> Типичные грабли: процесс запущен как `sh -c "app"` (PID 1 — это shell, который
> **не** пробрасывает SIGTERM ребёнку) → приложение не получает сигнал и умирает
> по SIGKILL. Запускайте процесс напрямую или используйте init (`tini`),
> пробрасывающий сигналы.

---

## Cleanup

```bash
kubectl delete pod graceful grace-exceeded --ignore-not-found --now
kubectl delete deploy zerodt --ignore-not-found --now
```

---

## Что вы узнали

- Порядок завершения: убрать из endpoints (async) → **preStop** → **SIGTERM** →
  ждать grace period → **SIGKILL**.
- **`preStop`** задерживает SIGTERM → endpoints успевают разойтись → нет обрыва
  in-flight запросов.
- **`terminationGracePeriodSeconds`** (default 30с) — жёсткий потолок на весь
  процесс; должен быть больше (preStop + graceful-выход).
- Zero-downtime деплой = `maxUnavailable:0` + `maxSurge:1` + **readiness** +
  **preStop**.
- Приложение обязано **обрабатывать SIGTERM** (drain); остерегайтесь shell как
  PID 1, не пробрасывающего сигналы.
