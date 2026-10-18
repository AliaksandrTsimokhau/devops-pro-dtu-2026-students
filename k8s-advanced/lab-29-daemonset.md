# Lab 29 — DaemonSet: один Pod на каждую ноду

> Имена нод берутся из кластера (имя кластера выбрали вы в module 02 Lab 01): `WORKER=$(kubectl get nodes -l '!node-role.kubernetes.io/control-plane' -o jsonpath='{.items[0].metadata.name}')`.

**Цель:** запустить DaemonSet (по одному Pod-у на ноду), управлять охватом нод
через taints/tolerations и `nodeSelector`, понять стратегии обновления.

**Время:** 20 минут
**Prerequisites:** Lab 08 (Deployment), Lab 27 (taints/tolerations — желательно).

> Требуется multi-node кластер из Lab 01 (1 control-plane + 3 worker) — иначе
> «по одному на каждую ноду» не прочувствовать.

---

## BLUF — зачем DaemonSet

Deployment = N реплик «где угодно». **DaemonSet = ровно один Pod на каждой
(подходящей) ноде**, и автоматически добавляет Pod при добавлении ноды. Это
паттерн для **нодовых агентов**:

- сбор логов (Fluent Bit, Promtail)
- метрики ноды (node-exporter)
- сеть (CNI: kindnet, Calico — сами по себе DaemonSet-ы)
- storage / security агенты

```bash
# Убедитесь, что системные DS уже работают именно так:
kubectl get ds -n kube-system
# kube-proxy и kindnet — по одному Pod-у на КАЖДОЙ ноде (DESIRED = число нод)
```

---

## Шаг 1. Базовый DaemonSet

`daemonset.yaml`:
```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: node-agent
spec:
  selector: {matchLabels: {app: node-agent}}
  template:
    metadata: {labels: {app: node-agent}}
    spec:
      containers:
      - name: agent
        image: busybox:1.37
        args: ["/bin/sh","-c","while true; do sleep 3600; done"]
        resources:
          requests: {cpu: 10m, memory: 16Mi}
```

```bash
kubectl apply -f daemonset.yaml
kubectl rollout status ds/node-agent --timeout=90s

kubectl get ds node-agent
# DESIRED  CURRENT  READY  ...   NODE SELECTOR
#    3        3       3            <none>

kubectl get pods -l app=node-agent -o wide --no-headers | awk '{print $1, $7}'
# node-agent-xxxxx <cluster>-worker
# node-agent-xxxxx <cluster>-worker2
# node-agent-xxxxx <cluster>-worker3
```

> Обратите внимание: **3** Pod-а, не 4. На `<cluster>-control-plane` Pod не поехал
> — там taint `node-role.kubernetes.io/control-plane:NoSchedule`, а у нас нет
> toleration. DaemonSet **не игнорирует** taint-ы (кроме нескольких системных
> вроде `not-ready`/`unreachable`, которые он толерейтит сам).

---

## Шаг 2. Toleration — охватить control-plane

Нодовому агенту (логи, метрики) обычно нужна **каждая** нода, включая
control-plane. Добавим toleration:

```yaml
    spec:
      tolerations:
      - key: node-role.kubernetes.io/control-plane
        operator: Exists
        effect: NoSchedule
      containers:
      - name: agent
        # ... как выше
```

```bash
kubectl patch ds node-agent --type=json -p \
  '[{"op":"add","path":"/spec/template/spec/tolerations","value":[{"key":"node-role.kubernetes.io/control-plane","operator":"Exists","effect":"NoSchedule"}]}]'
kubectl rollout status ds/node-agent --timeout=90s

kubectl get ds node-agent              # DESIRED стало 4
kubectl get pods -l app=node-agent -o wide --no-headers | awk '{print $1, $7}' | sort -k2
# теперь есть Pod и на <cluster>-control-plane
```

> DaemonSet сам пересчитал «подходящие» ноды: было 3 → стало 4. Удалите ноду —
> станет меньше; добавите — DaemonSet сам создаст там Pod.

---

## Шаг 3. nodeSelector — только подмножество нод

Иногда агент нужен не везде, а на нодах с определёнными свойствами (GPU, SSD,
зона). Используем label на ноде + `nodeSelector`:

```bash
# помечаем одну ноду
kubectl label node $WORKER disktype=ssd

kubectl patch ds node-agent --type=merge -p \
  '{"spec":{"template":{"spec":{"nodeSelector":{"disktype":"ssd"}}}}}'
kubectl rollout status ds/node-agent --timeout=90s

kubectl get ds node-agent
# DESIRED  ...  NODE SELECTOR
#    1            disktype=ssd        ← только ноды с этим label

kubectl get pods -l app=node-agent -o wide --no-headers | awk '{print $1, $7}'
# node-agent-xxxxx <cluster>-worker     ← только помеченная нода
```

> `nodeSelector` сужает «подходящие» ноды до тех, у кого есть label. Поставите
> label на другую ноду — DaemonSet тут же развернёт там Pod. Снимете —
> уберёт. (Более гибко — через `affinity.nodeAffinity`.)

---

## Шаг 4. Стратегия обновления

```yaml
spec:
  updateStrategy:
    type: RollingUpdate          # по умолчанию
    rollingUpdate:
      maxUnavailable: 1          # сколько нод обновлять одновременно
      # maxSurge: 0              # (1.22+) можно поднять новый Pod до удаления старого
```

| Стратегия | Поведение |
|---|---|
| `RollingUpdate` (default) | по очереди, не более `maxUnavailable` нод без агента одновременно |
| `OnDelete` | новый Pod создаётся только когда вы **вручную** удалите старый — полный контроль над раскаткой |

```bash
# Посмотреть текущую стратегию
kubectl get ds node-agent -o jsonpath='{.spec.updateStrategy}{"\n"}'

# Триггер обновления (меняем образ) и наблюдаем поэтапную раскатку:
kubectl set image ds/node-agent agent=busybox:1.37 --record 2>/dev/null
kubectl rollout status ds/node-agent
```

> `maxUnavailable: 1` — компромисс: на 1 ноде агент на секунды пропадёт, но
> кластер не остаётся без агентов целиком. Для критичных (CNI) ставят малый
> `maxUnavailable`.

---

## Cleanup

```bash
kubectl delete ds node-agent --now
kubectl label node $WORKER disktype-
```

---

## Что вы узнали

- **DaemonSet** = ровно один Pod на каждой подходящей ноде; авто-добавление при
  добавлении ноды.
- Use cases: логи, метрики ноды, CNI, storage/security агенты (всё это —
  DaemonSet-ы в `kube-system`).
- `DESIRED` у DS = число **подходящих** нод, не фиксированное число.
- DaemonSet **уважает taint-ы** → для control-plane нужен `toleration`.
- `nodeSelector` / `nodeAffinity` → запустить только на части нод.
- `updateStrategy`: `RollingUpdate` (с `maxUnavailable`) или `OnDelete` (ручной
  контроль).
- Отличие от Deployment: не «N реплик где-нибудь», а «по одному на ноду».
