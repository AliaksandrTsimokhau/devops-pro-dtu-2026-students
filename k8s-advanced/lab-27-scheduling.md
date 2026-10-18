# Lab 27 — Taints, Tolerations, Topology Spread

> Имя кластера выбрали вы при создании (module 02 Lab 01). Ниже `$CLUSTER` и `$WORKER` берутся из текущего контекста, имена нод в выводах — `<cluster>-…`.

**Цель:** управлять размещением Pod-ов через taints/tolerations и topology spread constraints.

**Время:** 25 минут
**Prerequisites:** module 02, Lab 01 — кластер (multi-node kind), module 03, Lab 08 — Deployment.

> ⚠️ Этой лабе нужен **multi-node** кластер (≥3 worker) — тот, что вы подняли в
> module 02 Lab 01. Шаги 2–8 на single-node не сработают.

---

## Шаг 0. Проверьте multi-node топологию

Кластер из **module 02 Lab 01** уже multi-node (1 control-plane + 3 worker). Убедитесь:
```bash
CLUSTER=$(kubectl config current-context | sed 's/^kind-//')
kubectl get nodes
# <cluster>-control-plane   control-plane
# <cluster>-worker          <none>
# <cluster>-worker2         <none>
# <cluster>-worker3         <none>
```
Видите **3 worker-ноды** → переходите к Шагу 1.

<details>
<summary>Если worker-нод нет (single-node кластер) — развернуть fallback</summary>

`kind` **не умеет добавлять ноды в работающий кластер** — его нужно пересоздать.
⚠️ Это удалит ресурсы предыдущих лаб в этом кластере.

`kind-multinode.yaml`:
```yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: <имя кластера>   # то же имя, что в $CLUSTER
nodes:
- role: control-plane
- role: worker
  labels: {topology.kubernetes.io/zone: zone-a}
- role: worker
  labels: {topology.kubernetes.io/zone: zone-b}
- role: worker
  labels: {topology.kubernetes.io/zone: zone-c}
```
```bash
# в этом окружении kind работает через podman:
# внимание: удалит текущий кластер вместе с ресурсами предыдущих лаб
KIND_EXPERIMENTAL_PROVIDER=podman kind delete cluster --name "$CLUSTER"
KIND_EXPERIMENTAL_PROVIDER=podman kind create cluster --name "$CLUSTER" --config kind-multinode.yaml
kubectl wait --for=condition=Ready nodes --all --timeout=120s
```
</details>

---

## Шаг 1. Inspect nodes

```bash
kubectl get nodes
# control-plane + 3 worker:
# NAME                       ROLES           ...
# <cluster>-control-plane     control-plane
# <cluster>-worker            <none>
# <cluster>-worker2           <none>
# <cluster>-worker3           <none>

kubectl get nodes --show-labels
```

> Важно: в kind у **control-plane стоит taint** `node-role.kubernetes.io/control-plane:NoSchedule`,
> поэтому обычные Pod-ы едут только на 3 worker-ноды. Это пригодится в Шаге 7.

---

## Шаг 2. Taint одну ноду

Выберите worker ноду (не control-plane!):
```bash
WORKER=$(kubectl get nodes -l '!node-role.kubernetes.io/control-plane' -o jsonpath='{.items[0].metadata.name}')
echo "Worker: $WORKER"

# Тейнтим
kubectl taint node $WORKER gpu=true:NoSchedule

# Проверим
kubectl describe node $WORKER | grep Taints
```

---

## Шаг 3. Pod без toleration — не приедет на эту ноду

`pod-normal.yaml`:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: normal-pod
spec:
  containers:
  - name: app
    image: nginx:1.27-alpine
```

```bash
kubectl apply -f pod-normal.yaml
kubectl get pod -o wide
# Должен попасть на ДРУГОЙ узел, не на $WORKER
```

---

## Шаг 4. Pod с toleration — может приехать

`pod-toleration.yaml`:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: gpu-pod
spec:
  tolerations:
  - key: gpu
    operator: Equal
    value: "true"
    effect: NoSchedule
  containers:
  - name: app
    image: nginx:1.27-alpine
```

```bash
kubectl apply -f pod-toleration.yaml
kubectl get pod gpu-pod -o wide
# Может попасть и на $WORKER (но не обязательно)
```

> Toleration ≠ принуждение к ноде. Это только "разрешение".

---

## Шаг 5. Принудительно отправим Pod на GPU-ноду

```bash
# Labelим ноду
kubectl label node $WORKER hardware=gpu
```

`pod-affinity.yaml`:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: gpu-only
spec:
  tolerations:
  - key: gpu
    operator: Equal
    value: "true"
    effect: NoSchedule
  nodeSelector:
    hardware: gpu        # ← ХОЧУ на ноду с этим label
  containers:
  - name: app
    image: nginx:1.27-alpine
```

```bash
kubectl apply -f pod-affinity.yaml
kubectl get pod gpu-only -o wide
# Гарантированно на $WORKER
```

---

## Шаг 6. NoExecute — выселяем Pod-ы

> ⚠️ **NoExecute выселяет все Pod-ы без подходящей toleration с `$WORKER`**, включая Pod-ы из предыдущих шагов и лаб, которые оказались на этой ноде (например, `gpu-only` из Шага 5 — у него нет toleration на `critical`). Сразу после наблюдения выполните Шаг 7: он снимает taint.

```bash
kubectl taint node $WORKER critical=true:NoExecute

# Все Pod-ы без соответствующей toleration будут evicted с $WORKER
kubectl get pods -o wide --watch
```

> Pod-ы, созданные напрямую (без Deployment, например `gpu-pod` и `gpu-only`), после выселения
> **не пересоздаются**: объекты остаются в статусе `Completed` (`0/1`). Это не ошибка. Удалите их,
> когда закончите наблюдение:
> ```bash
> kubectl delete pod gpu-pod gpu-only --ignore-not-found
> ```

---

## Шаг 7. Topology Spread Constraints

Уберём taint:
```bash
kubectl taint node $WORKER gpu=true:NoSchedule-
kubectl taint node $WORKER critical=true:NoExecute-
```

`deploy-spread.yaml`:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: spread-app
spec:
  replicas: 6
  selector:
    matchLabels: {app: spread}
  template:
    metadata:
      labels: {app: spread}
    spec:
      topologySpreadConstraints:
      - maxSkew: 1
        topologyKey: kubernetes.io/hostname    # spread по нодам
        whenUnsatisfiable: DoNotSchedule
        nodeTaintsPolicy: Honor                # ← см. примечание ниже
        labelSelector:
          matchLabels: {app: spread}
      containers:
      - name: c
        image: nginx:1.27-alpine
```

```bash
kubectl apply -f deploy-spread.yaml
kubectl get pods -o wide -l app=spread
# Pod-ы должны быть распределены равномерно по нодам
```

Подсчитаем распределение:
```bash
kubectl get pods -l app=spread -o wide --no-headers | awk '{print $7}' | sort | uniq -c
#   2 <cluster>-worker
#   2 <cluster>-worker2
#   2 <cluster>-worker3   ← ровно 2/2/2 по трём worker-нодам
```

> **Зачем `nodeTaintsPolicy: Honor`?** По умолчанию (`Ignore`) scheduler считает
> доменом распределения **все** ноды по `topologyKey`, включая tainted
> control-plane, куда Pod-ы поехать не могут. Получается домен с 0 подов, и чтобы
> не превысить `maxSkew: 1`, на каждый worker влезает максимум 1 под → 3 пода
> зависают в `Pending`. `Honor` исключает ноды, чьи taint-ы Pod не толерейтит
> (control-plane), и остаются только 3 worker-ноды → честные 2/2/2.
> (Поле beta с Kubernetes 1.26, включено по умолчанию; GA в 1.33 — проверь по release notes Kubernetes.)

---

## Шаг 8. Pod Anti-Affinity (старая форма)

`anti-affinity.yaml`:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ha-app
spec:
  replicas: 3
  selector:
    matchLabels: {app: ha-app}
  template:
    metadata:
      labels: {app: ha-app}
    spec:
      affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
          - labelSelector:
              matchExpressions:
              - key: app
                operator: In
                values: [ha-app]
            topologyKey: kubernetes.io/hostname
      containers:
      - name: c
        image: nginx:1.27-alpine
```

```bash
kubectl apply -f anti-affinity.yaml
kubectl get pods -l app=ha-app -o wide
# Гарантированно по одной реплике на ноду
```

---

## 👀 Наблюдение и диагностика

**Смотреть распределение Pod-ов вживую** (во время Шагов 7–8):
```bash
watch -n2 'kubectl get pods -l app=spread -o wide --no-headers | awk "{print \$7}" | sort | uniq -c'
# на macOS команды watch может не быть: brew install watch (или наблюдайте через kubectl get pods -o wide --watch)
# должно сойтись к   2 worker / 2 worker2 / 2 worker3
```
Удобно также в **k9s** (`:pods`, колонка NODE) — см.
[appendix-accessing-uis.md](appendix-accessing-uis.md).

✅ **Checkpoint:** topology spread даёт **2/2/2** по трём worker-нодам; pod
anti-affinity (Шаг 8) — ровно **по одной реплике на ноду**, все `Running`.

🔧 **Troubleshooting:**
- 3 пода в `Pending` и раскладка 1/1/1 → забыли `nodeTaintsPolicy: Honor`
  (Шаг 7): tainted control-plane ломает расчёт skew.
- все Pod-ы на одной ноде → неверный `topologyKey` или нет нужных label-ов на
  нодах (`kubectl get nodes --show-labels`).
- ничего не раскладывается / всё `Pending` → кластер single-node; нужен
  multi-node из Lab 01.

---

## Cleanup

```bash
WORKER=$(kubectl get nodes -l '!node-role.kubernetes.io/control-plane' -o jsonpath='{.items[0].metadata.name}')
kubectl delete --ignore-not-found -f anti-affinity.yaml -f deploy-spread.yaml -f pod-affinity.yaml -f pod-toleration.yaml -f pod-normal.yaml
# bare Pod-ы, выселенные NoExecute (Шаг 6), остаются как Completed
kubectl delete pod gpu-pod gpu-only --ignore-not-found

# Снять taints (если ещё есть)
kubectl taint node $WORKER gpu=true:NoSchedule- 2>/dev/null
kubectl taint node $WORKER critical=true:NoExecute- 2>/dev/null

# Снять labels
kubectl label node $WORKER hardware- 2>/dev/null
```

---

## Что вы узнали

- **Taint** на ноде = "не размещай Pod-ы, если они не tolerate"
- **Toleration** в Pod = "я разрешаю себя размещать на ноду с этим taint"
- Toleration ≠ принуждение; для принуждения нужен `nodeSelector` / `nodeAffinity`
- 3 эффекта taint: `NoSchedule` / `PreferNoSchedule` / `NoExecute`
- **Topology Spread Constraints** — распределение Pod-ов по failure-domains (zones, hosts)
- `nodeTaintsPolicy: Honor` — чтобы tainted control-plane не ломал расчёт skew
- **Pod Anti-Affinity** — старая форма "не размещать рядом с такими же"
- Use cases: GPU/Spot nodes, dedicated tenancy, HA для production app-ов
