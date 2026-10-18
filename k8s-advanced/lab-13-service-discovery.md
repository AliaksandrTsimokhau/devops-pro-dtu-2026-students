# Lab 13 — Service Discovery & DNS

**Цель:** разобраться как Kubernetes резолвит имена сервисов через CoreDNS, увидеть short names vs FQDN.

**Время:** 25 минут
**Prerequisites:** Lab 06, 08.

---

## Шаг 1. Inspect cluster DNS

```bash
# CoreDNS Pods
kubectl get pods -n kube-system -l k8s-app=kube-dns

# CoreDNS Service — это и есть точка входа для всех Pod-ов кластера
kubectl get svc -n kube-system kube-dns
# CLUSTER-IP обычно 10.96.0.10 (или подобное)
```

Запомните этот ClusterIP.

---

## Шаг 2. Развернём 2 идентичных app в 2 namespace

`sd-example.yaml`:
```yaml
apiVersion: v1
kind: Namespace
metadata: {name: dev}
---
apiVersion: v1
kind: Namespace
metadata: {name: prod}
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: enterprise
  namespace: dev
spec:
  replicas: 2
  selector:
    matchLabels: {app: enterprise}
  template:
    metadata:
      labels: {app: enterprise}
    spec:
      containers:
      - name: web
        image: nigelpoulton/k8sbook:text-dev
        ports: [{containerPort: 8080}]
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: enterprise
  namespace: prod
spec:
  replicas: 2
  selector:
    matchLabels: {app: enterprise}
  template:
    metadata:
      labels: {app: enterprise}
    spec:
      containers:
      - name: web
        image: nigelpoulton/k8sbook:text-prod
        ports: [{containerPort: 8080}]
---
apiVersion: v1
kind: Service
metadata:
  name: ent
  namespace: dev
spec:
  ports: [{port: 8080}]
  selector: {app: enterprise}
---
apiVersion: v1
kind: Service
metadata:
  name: ent
  namespace: prod
spec:
  ports: [{port: 8080}]
  selector: {app: enterprise}
---
apiVersion: v1
kind: Pod
metadata:
  name: jump
  namespace: dev
spec:
  containers:
  - image: ubuntu
    name: jump
    tty: true
    stdin: true
    command: ["sleep", "infinity"]
```

```bash
kubectl apply -f sd-example.yaml
kubectl get all -n dev
kubectl get all -n prod
```

---

## Шаг 3. /etc/resolv.conf внутри Pod

```bash
kubectl exec -n dev jump -- cat /etc/resolv.conf
```

Должно быть:
```
search dev.svc.cluster.local svc.cluster.local cluster.local
nameserver 10.96.0.10
options ndots:5
```

**Что это значит:**
- `nameserver 10.96.0.10` = CoreDNS Service IP
- `search dev.svc.cluster.local` = первый search domain (наш namespace)
- `options ndots:5` = если в имени <5 точек, K8s пробует search domains

---

## Шаг 4. Short name resolution (same namespace)

```bash
kubectl exec -it -n dev jump -- bash
# внутри:
apt-get update && apt-get install -y curl dnsutils

# Short name работает (есть search domain dev)
curl ent:8080
# Hello from the DEV Namespace!

nslookup ent
# Server: 10.96.0.10
# Address: 10.96.0.10#53
# Name: ent.dev.svc.cluster.local
# Address: 10.96.x.x
```

---

## Шаг 5. Cross-namespace — нужен FQDN

```bash
# Внутри jump pod (всё ещё в namespace dev):

# Short name — НЕ найдёт ent в prod
nslookup ent.prod 2>&1 | head -5
# Это уже не short name (1 dot < 5) → попробуется через search domains:
# ent.prod.dev.svc.cluster.local — нет
# ent.prod.svc.cluster.local — есть!

curl ent.prod:8080
# Hello from the PROD Namespace!

# Или полный FQDN
curl ent.prod.svc.cluster.local:8080
exit
```

> Best practice: для cross-namespace **всегда FQDN** в манифестах.

---

## Шаг 6. Поломаем DNS — посмотрим что произойдёт

В отдельном терминале:
```bash
# Удалим один CoreDNS Pod
kubectl delete pod -n kube-system -l k8s-app=kube-dns
```

Внутри jump pod:
```bash
kubectl exec -it -n dev jump -- bash
nslookup ent                              # должно ещё работать (вторая реплика CoreDNS)
```

Если бы вы удалили все CoreDNS реплики одновременно — DNS бы сломался в кластере на несколько секунд. Поэтому в production CoreDNS всегда 2+ реплик с anti-affinity.

---

## Шаг 7. kube-proxy + iptables (на ноде)

```bash
# Только если у вас kind/k3s — можно зайти на ноду:
docker exec -it <node-container> sh
iptables -t nat -L KUBE-SERVICES | grep ent
```

Видим правила: трафик на ClusterIP `ent` редиректится на Pod IPs.

---

## Cleanup

```bash
kubectl delete -f sd-example.yaml
```

---

## Что вы узнали

- Каждый Pod автоматически настроен резолвить через **CoreDNS**
- `/etc/resolv.conf` имеет search domains namespace-а Pod-а
- Short names резолвятся в same-namespace
- Cross-namespace — нужен FQDN: `<svc>.<namespace>.svc.cluster.local`
- ClusterIP — виртуальный IP, работает через kube-proxy + iptables/IPVS/eBPF
- CoreDNS должен иметь 2+ реплик с anti-affinity для отказоустойчивости
