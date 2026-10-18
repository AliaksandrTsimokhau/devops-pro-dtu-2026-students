# Lab 03 — Deploy your first Pod

**Цель:** развернуть Pod из YAML-манифеста и сделать introspection.

**Время:** 15 минут
**Prerequisites:** Lab 01, 02.

---

## Шаг 1. Pod manifest

`pod.yaml`:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: hello-pod
  labels:
    zone: prod
    version: v1
spec:
  containers:
  - name: hello-ctr
    image: nigelpoulton/k8sbook:1.0
    ports:
    - containerPort: 8080
    resources:
      limits:
        memory: 128Mi
        cpu: 500m
```

```bash
kubectl apply -f pod.yaml
kubectl get pods
```

Если STATUS `ContainerCreating` → подождите 10–20 сек.

---

## Шаг 2. Introspect

```bash
# Простая инфо
kubectl get pod hello-pod

# С labels
kubectl get pod hello-pod --show-labels

# Wide — + IP, node
kubectl get pod hello-pod -o wide

# Полный YAML, включая Status (observed state)
kubectl get pod hello-pod -o yaml | head -60
```

Замечаете: в выводе намного больше полей, чем вы написали в манифесте. Kubernetes сам заполняет defaults и status.

---

## Шаг 3. describe — события и условия

```bash
kubectl describe pod hello-pod
```

Особое внимание:
- **Conditions** — `Ready`, `Initialized`, `ContainersReady`
- **Events** — внизу: вытаскивание image, старт контейнера

---

## Шаг 4. logs

```bash
kubectl logs hello-pod
kubectl logs hello-pod -f                 # follow
kubectl logs hello-pod --tail=20
kubectl logs hello-pod --previous          # из предыдущего инстанса (если был restart)
```

---

## Шаг 5. exec — внутрь Pod

```bash
# Одна команда
kubectl exec hello-pod -- ps aux
kubectl exec hello-pod -- env

# Интерактивный shell
kubectl exec -it hello-pod -- sh

# Внутри:
apk add curl                              # установим curl
curl localhost:8080                       # проверим что приложение работает
exit
```

---

## Шаг 6. Immutability — попробуем поменять Pod

```bash
kubectl edit pod hello-pod
```

Откроется редактор. Попробуйте поменять:
- `metadata.name`
- `spec.containers[0].name`
- `spec.containers[0].ports[0].containerPort`

Сохранитесь — получите ошибку: **forbidden, fields are immutable**.

> Pod — immutable. Для изменения — пересоздайте.

---

## Шаг 7. Cleanup

```bash
kubectl delete pod hello-pod
# или
kubectl delete -f pod.yaml
```

---

## Что вы узнали

- 4 обязательных поля Pod: `apiVersion`, `kind`, `metadata`, `spec`
- `kubectl describe` → events помогают понять что не так
- `kubectl logs --previous` — увидеть последний crash
- `kubectl exec -it` — интерактивный shell внутри контейнера
- Pod-ы immutable: editable fields очень ограничены
