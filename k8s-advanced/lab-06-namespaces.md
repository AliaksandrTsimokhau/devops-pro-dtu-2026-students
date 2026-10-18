# Lab 06 — Working with Namespaces

**Цель:** научиться создавать namespaces, развёртывать в них приложения и переключать контекст.

**Время:** 15 минут
**Prerequisites:** Lab 01–02.

---

## Шаг 1. Список существующих namespaces

```bash
kubectl get namespaces                    # или kubectl get ns
kubectl describe ns default
kubectl describe ns kube-system
```

**Стандартные namespace-ы:**
- `default` — куда попадает всё без `-n`
- `kube-system` — control plane
- `kube-public` — публично-читаемые объекты
- `kube-node-lease` — heartbeat от kubelet

---

## Шаг 2. Создаём namespaces

### Imperative
```bash
kubectl create ns hydra
```

### Declarative
`shield-ns.yaml`:
```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: shield
  labels:
    env: marvel
```

```bash
kubectl apply -f shield-ns.yaml
kubectl get ns
kubectl get ns --show-labels
```

---

## Шаг 3. Деплоим приложение в shield namespace

`app-shield.yaml`:
```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  namespace: shield
  name: default
---
apiVersion: v1
kind: Service
metadata:
  namespace: shield
  name: the-bus
spec:
  type: ClusterIP
  ports:
  - port: 8080
    targetPort: 8080
  selector:
    env: marvel
---
apiVersion: v1
kind: Pod
metadata:
  namespace: shield
  name: triskelion
  labels:
    env: marvel
spec:
  containers:
  - name: web
    image: nginx:1.27-alpine
    ports:
    - containerPort: 80
```

```bash
kubectl apply -f app-shield.yaml
kubectl get pods -n shield
kubectl get svc -n shield
```

---

## Шаг 4. Привязываем kubectl к namespace

```bash
# Сейчас:
kubectl config view --minify | grep namespace || echo "namespace = default"

# Переключаемся
kubectl config set-context --current --namespace=shield

# Теперь get pods без -n
kubectl get pods                          # Pod-ы shield
kubectl get all                           # всё в текущем namespace
```

---

## Шаг 5. Cross-namespace traffic

```bash
# Создадим debug Pod в default namespace
kubectl run debug --rm -it \
  --image=alpine \
  --namespace=default \
  -- sh

# Внутри debug-pod-а попробуйте резолвить:
apk add curl bind-tools
nslookup the-bus                         # FAIL — ищет в default
nslookup the-bus.shield                  # OK — добавили namespace
nslookup the-bus.shield.svc.cluster.local  # FQDN
exit
```

> Из другого namespace **нужен FQDN** или хотя бы `<svc>.<namespace>`.

---

## Шаг 6. Удаление namespace = удаление всего внутри

```bash
# Вернёмся в default
kubectl config set-context --current --namespace=default

# Удалим shield (каскадно!)
kubectl delete ns shield

# Проверим
kubectl get pods -n shield               # No resources

# Удалим hydra
kubectl delete ns hydra
```

---

## Что вы узнали

- 4 встроенных namespace в каждом кластере
- Namespace создаётся imperative (`kubectl create ns`) или declarative
- `kubectl config set-context --current --namespace=X` экономит много `-n`
- Cross-namespace traffic требует FQDN или `<svc>.<namespace>`
- Удаление namespace каскадно удаляет ВСЁ внутри — необратимо
