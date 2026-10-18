# Lab 08 — Deployment basics: create, scale, expose

**Цель:** развернуть Deployment с 10 репликами, экспонировать через LoadBalancer Service, скейлить вверх/вниз.

**Время:** 20 минут
**Prerequisites:** Lab 03, 05.

---

## Шаг 1. Deployment manifest

`deploy.yaml`:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: hello-deploy
spec:
  replicas: 10
  selector:
    matchLabels:
      app: hello-world
  revisionHistoryLimit: 5
  progressDeadlineSeconds: 300
  minReadySeconds: 10
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 1
      maxSurge: 1
  template:
    metadata:
      labels:
        app: hello-world
    spec:
      containers:
      - name: hello-pod
        image: nigelpoulton/k8sbook:1.0
        ports:
        - containerPort: 8080
```

```bash
kubectl apply -f deploy.yaml
kubectl get deploy hello-deploy
kubectl get rs                            # появится 1 ReplicaSet с хешем
kubectl get pods --show-labels
```

---

## Шаг 2. Inspect

```bash
kubectl describe deploy hello-deploy
# обратите внимание на:
# - StrategyType: RollingUpdate
# - RollingUpdateStrategy: 1 max unavailable, 1 max surge
# - NewReplicaSet: hello-deploy-<hash>

kubectl describe rs hello-deploy-<hash>
# Pod-ы помечены: app=hello-world, pod-template-hash=<hash>
```

---

## Шаг 3. Expose через LoadBalancer

`lb.yaml`:
```yaml
apiVersion: v1
kind: Service
metadata:
  name: lb-svc
  labels:
    app: hello-world
spec:
  type: LoadBalancer
  ports:
  - port: 8080
    protocol: TCP
  selector:
    app: hello-world
```

```bash
kubectl apply -f lb.yaml
kubectl get svc lb-svc
```

В Docker Desktop / kind `EXTERNAL-IP` будет `localhost`. Откройте http://localhost:8080 в браузере.

---

## Шаг 4. Manual scaling

### Imperative
```bash
kubectl scale deploy hello-deploy --replicas=5
kubectl get deploy hello-deploy
kubectl get pods -l app=hello-world
```

### Declarative (правильный путь)
Поменяйте `replicas: 5` → `replicas: 8` в `deploy.yaml`:
```bash
kubectl apply -f deploy.yaml
kubectl get deploy hello-deploy
```

> Imperative scale = drift с Git. Всегда после imperative reapply YAML.

---

## Шаг 5. Self-healing — попробуем удалить Pod

```bash
# Список
kubectl get pods -l app=hello-world

# Удалим один Pod
kubectl delete pod <pod-name>

# Через секунду новый Pod уже создан
kubectl get pods -l app=hello-world
```

Замечаете: имя Pod-а другое, но Deployment поддержал количество реплик. **Self-healing работает.**

---

## Cleanup

```bash
kubectl delete -f lb.yaml
kubectl delete -f deploy.yaml
```

---

## Что вы узнали

- Deployment → ReplicaSet → Pod (три уровня)
- `kubectl scale` — imperative; редактирование YAML — declarative
- Pod-ы помечаются `pod-template-hash` автоматически
- Self-healing: удалили Pod → controller создаст замену
- LoadBalancer Service экспонирует наружу с balancing'ом
