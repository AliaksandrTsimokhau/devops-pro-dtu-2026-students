# Lab 18 — ConfigMaps: env, args, volume mount

**Цель:** научиться использовать ConfigMap во всех трёх стилях и увидеть какой обновляется live.

**Время:** 20 минут
**Prerequisites:** Lab 03.

---

## Шаг 1. Создаём ConfigMap

### Imperative
```bash
kubectl create cm app-settings \
  --from-literal=log_level=info \
  --from-literal=greeting="Hello, Kubernetes!"

kubectl describe cm app-settings
```

### Declarative
`cm.yaml`:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-files
data:
  app.properties: |
    timeout=30
    retries=5
    feature.new_ui=true
  greeting: "Welcome to k8s"
```

```bash
kubectl apply -f cm.yaml
kubectl get cm
```

---

## Шаг 2. Use as env vars

`pod-env.yaml`:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-env
spec:
  containers:
  - name: app
    image: busybox
    command: ["sleep", "3600"]
    env:
    - name: LOG_LEVEL
      valueFrom:
        configMapKeyRef:
          name: app-settings
          key: log_level
    - name: GREETING
      valueFrom:
        configMapKeyRef:
          name: app-settings
          key: greeting
```

```bash
kubectl apply -f pod-env.yaml
kubectl exec pod-env -- env | grep -E 'LOG_LEVEL|GREETING'
# LOG_LEVEL=info
# GREETING=Hello, Kubernetes!
```

---

## Шаг 3. Use as command args

`pod-args.yaml`:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-args
spec:
  containers:
  - name: app
    image: busybox
    command: ["/bin/sh", "-c", "echo Level $(LOG_LEVEL): $(GREETING) && sleep 3600"]
    env:
    - name: LOG_LEVEL
      valueFrom: {configMapKeyRef: {name: app-settings, key: log_level}}
    - name: GREETING
      valueFrom: {configMapKeyRef: {name: app-settings, key: greeting}}
```

```bash
kubectl apply -f pod-args.yaml
kubectl logs pod-args
# Level info: Hello, Kubernetes!
```

---

## Шаг 4. Use as volume mount (рекомендуется)

`pod-vol.yaml`:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-vol
spec:
  volumes:
  - name: config
    configMap:
      name: app-files
  containers:
  - name: app
    image: busybox
    command: ["sleep", "3600"]
    volumeMounts:
    - mountPath: /etc/app
      name: config
```

```bash
kubectl apply -f pod-vol.yaml
kubectl exec pod-vol -- ls /etc/app
# app.properties  greeting
kubectl exec pod-vol -- cat /etc/app/app.properties
```

---

## Шаг 5. Live update — только volume mount обновляется!

Поменяем ConfigMap:
```bash
kubectl edit cm app-files
# измените greeting на "UPDATED!"
```

Проверим:
```bash
# Через 30-60 секунд:
kubectl exec pod-vol -- cat /etc/app/greeting
# UPDATED!

# А в env-варианте — старое значение!
kubectl exec pod-env -- env | grep GREETING
# GREETING=Hello, Kubernetes!   # старое! нужен рестарт Pod-а
```

> Volume mount = live updates. Env vars = snapshot на момент старта Pod-а.

---

## Cleanup

```bash
kubectl delete -f pod-env.yaml -f pod-args.yaml -f pod-vol.yaml -f cm.yaml
kubectl delete cm app-settings
```

---

## Что вы узнали

- ConfigMap создаётся imperative (`--from-literal`, `--from-file`) или declarative
- 3 способа использования: env / args / volume
- **Volume mount** — единственный способ который **обновляется live** (через ~60 сек)
- Env vars и args — snapshot, требуют рестарта Pod-а для обновления
- ConfigMap до 1 MiB — для всего больше используйте PVC
