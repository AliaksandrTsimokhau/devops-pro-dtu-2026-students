# Lab 24 — NetworkPolicy: micro-segmentation

> Кластер: имя выбираете вы (его создаёт module 02 Lab 01); ниже `$CLUSTER` берётся из текущего контекста.

**Цель:** включить default-deny в namespace и точечно разрешить frontend → backend → db.

**Время:** 25 минут
**Prerequisites:** module 02, Lab 17 — namespaces.

> ⚠️ NetworkPolicy — это объект API, но *применяет* его CNI. Нужен CNI с
> поддержкой NetworkPolicy: **kindnet** в новых версиях kind (см. Шаг 0), Calico, Cilium.
> Старый kindnet и basic Flannel — НЕ применяют (манифесты
> создаются, но трафик не фильтруется). **Сначала пройдите Шаг 0.**
> Граница версий kindnet — проверь: по тесту из Шага 0.2 (default-deny должен блокировать трафик).
> Calico ниже — только запасной вариант (fallback), если тест 0.2 показал, что policy игнорируется.

---

## Шаг 0. Проверка / включение CNI с поддержкой NetworkPolicy

Не угадывайте по названию CNI — **проверьте enforce эмпирически**. Если CNI не
умеет NetworkPolicy, манифесты применяются без ошибок, но правила молча
игнорируются, и вся лаба будет «не работать» необъяснимо.

### 0.1 Какой CNI установлен

```bash
kubectl get pods -n kube-system | grep -Ei 'kindnet|cilium|calico|flannel'
# kindnet-xxxxx   1/1   Running   ...
```

### 0.2 Тест: реально ли работает enforce

```bash
kubectl create ns np-check
kubectl -n np-check run server --image=nginx:1.27-alpine --port=80
kubectl -n np-check run client --image=alpine --command -- sleep 3600
kubectl -n np-check wait --for=condition=Ready pod/server pod/client --timeout=90s
SIP=$(kubectl -n np-check get pod server -o jsonpath='{.status.podIP}')

# ДО политики — должно работать
kubectl -n np-check exec client -- wget -qO- --timeout=3 http://$SIP | grep -i title
# <title>Welcome to nginx!</title>

# включаем default-deny
kubectl -n np-check apply -f - <<'EOF'
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata: {name: deny, namespace: np-check}
spec: {podSelector: {}, policyTypes: [Ingress, Egress]}
EOF

# ПОСЛЕ политики — должно зависнуть/timeout
kubectl -n np-check exec client -- wget -qO- --timeout=3 http://$SIP \
  || echo "BLOCKED ✅ CNI enforce-ит NetworkPolicy"

kubectl delete ns np-check
```

- Запрос **завис/timeout** → CNI поддерживает NetworkPolicy → переходите к Шагу 1.
- Запрос **всё ещё проходит** → CNI игнорирует policy (старый kindnet / Flannel)
  → см. Шаг 0.3.

### 0.3 kind: CNI с поддержкой NetworkPolicy

**Современный kind уже enforce-ит NetworkPolicy «из коробки» — отдельный CNI
ставить не нужно**, Шаг 0.2 это подтвердит. Calico ниже не нужен в норме.
*(Проверено: kind v0.31.0, k8s v1.35, образ `kindnetd v20251212`; также kind v0.33.0,
k8s v1.37.0 на podman — kindnet enforce-ит policy, default-deny блокирует трафик.
Вариант с Calico v3.33.0 (kind v0.33.0, k8s v1.36.1, podman) тоже проверен: default-deny блокирует трафик, разрешающие правила работают.)*

Calico — **только запасной вариант**: используйте его, если у вас старый kind/kindnet
или Flannel и тест 0.2 показал, что policy игнорируется. Тогда пересоздайте кластер
с Calico, отключив дефолтный CNI. Сначала возьмите имя текущего кластера из контекста:

```bash
CLUSTER=$(kubectl config current-context | sed 's/^kind-//')
echo "$CLUSTER"
```

`kind-calico.yaml` (1 control-plane + 3 worker с zone-метками, как в Lab 27):
```yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
networking:
  disableDefaultCNI: true        # выключаем kindnet
  podSubnet: "192.168.0.0/16"    # дефолтный CIDR Calico
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
KIND_EXPERIMENTAL_PROVIDER=podman \
  kind create cluster --name "$CLUSTER" --config kind-calico.yaml

# ставим Calico (проверьте актуальную версию на docs.tigera.io)
kubectl apply -f https://raw.githubusercontent.com/projectcalico/calico/v3.33.0/manifests/calico.yaml

# ждём, пока поднимется
kubectl -n kube-system rollout status ds/calico-node --timeout=180s
kubectl get pods -n kube-system | grep calico
```

Альтернатива с minikube (проверьте флаг: `minikube start --help | grep -i cni`):
```bash
minikube start --cni=calico   # проверь: флаг --cni=calico в вашей версии minikube
```

После установки повторите тест 0.2 — default-deny должен блокировать трафик.

---

## Шаг 1. Setup — 3 app в одном namespace

`apps.yaml`:
```yaml
apiVersion: v1
kind: Namespace
metadata: {name: zone}
---
# Frontend
apiVersion: apps/v1
kind: Deployment
metadata: {name: frontend, namespace: zone}
spec:
  replicas: 1
  selector: {matchLabels: {app: frontend}}
  template:
    metadata: {labels: {app: frontend}}
    spec:
      containers:
      - name: c
        image: alpine
        command: ["sleep", "3600"]
---
# Backend
apiVersion: apps/v1
kind: Deployment
metadata: {name: backend, namespace: zone}
spec:
  replicas: 1
  selector: {matchLabels: {app: backend}}
  template:
    metadata: {labels: {app: backend}}
    spec:
      containers:
      - name: c
        image: nginx:1.27-alpine
        ports: [{containerPort: 80}]
---
apiVersion: v1
kind: Service
metadata: {name: backend, namespace: zone}
spec:
  selector: {app: backend}
  ports: [{port: 80, targetPort: 80}]
---
# DB
apiVersion: apps/v1
kind: Deployment
metadata: {name: db, namespace: zone}
spec:
  replicas: 1
  selector: {matchLabels: {app: db}}
  template:
    metadata: {labels: {app: db}}
    spec:
      containers:
      - name: c
        image: nginx:1.27-alpine
        ports: [{containerPort: 80}]
---
apiVersion: v1
kind: Service
metadata: {name: db, namespace: zone}
spec:
  selector: {app: db}
  ports: [{port: 80, targetPort: 80}]
```

```bash
kubectl apply -f apps.yaml
kubectl get all -n zone
```

---

## Шаг 2. Baseline — все могут всё

```bash
# Переменная живёт только в этой оболочке: в новой сессии задайте её заново.
FRONTEND=$(kubectl get pods -n zone -l app=frontend -o jsonpath='{.items[0].metadata.name}')

# Frontend может ходить в backend и db
kubectl exec -n zone $FRONTEND -- wget -qO- --timeout=2 backend:80
# должен вернуть nginx welcome page

kubectl exec -n zone $FRONTEND -- wget -qO- --timeout=2 db:80
# тоже работает

# Резолвится DNS
kubectl exec -n zone $FRONTEND -- getent hosts backend
# 10.96.x.x  backend.zone.svc.cluster.local ...   (exit 0)
```

> Используем `getent hosts`, а не `nslookup`: busybox-овский `nslookup` в alpine
> печатает NXDOMAIN-шум по лишним search-доменам и возвращает exit 1 **даже когда
> DNS работает**. `getent hosts` даёт чистый сигнал: строка с IP + exit 0, либо
> пусто + ненулевой код.

---

## Шаг 3. Default-deny all egress

`deny-all.yaml`:
```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: default-deny-all
  namespace: zone
spec:
  podSelector: {}            # все Pod-ы в namespace
  policyTypes: [Ingress, Egress]
```

```bash
kubectl apply -f deny-all.yaml

# Проверим — всё должно отвалиться
kubectl exec -n zone $FRONTEND -- wget -qO- --timeout=2 backend:80
# wget: bad address 'backend:80'  или  timeout  (оба варианта — блокировка; на практике первым срабатывает отказ DNS;
# при запрете DNS wget может упасть сразу на резолве имени)

kubectl exec -n zone $FRONTEND -- getent hosts backend
# пусто + exit≠0 — даже DNS не работает (egress на :53 тоже заблокирован)
```

---

## Шаг 4. Allow DNS — обязательно

`allow-dns.yaml`:
```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-dns
  namespace: zone
spec:
  podSelector: {}
  policyTypes: [Egress]
  egress:
  - to:
    - namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: kube-system
    ports:
    - port: 53
      protocol: UDP
    - port: 53
      protocol: TCP
```

```bash
kubectl apply -f allow-dns.yaml

# Теперь DNS работает
kubectl exec -n zone $FRONTEND -- getent hosts backend
# 10.96.x.x  backend.zone.svc.cluster.local ...   (exit 0)
# но к самому backend всё ещё нет доступа
kubectl exec -n zone $FRONTEND -- wget -qO- --timeout=2 backend:80
# timeout
```

---

## Шаг 5. Allow frontend → backend

`allow-frontend-to-backend.yaml`:
```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-frontend-to-backend
  namespace: zone
spec:
  # На стороне получателя — backend
  podSelector:
    matchLabels:
      app: backend
  policyTypes: [Ingress]
  ingress:
  - from:
    - podSelector:
        matchLabels:
          app: frontend
    ports:
    - port: 80
---
# И симметрично — разрешим frontend ВЫХОДИТЬ
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: frontend-egress-backend
  namespace: zone
spec:
  podSelector:
    matchLabels:
      app: frontend
  policyTypes: [Egress]
  egress:
  - to:
    - podSelector:
        matchLabels:
          app: backend
    ports:
    - port: 80
```

```bash
kubectl apply -f allow-frontend-to-backend.yaml

# Проверим
kubectl exec -n zone $FRONTEND -- wget -qO- --timeout=2 backend:80
# должно работать!

# А к db всё ещё нет
kubectl exec -n zone $FRONTEND -- wget -qO- --timeout=2 db:80
# timeout
```

---

## Шаг 6. Allow backend → db

```yaml
# allow-backend-to-db.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata: {name: allow-backend-to-db, namespace: zone}
spec:
  podSelector: {matchLabels: {app: db}}
  policyTypes: [Ingress]
  ingress:
  - from:
    - podSelector: {matchLabels: {app: backend}}
    ports:
    - port: 80
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata: {name: backend-egress-db, namespace: zone}
spec:
  podSelector: {matchLabels: {app: backend}}
  policyTypes: [Egress]
  egress:
  - to:
    - podSelector: {matchLabels: {app: db}}
    ports:
    - port: 80
```

```bash
kubectl apply -f allow-backend-to-db.yaml

# Переменная живёт только в этой оболочке: в новой сессии задайте её заново.
BACKEND=$(kubectl get pods -n zone -l app=backend -o jsonpath='{.items[0].metadata.name}')
kubectl exec -n zone $BACKEND -- curl --max-time 2 db:80
# должно работать

# frontend всё ещё НЕ может в db напрямую — это правильно (защита in depth)
kubectl exec -n zone $FRONTEND -- wget -qO- --timeout=2 db:80
# timeout ✅
```

---

## Шаг 7. Cross-namespace policy

Полный манифест (не применялся при проверке; `podSelector` цели — подставьте свои Pod-ы, `app: backend` здесь пример; проверь):

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-prod-api-ingress
  namespace: zone
spec:
  podSelector:
    matchLabels:
      app: backend
  policyTypes: [Ingress]
  ingress:
  - from:
    - namespaceSelector:
        matchLabels:
          env: production
      podSelector:
        matchLabels:
          app: api
    ports:
    - port: 80
```

> namespace label `env: production` нужно поставить вручную:
> `kubectl create ns prod --dry-run=client -o yaml | kubectl apply -f - && kubectl label ns prod env=production --overwrite`
> (namespace `prod` в этой лабе не создаётся, команда создаёт его, если его нет)

---

## Cleanup

```bash
kubectl delete ns zone
```

---

## Что вы узнали

- **NetworkPolicy** требует CNI с поддержкой (Cilium, Calico)
- Default state: всё разрешено → NetworkPolicy переключают на **deny-by-default**
- **Default-deny-all** + explicit allow rules — это canonical pattern
- **Allow DNS** обязательно — без него все timeout-ы
- Двусторонний контроль: Ingress (получатель) + Egress (отправитель)
- Cross-namespace через `namespaceSelector`
- Production: micro-segmentation per business function (frontend → backend → db)
