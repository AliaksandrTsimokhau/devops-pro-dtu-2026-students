# Lab 11 — Ingress: host and path routing

**Цель:** установить NGINX Ingress Controller, развернуть 2 приложения, маршрутизировать трафик по host и path.

**Время:** 30 минут
**Prerequisites:** Lab 10.

> ⚠️ Ingress NGINX retired March 2026. Для production — Gateway API. Здесь мы используем NGINX для учебных целей.

---

## Шаг 1. Установка NGINX Ingress Controller

```bash
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/controller-v1.9.4/deploy/static/provider/cloud/deploy.yaml

# Дождитесь:
kubectl wait --namespace ingress-nginx \
  --for=condition=ready pod \
  --selector=app.kubernetes.io/component=controller \
  --timeout=120s
```

Проверим:
```bash
kubectl get pods -n ingress-nginx
kubectl get svc -n ingress-nginx
kubectl get ingressclass
# NAME    CONTROLLER             ...
# nginx   k8s.io/ingress-nginx
```

---

## Шаг 2. Деплоим 2 приложения

`app.yaml`:
```yaml
apiVersion: v1
kind: Service
metadata:
  name: svc-shield
spec:
  type: ClusterIP
  ports:
  - port: 8080
    targetPort: 8080
  selector:
    env: shield
---
apiVersion: v1
kind: Service
metadata:
  name: svc-hydra
spec:
  type: ClusterIP
  ports:
  - port: 8080
    targetPort: 8080
  selector:
    env: hydra
---
apiVersion: v1
kind: Pod
metadata:
  name: shield
  labels:
    env: shield
spec:
  containers:
  - name: shield-ctr
    image: nigelpoulton/k8sbook:shield-ingress
    ports:
    - containerPort: 8080
---
apiVersion: v1
kind: Pod
metadata:
  name: hydra
  labels:
    env: hydra
spec:
  containers:
  - name: hydra-ctr
    image: nigelpoulton/k8sbook:hydra-ingress
    ports:
    - containerPort: 8080
```

```bash
kubectl apply -f app.yaml
kubectl get pods,svc
```

---

## Шаг 3. Ingress с host + path routing

`ingress.yaml`:
```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: mcu-all
  annotations:
    nginx.ingress.kubernetes.io/rewrite-target: /
spec:
  ingressClassName: nginx
  rules:
  # Host-based
  - host: shield.mcu.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: svc-shield
            port: {number: 8080}
  - host: hydra.mcu.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: svc-hydra
            port: {number: 8080}
  # Path-based
  - host: mcu.com
    http:
      paths:
      - path: /shield
        pathType: Prefix
        backend:
          service:
            name: svc-shield
            port: {number: 8080}
      - path: /hydra
        pathType: Prefix
        backend:
          service:
            name: svc-hydra
            port: {number: 8080}
```

```bash
kubectl apply -f ingress.yaml
kubectl get ing mcu-all
kubectl describe ing mcu-all
```

---

## Шаг 4. DNS resolution локально

Узнайте адрес LB:
```bash
kubectl get svc -n ingress-nginx ingress-nginx-controller
# EXTERNAL-IP — это адрес для DNS. Для Docker Desktop = localhost.
```

Добавьте записи в `/etc/hosts`:
```
127.0.0.1 shield.mcu.com
127.0.0.1 hydra.mcu.com
127.0.0.1 mcu.com
```

> На Mac: `sudo vi /etc/hosts`
> На Windows: `C:\Windows\System32\drivers\etc\hosts`

---

## Шаг 5. Тестируем

```bash
curl http://shield.mcu.com
curl http://hydra.mcu.com
curl http://mcu.com/shield
curl http://mcu.com/hydra
```

В браузере: те же URL-ы.

---

## Cleanup

```bash
kubectl delete -f ingress.yaml -f app.yaml

# Удалить Ingress Controller (если не нужен для следующих лаб):
kubectl delete -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/controller-v1.9.4/deploy/static/provider/cloud/deploy.yaml

# Уберите /etc/hosts записи
```

---

## Что вы узнали

- Ingress = resource + controller (controller нужно устанавливать отдельно)
- IngressClass привязывает Ingress к controller-у (если их несколько)
- Host-based routing: разные `host` в `rules`
- Path-based routing: разные `paths` под одним `host`
- Annotations — NGINX-специфичные настройки (rewrite, timeouts, etc.)
- Pod → Service → Ingress → External — типовой production-стек
