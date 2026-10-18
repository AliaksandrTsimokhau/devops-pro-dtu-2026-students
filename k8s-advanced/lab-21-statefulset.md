# Lab 21 — StatefulSet with persistent storage

**Цель:** развернуть StatefulSet с volumeClaimTemplates, увидеть sticky volumes, headless Service DNS.

**Время:** 25 минут
**Prerequisites:** module 02, Lab 20 — PVC/StorageClass, module 03, Lab 08 — Deployment.

---

## Шаг 1. Headless Service для peer discovery

`headless.yaml`:
```yaml
apiVersion: v1
kind: Service
metadata:
  name: nginx-headless
  labels:
    app: web-sts
spec:
  clusterIP: None                # ← headless
  selector:
    app: web-sts
  ports:
  - port: 80
    name: web
```

```bash
kubectl apply -f headless.yaml
kubectl get svc nginx-headless
# CLUSTER-IP: None
```

---

## Шаг 2. StatefulSet

`sts.yaml`:
```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: web
spec:
  serviceName: nginx-headless    # ← governing Service
  replicas: 3
  selector:
    matchLabels: {app: web-sts}
  template:
    metadata:
      labels: {app: web-sts}
    spec:
      terminationGracePeriodSeconds: 10
      containers:
      - name: nginx
        image: nginx:1.27-alpine
        ports:
        - containerPort: 80
          name: web
        volumeMounts:
        - name: www
          mountPath: /usr/share/nginx/html
  volumeClaimTemplates:          # ← один PVC per Pod
  - metadata:
      name: www
    spec:
      accessModes: [ReadWriteOnce]
      storageClassName: standard
      resources:
        requests:
          storage: 100Mi
```

```bash
kubectl apply -f sts.yaml
kubectl get sts web --watch
# READY переходит 0/3 → 1/3 → 2/3 → 3/3 — по очереди
```

---

## Шаг 3. Inspect

```bash
kubectl get pods -l app=web-sts -o wide
# web-0   1/1   Running   ...
# web-1   1/1   Running   ...
# web-2   1/1   Running   ...

# PVC создались автоматически (одна per Pod)
kubectl get pvc
# www-web-0   Bound   ...   100Mi   RWO
# www-web-1   Bound   ...   100Mi   RWO
# www-web-2   Bound   ...   100Mi   RWO
```

---

## Шаг 4. DNS per Pod

```bash
# Поднимем debug-Pod
kubectl run dnsutils --rm -it \
  --image=registry.k8s.io/e2e-test-images/jessie-dnsutils:1.7 -- bash

# Внутри:
# Имена пишем с точкой на конце (абсолютный FQDN). Без неё resolver добавляет
# search-домены хоста (ndots:5): на машинах с корпоративными search-доменами
# и wildcard-записью короткое имя может вернуть чужой адрес вместо IP подов.
nslookup web-0.nginx-headless.default.svc.cluster.local.
# вернёт IP конкретного Pod-а web-0

nslookup web-1.nginx-headless.default.svc.cluster.local.
nslookup web-2.nginx-headless.default.svc.cluster.local.

# Headless Service возвращает ВСЕ IPs
nslookup nginx-headless.default.svc.cluster.local.
# Address: <web-0 IP>
# Address: <web-1 IP>
# Address: <web-2 IP>

dig SRV nginx-headless.default.svc.cluster.local.
# ANSWER SECTION — мапинг на Pod DNS names
exit
```

---

## Шаг 5. Sticky volumes — данные привязаны к ordinal

```bash
# Запишем разные данные в каждый Pod
kubectl exec web-0 -- sh -c 'echo "I am web-0" > /usr/share/nginx/html/index.html'
kubectl exec web-1 -- sh -c 'echo "I am web-1" > /usr/share/nginx/html/index.html'
kubectl exec web-2 -- sh -c 'echo "I am web-2" > /usr/share/nginx/html/index.html'

# Проверим
for i in 0 1 2; do kubectl exec web-$i -- cat /usr/share/nginx/html/index.html; done
```

---

## Шаг 6. Удалим Pod web-0 — данные сохранятся!

```bash
kubectl delete pod web-0
# K8s сразу создаст новый Pod с тем же именем

kubectl get pods -l app=web-sts --watch
# web-0   0/1   ContainerCreating
# web-0   1/1   Running

# Проверим что volume тот же
kubectl exec web-0 -- cat /usr/share/nginx/html/index.html
# I am web-0
```

> Pod пересоздан, но получил **тот же PVC** `www-web-0` → те же данные.

---

## Шаг 7. Scale down — PVC остаются

```bash
kubectl scale sts web --replicas=1
kubectl get pods -l app=web-sts
# Только web-0
kubectl get pvc
# Все три PVC ещё есть!
```

Scale up обратно:
```bash
kubectl scale sts web --replicas=3
kubectl exec web-2 -- cat /usr/share/nginx/html/index.html
# I am web-2  ← старые данные вернулись!
```

---

## Cleanup

```bash
# Сначала scale to 0 для graceful shutdown
kubectl scale sts web --replicas=0

# Удалить StatefulSet
kubectl delete sts web
kubectl delete svc nginx-headless

# PVC не удаляются автоматически
# метка app=web-sts только у Pod-ов и PVC этой лабы (не затрагивает чужие PVC)
kubectl delete pvc -l app=web-sts
# или
kubectl delete pvc www-web-0 www-web-1 www-web-2
```

---

> Заметка: по умолчанию PVC StatefulSet-а **не удаляются** вместе с ним и при scale down. Поле `persistentVolumeClaimRetentionPolicy` (`whenDeleted` / `whenScaled`: `Retain` или `Delete`) меняет это поведение; проверь: поле есть в вашей версии кластера (`kubectl explain statefulset.spec.persistentVolumeClaimRetentionPolicy`).

---

## Что вы узнали

- StatefulSet даёт **sticky identity**: имя + DNS + volume — стабильны
- `volumeClaimTemplates` создаёт PVC per Pod автоматически
- Headless Service (`clusterIP: None`) даёт DNS per Pod для peer discovery
- Pod failure → новый Pod с тем же именем и тем же PVC
- Scale down: Pod-ы удаляются, **PVC остаются**
- Production stateful — обычно через Operators (CloudNativePG, MongoDB Operator)
