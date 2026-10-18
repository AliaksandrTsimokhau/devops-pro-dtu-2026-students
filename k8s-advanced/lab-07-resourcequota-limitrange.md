# Lab 07 — ResourceQuota, LimitRange и QoS-классы

**Цель:** ограничить потребление ресурсов на уровне namespace (ResourceQuota),
задать дефолты и границы на контейнер (LimitRange) и понять, как requests/limits
определяют **QoS-класс** Pod-а (Guaranteed / Burstable / BestEffort).

**Время:** 25 минут
**Prerequisites:** Lab 06 (Namespaces), Lab 08.

> Тема governance / multi-tenancy: как поделить кластер между командами и не дать
> одному воркложу «съесть» всё. Идёт после namespaces;

---

## BLUF — три механизма, разные уровни

| Объект | Уровень | Что делает |
|---|---|---|
| **QoS class** | Pod | вычисляется автоматически из requests/limits; влияет на порядок **вытеснения** под давлением памяти |
| **LimitRange** | namespace → каждый контейнер | дефолтные requests/limits + min/max на контейнер |
| **ResourceQuota** | namespace (суммарно) | потолок на сумму requests/limits и число объектов |

Связка: ResourceQuota **требует**, чтобы у каждого Pod-а были requests/limits, а
LimitRange их **проставляет по умолчанию** — иначе пользователям пришлось бы
указывать ресурсы вручную в каждом манифесте.

---

## Шаг 0. Песочница

```bash
kubectl create ns quota-demo
```

---

## Шаг 1. QoS-классы — как requests/limits определяют судьбу Pod-а

QoS назначается автоматически и его **нельзя задать напрямую**:

| QoS | Условие | Под давлением памяти |
|---|---|---|
| **Guaranteed** | requests == limits для **всех** ресурсов всех контейнеров | вытесняется **последним** |
| **Burstable** | есть хоть какой-то request, но не Guaranteed | вытесняется во вторую очередь |
| **BestEffort** | нет ни requests, ни limits | вытесняется **первым** |

```bash
# Guaranteed: requests == limits
kubectl run g -n quota-demo --image=nginx:1.27-alpine --restart=Never \
  --overrides='{"spec":{"containers":[{"name":"g","image":"nginx:1.27-alpine","resources":{"requests":{"cpu":"100m","memory":"128Mi"},"limits":{"cpu":"100m","memory":"128Mi"}}}]}}'

# Burstable: requests < limits
kubectl run b -n quota-demo --image=nginx:1.27-alpine --restart=Never \
  --overrides='{"spec":{"containers":[{"name":"b","image":"nginx:1.27-alpine","resources":{"requests":{"cpu":"50m","memory":"64Mi"},"limits":{"cpu":"200m","memory":"256Mi"}}}]}}'

# BestEffort: ничего не указано
kubectl run be -n quota-demo --image=nginx:1.27-alpine --restart=Never

kubectl get pods -n quota-demo -o custom-columns='NAME:.metadata.name,QOS:.status.qosClass'
# b    Burstable
# be   BestEffort
# g    Guaranteed

kubectl delete pods -n quota-demo --all --now
```

> 🧠 BestEffort Pod-ы — первые кандидаты на eviction при нехватке памяти на ноде.
> Для prod-воркложов всегда задавайте хотя бы requests (минимум — Burstable).

---

## Шаг 2. LimitRange — дефолты и границы на контейнер

`limitrange.yaml`:
```yaml
apiVersion: v1
kind: LimitRange
metadata: {name: limits}
spec:
  limits:
  - type: Container
    default:        {cpu: 200m, memory: 256Mi}   # limit, если не указан
    defaultRequest: {cpu: 100m, memory: 128Mi}   # request, если не указан
    max:            {cpu: "1",  memory: 1Gi}      # верхняя граница
    min:            {cpu: 10m,  memory: 16Mi}     # нижняя граница
```

```bash
kubectl apply -n quota-demo -f limitrange.yaml

# Pod БЕЗ resources — LimitRange проставит дефолты
kubectl run nodef -n quota-demo --image=nginx:1.27-alpine --restart=Never
kubectl get pod nodef -n quota-demo -o jsonpath='{.spec.containers[0].resources}{"\n"}'
# {"limits":{"cpu":"200m","memory":"256Mi"},"requests":{"cpu":"100m","memory":"128Mi"}}
#  ↑ дефолты впрыснуты, хотя в манифесте ресурсов не было

# Pod, превышающий max → отклонён
kubectl run toobig -n quota-demo --image=nginx:1.27-alpine --restart=Never \
  --overrides='{"spec":{"containers":[{"name":"toobig","image":"nginx:1.27-alpine","resources":{"limits":{"cpu":"2","memory":"128Mi"}}}]}}'
# Error from server (Forbidden): maximum cpu usage per Container is 1, but limit is 2

kubectl delete pods -n quota-demo --all --now
```

> LimitRange решает две задачи: (1) **дефолты** — чтобы Pod-ы без ресурсов всё
> равно учитывались в квоте; (2) **guardrails** — никто не запросит абсурдные
> 64 CPU на один контейнер.

---

## Шаг 3. ResourceQuota — суммарный потолок на namespace

`resourcequota.yaml`:
```yaml
apiVersion: v1
kind: ResourceQuota
metadata: {name: ns-quota}
spec:
  hard:
    requests.cpu: "300m"
    requests.memory: 512Mi
    limits.cpu: "600m"
    limits.memory: 1Gi
    pods: "3"               # можно квотировать и число объектов
```

```bash
kubectl apply -n quota-demo -f resourcequota.yaml

# Создаём 3 Pod-а (каждый получит дефолт 100m request из LimitRange)
for n in p1 p2 p3; do kubectl run $n -n quota-demo --image=nginx:1.27-alpine --restart=Never; done

kubectl get resourcequota ns-quota -n quota-demo
# покажет USED vs HARD; requests.cpu 300m/300m, pods 3/3 — квота исчерпана

# 4-й Pod → отклонён (уперлись и в pods=3, и в requests.cpu)
kubectl run p4 -n quota-demo --image=nginx:1.27-alpine --restart=Never
# Error from server (Forbidden): exceeded quota: ns-quota,
#   requested: pods=1,requests.cpu=100m, used: pods=3,requests.cpu=300m, limited: pods=3,requests.cpu=300m

kubectl describe resourcequota ns-quota -n quota-demo
```

> ⚠️ Важный нюанс: если в namespace есть ResourceQuota на `requests.*`/`limits.*`,
> то **каждый** Pod обязан их указать — иначе создание отклоняется
> (`must specify requests.cpu`). Именно поэтому LimitRange (Шаг 2) обычно ставят
> **вместе** с квотой: он автоматически проставит недостающее.

---

## Шаг 4. Квотирование объектов (бонус)

ResourceQuota умеет ограничивать не только CPU/память, но и количество объектов:
```yaml
spec:
  hard:
    services.loadbalancers: "2"     # не больше 2 LB-сервисов
    services.nodeports: "0"         # запретить NodePort вообще
    persistentvolumeclaims: "5"
    count/deployments.apps: "10"
    secrets: "20"
```

> Полезно для защиты от случайного создания дорогих ресурсов (LoadBalancer-ы в
> облаке — это деньги) и от утечки объектов.

---

## Cleanup

```bash
kubectl delete ns quota-demo
```

---

## Что вы узнали

- **QoS-класс** (Guaranteed / Burstable / BestEffort) вычисляется из
  requests/limits и задаёт **приоритет вытеснения** под давлением памяти.
  BestEffort умирает первым → для prod задавайте хотя бы requests.
- **LimitRange** — per-container: дефолтные requests/limits + min/max guardrails
  в рамках namespace.
- **ResourceQuota** — суммарный потолок namespace на CPU/память **и** на число
  объектов (pods, services, PVC, LoadBalancer-ы…).
- Связка LimitRange + ResourceQuota: квота требует наличия requests/limits,
  LimitRange проставляет их по умолчанию.
- Это основа **multi-tenancy**: делим кластер между командами по namespace-ам,
  каждому — своя квота.
