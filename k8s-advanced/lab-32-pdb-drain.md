# Lab 32 — PodDisruptionBudget + node drain (Day-2 ops)

> Имена нод берутся из кластера (имя кластера выбрали вы в module 02 Lab 01): `WORKER=$(kubectl get nodes -l '!node-role.kubernetes.io/control-plane' -o jsonpath='{.items[0].metadata.name}')`.

**Цель:** защитить доступность приложения при **добровольных** прерываниях
(обслуживание ноды) через PodDisruptionBudget, и безопасно выводить ноду из
эксплуатации командами `cordon` / `drain` / `uncordon`.

**Время:** 25 минут
**Prerequisites:** Lab 08, Lab 27 (anti-affinity — полезно).

> Требуется multi-node кластер из Lab 01. Это операционная (Day-2) лаба:
> что делает SRE перед апгрейдом/заменой ноды.

---

## BLUF — два вида прерываний

| Тип | Примеры | Защита |
|---|---|---|
| **Involuntary** (недобровольные) | падение ноды, OOM, железо | репликация + anti-affinity (Lab 27) |
| **Voluntary** (добровольные) | `kubectl drain`, апгрейд, autoscaler убирает ноду | **PodDisruptionBudget** |

PDB говорит: «при добровольном выселении держи минимум N (или максимум M
недоступных) Pod-ов этого приложения». `kubectl drain` **уважает** PDB — он не
выселит Pod, если это нарушит бюджет, и будет ждать.

---

## Шаг 1. HA-приложение + PDB

`ha-web.yaml`:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata: {name: ha-web}
spec:
  replicas: 3
  selector: {matchLabels: {app: ha-web}}
  template:
    metadata: {labels: {app: ha-web}}
    spec:
      affinity:                          # по одному Pod-у на ноду (как Lab 27)
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
          - labelSelector:
              matchExpressions: [{key: app, operator: In, values: [ha-web]}]
            topologyKey: kubernetes.io/hostname
      containers:
      - name: web
        image: nginx:1.27-alpine
        resources: {requests: {cpu: 20m, memory: 32Mi}}
---
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata: {name: ha-web}
spec:
  minAvailable: 3                        # все 3 обязаны быть доступны
  selector: {matchLabels: {app: ha-web}}
```

```bash
kubectl apply -f ha-web.yaml
kubectl rollout status deploy/ha-web --timeout=90s

kubectl get pods -l app=ha-web -o wide --no-headers | awk '{print $1, $7}'
# по одному Pod-у на <cluster>-worker / worker2 / worker3

kubectl get pdb ha-web
# MIN AVAILABLE   ALLOWED DISRUPTIONS
#       3                  0              ← 3 живых, минимум 3 → выселять нельзя
```

> `ALLOWED DISRUPTIONS: 0` — это значит «прямо сейчас ни один Pod нельзя выселить
> добровольно». Слишком строгий PDB (`minAvailable` == replicas) полностью
> блокирует drain — частая ошибка.

---

## Шаг 2. cordon — пометить ноду «не планировать сюда»

```bash
kubectl cordon $WORKER
kubectl get node $WORKER --no-headers | awk '{print $1, $2}'
# <cluster>-worker  Ready,SchedulingDisabled
```

> `cordon` только запрещает **новые** Pod-ы на ноде. Существующие остаются
> работать — это шаг 1 безопасного вывода ноды.

---

## Шаг 3. drain под строгим PDB → выселение блокируется

```bash
kubectl drain $WORKER --ignore-daemonsets --delete-emptydir-data --timeout=25s
# evicting pod default/ha-web-...
# error when evicting pod ... (will retry after 5s):
#   Cannot evict pod as it would violate the pod's disruption budget.
# ... (повторяет и упирается в таймаут)
```

✅ PDB сработал: выселение `ha-web` Pod-а нарушило бы `minAvailable: 3`, поэтому
drain **не смог** его убрать. Так PDB защищает доступность во время обслуживания.

- `--ignore-daemonsets` — обязателен: DaemonSet-Pod-ы (kindnet, kube-proxy)
  нельзя выселить, их пропускаем.
- `--delete-emptydir-data` — разрешить удаление Pod-ов с emptyDir (данные в нём
  потеряются — это ожидаемо).

---

## Шаг 4. Ослабляем PDB → drain проходит

```bash
kubectl patch pdb ha-web --type=merge -p '{"spec":{"minAvailable":2}}'
kubectl get pdb ha-web
# MIN AVAILABLE   ALLOWED DISRUPTIONS
#       2                  1              ← теперь можно выселить 1

kubectl drain $WORKER --ignore-daemonsets --delete-emptydir-data --timeout=60s
# pod/ha-web-... evicted
# node/<cluster>-worker drained          ← успех

kubectl get pods -l app=ha-web -o wide --no-headers | awk '{print $1, $3, $7}'
# два Pod-а Running на worker2/worker3
# третий — Pending <none>
```

> 🧠 Почему 3-й Pod завис в **Pending**? Anti-affinity требует «один на ноду», а
> единственная свободная нода (`<cluster>-worker`) **cordoned** — туда планировать
> нельзя. Replica не разместится, пока нода под cordon. Это нормальное поведение,
> а не баг.

---

## Шаг 5. uncordon — вернуть ноду в строй

```bash
kubectl uncordon $WORKER
kubectl wait --for=condition=ready pod -l app=ha-web --timeout=60s

kubectl get pods -l app=ha-web -o wide --no-headers | awk '{print $1, $3, $7}'
# все 3 Running, снова по одному на каждый worker

kubectl get node $WORKER --no-headers | awk '{print $1, $2}'
# <cluster>-worker  Ready                  ← SchedulingDisabled снят
```

**Полный цикл обслуживания ноды:** `cordon` → `drain` → (чините/апгрейдите
ноду) → `uncordon`.

---

## Шаг 6. `maxUnavailable` вместо `minAvailable`

PDB можно задавать двумя способами (только один за раз):
```yaml
spec:
  maxUnavailable: 1        # «не больше 1 недоступного» — удобно при автоскейле
  # или
  minAvailable: 80%        # можно в процентах от числа реплик
```

| Запись | Когда удобно |
|---|---|
| `minAvailable: N` / `%` | гарантировать минимум живых (HA-сервисы) |
| `maxUnavailable: N` / `%` | ограничить размах одновременных выселений (большие Deployment-ы) |

---

## 👀 Наблюдение и диагностика

**Смотреть выселение/восстановление вживую** (в отдельном окне во время drain):
```bash
watch -n2 'kubectl get pods -l app=ha-web -o wide; echo; kubectl get pdb ha-web'
# видно: ALLOWED DISRUPTIONS, какие Pod-ы Terminating/Pending, переезд на другие ноды
kubectl events --watch | grep -iE 'evict|disrupt|drain'   # поток событий
```
См. [appendix-accessing-uis.md](appendix-accessing-uis.md) (k9s удобен: `:pods` + `:events`).

✅ **Checkpoint:** при строгом PDB `ALLOWED DISRUPTIONS = 0` и `drain`
**блокируется** (`Cannot evict pod ... disruption budget`); после ослабления до
`minAvailable: 2` drain проходит, один Pod остаётся `Pending` до `uncordon`.

🔧 **Troubleshooting:**
- `drain` висит бесконечно → PDB слишком строгий (понизьте `minAvailable`) или
  забыли `--ignore-daemonsets`.
- `cannot delete Pods with local storage` → добавьте `--delete-emptydir-data`.
- Pod завис в `Pending` после drain → нода ещё `cordoned` (+ anti-affinity);
  верните её в строй `kubectl uncordon <node>`.

---

## Cleanup

```bash
kubectl delete deploy ha-web --now
kubectl delete pdb ha-web
kubectl uncordon $WORKER 2>/dev/null   # на всякий случай
```

---

## Что вы узнали

- 2 типа прерываний: **involuntary** (репликация спасает) и **voluntary**
  (защищает **PDB**).
- **PodDisruptionBudget** = `minAvailable` или `maxUnavailable`; `kubectl drain`
  его уважает и ждёт, не нарушая бюджет.
- Слишком строгий PDB (`minAvailable` == replicas → ALLOWED DISRUPTIONS=0)
  **блокирует** обслуживание — баланс между HA и operability.
- Цикл вывода ноды: **`cordon`** (не планировать) → **`drain`** (выселить,
  `--ignore-daemonsets`) → ремонт → **`uncordon`**.
- Anti-affinity + cordoned нода может оставить replica в `Pending` — это
  ожидаемо, пока нода не вернётся.
- Это фундамент для безопасных **апгрейдов кластера** и работы Cluster
  Autoscaler-а.
