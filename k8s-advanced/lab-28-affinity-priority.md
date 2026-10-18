# Lab 28 — Node/Pod Affinity, PriorityClass и Preemption

**Цель:** управлять размещением через `nodeAffinity` / `podAffinity` /
`podAntiAffinity` (тоньше, чем `nodeSelector`) и задавать приоритет Pod-ов через
`PriorityClass` — вплоть до **вытеснения** (preemption) менее важных Pod-ов.

**Время:** 30 минут
**Prerequisites:** Lab 27 (taints/tolerations, topology spread).

> Расширение Lab 27. Требуется multi-node кластер из Lab 01 (с метками зон
> `zone-a/b/c`).

---

## BLUF — affinity vs nodeSelector vs priority

- **`nodeSelector`** (Lab 27, 23) — простое «нода ДОЛЖНА иметь label X».
- **`nodeAffinity`** — то же, но с `required` (жёстко) **и** `preferred`
  (мягко, с весами) + операторы (`In`, `NotIn`, `Gt`, `Exists`…).
- **`podAffinity` / `podAntiAffinity`** — размещение относительно **других
  Pod-ов** (рядом / не рядом), по `topologyKey`.
- **`PriorityClass`** — числовой вес Pod-а. При нехватке ресурсов scheduler
  **вытесняет** (preempt) Pod-ы с меньшим приоритетом ради более важных.

---

## Шаг 1. nodeAffinity — required + preferred

`node-affinity.yaml`:
```yaml
apiVersion: v1
kind: Pod
metadata: {name: affinity-pod}
spec:
  affinity:
    nodeAffinity:
      # ЖЁСТКО: только зоны a или b
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
        - matchExpressions:
          - key: topology.kubernetes.io/zone
            operator: In
            values: [zone-a, zone-b]
      # МЯГКО: предпочитаем зону a (но не обязательно)
      preferredDuringSchedulingIgnoredDuringExecution:
      - weight: 100
        preference:
          matchExpressions:
          - key: topology.kubernetes.io/zone
            operator: In
            values: [zone-a]
  containers:
  - name: c
    image: nginx:1.27-alpine
```

```bash
kubectl apply -f node-affinity.yaml
kubectl get pod affinity-pod -o wide --no-headers | awk '{print $1,$3,$7}'
# поедет на zone-a (preferred), при недоступности — zone-b; на zone-c — НИКОГДА
kubectl delete pod affinity-pod --now
```

> `requiredDuringScheduling` = условие планирования (жёсткое). `preferred` =
> подсказка с весом. `IgnoredDuringExecution` означает: уже запущенный Pod не
> выселяется, если label ноды позже изменится.

---

## Шаг 2. podAffinity / podAntiAffinity

Размещение относительно **других Pod-ов** (например: кэш рядом с приложением;
реплики БД — на разных нодах):

```yaml
# Co-locate: запустить РЯДОМ с Pod-ами app=web (на той же ноде)
affinity:
  podAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
    - labelSelector: {matchLabels: {app: web}}
      topologyKey: kubernetes.io/hostname

# Spread: НЕ запускать рядом с такими же (по одному на ноду) — как в Lab 27
affinity:
  podAntiAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
    - labelSelector: {matchLabels: {app: web}}
      topologyKey: kubernetes.io/hostname
```

| Конструкция | Смысл |
|---|---|
| `podAffinity` | «хочу рядом с Pod-ами с label X» (latency, shared cache) |
| `podAntiAffinity` | «не рядом с такими же» (HA, отказоустойчивость) |
| `topologyKey` | домен «рядом»: `hostname` (нода), `zone` (зона) |

> ⚠️ `podAffinity` с `requiredDuringScheduling` дорог для scheduler-а на больших
> кластерах. Для простого «размазать по нодам/зонам» предпочитайте
> **topologySpreadConstraints** (Lab 27) — они легче.

---

## Шаг 3. PriorityClass

```yaml
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata: {name: low-prio}
value: 100
globalDefault: false
description: "Low priority — preemptible"
---
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata: {name: high-prio}
value: 1000000
globalDefault: false
description: "High priority — can preempt"
```

```bash
kubectl apply -f priorityclasses.yaml
kubectl get priorityclass | grep -E 'low-prio|high-prio|system-'
# есть и системные: system-cluster-critical / system-node-critical
```

> `globalDefault: true` (только у одного PC) задаёт приоритет Pod-ам без
> явного `priorityClassName`. Системные компоненты используют
> `system-node-critical` (2000001000) — поэтому их почти никогда не вытесняют.

---

## Шаг 4. Preemption — высокий приоритет вытесняет низкий

Сценарий: нода почти заполнена низкоприоритетным Pod-ом, приходит важный — и
ему **нет места**. Scheduler вытесняет низкоприоритетный.

```bash
# 4.1 Жертва: low-prio, прибита к zone-a (1 нода), просит ~5 из 6 CPU
kubectl apply -f - <<'EOF'
apiVersion: v1
kind: Pod
metadata: {name: victim}
spec:
  priorityClassName: low-prio
  nodeSelector: {topology.kubernetes.io/zone: zone-a}
  containers:
  - {name: c, image: nginx:1.27-alpine, resources: {requests: {cpu: "5"}}}
EOF
kubectl wait --for=condition=ready pod/victim --timeout=60s
kubectl get pod victim -o wide --no-headers | awk '{print $1,$3,$7}'   # Running на zone-a

# 4.2 Важный: high-prio, та же нода, тоже ~5 CPU → не влезает → вытесняет victim
kubectl apply -f - <<'EOF'
apiVersion: v1
kind: Pod
metadata: {name: important}
spec:
  priorityClassName: high-prio
  nodeSelector: {topology.kubernetes.io/zone: zone-a}
  containers:
  - {name: c, image: nginx:1.27-alpine, resources: {requests: {cpu: "5"}}}
EOF

# Наблюдаем: victim → Terminating/исчезает, important → Running
kubectl get pods -w
```

Доказательства:
```bash
kubectl get pod important -o wide --no-headers | awk '{print $1,$3,$7}'   # Running
kubectl get pod victim 2>&1                                                # NotFound — вытеснен
kubectl get events | grep -i preempt
# Preempted  pod/victim  Preempted by pod <uid> on node <cluster>-worker
```

> 🧠 Preemption ≠ просто «не запланировать». Scheduler **активно убивает**
> низкоприоритетный Pod, освобождая место. Жертва выбирается так, чтобы
> минимально нарушить PDB и приоритеты. Это причина, по которой батч/best-effort
> воркложи дают низкий приоритет, а критичные сервисы — высокий.

---

## Cleanup

```bash
kubectl delete pod victim important affinity-pod --ignore-not-found --now
kubectl delete priorityclass low-prio high-prio --ignore-not-found
```

---

## Что вы узнали

- **`nodeAffinity`** — `required` (жёстко) + `preferred` (мягко, с весами),
  богаче `nodeSelector`.
- **`podAffinity` / `podAntiAffinity`** — размещение относительно других Pod-ов
  по `topologyKey`; для простого spread лучше topologySpreadConstraints (Lab 27).
- `IgnoredDuringExecution` — правило проверяется при планировании, не выселяет
  уже запущенный Pod при изменении label-ов.
- **`PriorityClass`** задаёт вес Pod-а; `globalDefault` — для не указавших.
- **Preemption** — высокоприоритетный Pod **вытесняет** низкоприоритетные, если
  иначе ему нет места (с учётом PDB из Lab 32).
- Системные PC (`system-node-critical`) защищают компоненты кластера.
