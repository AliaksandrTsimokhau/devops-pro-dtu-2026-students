# Lab 10 — Service types: ClusterIP, NodePort, LoadBalancer

**Цель:** разобраться с тремя основными типами Service, увидеть как они построены друг на друге.

**Время:** 25 минут
**Prerequisites:** Lab 08.

---

## Шаг 1. Bootstrap — поднимем Deployment

`deploy.yaml`:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: svc-test
spec:
  replicas: 5
  selector:
    matchLabels:
      chapter: services
  template:
    metadata:
      labels:
        chapter: services
    spec:
      containers:
      - name: web
        image: nigelpoulton/k8sbook:1.0
        ports:
        - containerPort: 8080
```

```bash
kubectl apply -f deploy.yaml
kubectl get pods -l chapter=services -o wide
```

Запомните Pod IPs.

---

## Шаг 2. ClusterIP — внутренний

`clusterip.yaml`:
```yaml
apiVersion: v1
kind: Service
metadata:
  name: cip
spec:
  type: ClusterIP                # default; можно опустить
  ports:
  - port: 8080
    targetPort: 8080
  selector:
    chapter: services
```

```bash
kubectl apply -f clusterip.yaml
kubectl get svc cip
kubectl describe svc cip
```

Обратите внимание на `Endpoints:` — там IP всех Pod-ов с label `chapter=services`.

```bash
kubectl get endpointslices -l kubernetes.io/service-name=cip
```

### Проверка из кластера

```bash
kubectl run debug --rm -it --image=alpine -- sh
# внутри:
apk add curl bind-tools
nslookup cip                              # → ClusterIP
curl http://cip:8080                      # должно работать
exit
```

> ClusterIP **не достижим** снаружи кластера.

---

## Шаг 3. NodePort — внешний доступ через порт нод

`nodeport.yaml`:
```yaml
apiVersion: v1
kind: Service
metadata:
  name: np
spec:
  type: NodePort
  ports:
  - port: 8080
    targetPort: 8080
    nodePort: 30080
  selector:
    chapter: services
```

```bash
kubectl apply -f nodeport.yaml
kubectl get svc np
```

Колонка PORT: `8080:30080/TCP`.

### Проверка

Найдите IP ноды:
```bash
kubectl get nodes -o wide
```

И обратитесь снаружи:
```bash
curl http://<NODE-IP>:30080
# или для Docker Desktop:
curl http://localhost:30080
```

> NodePort всегда в диапазоне 30000–32767.

---

## Шаг 4. LoadBalancer — облачный LB

`loadbalancer.yaml`:
```yaml
apiVersion: v1
kind: Service
metadata:
  name: lb
spec:
  type: LoadBalancer
  ports:
  - port: 8080
    targetPort: 8080
  selector:
    chapter: services
```

```bash
kubectl apply -f loadbalancer.yaml
kubectl get svc lb
```

- **Docker Desktop**: `EXTERNAL-IP` = `localhost`
- **kind**: `<pending>` — пока не запущен провайдер LB (см. ниже)
- **Cloud**: реальный публичный IP через 30–60 сек

```bash
curl http://localhost:8080                # Docker Desktop
```

### Шаг 4a. Включаем LoadBalancer на kind + podman (macOS / Windows)

kind сам по себе **не реализует** LoadBalancer — Service остаётся в `<pending>`.
Нужен отдельный хостовый бинарь **`cloud-provider-kind`**: он следит за Service
типа LoadBalancer и поднимает контейнер-балансировщик (envoy) на каждый сервис.

> **Почему так на macOS/Windows:** ноды kind работают внутри VM (podman machine).
> IP, который выдаёт балансировщик (`172.18.x` / `10.89.x`), **не маршрутизируется**
> с хоста. Поэтому обязателен флаг `--enable-lb-port-mapping` — он публикует сервис
> на `127.0.0.1:<порт>`.

Установка:
```bash
brew install cloud-provider-kind          # или: go install sigs.k8s.io/cloud-provider-kind@latest
```

Запуск (в отдельном терминале, держать запущенным):
```bash
# podman: указываем на rootful-сокет podman machine, где живут ноды kind
sudo DOCKER_HOST="unix://$(podman machine inspect <MACHINE> --format '{{.ConnectionInfo.PodmanSocket.Path}}')" \
     KUBECONFIG="$HOME/.kube/config" \
     cloud-provider-kind --enable-lb-port-mapping
```

Подводные камни:
- **`sudo` обязателен** на macOS — без него бинарь сразу падает с `please run this again with sudo`.
- Передавайте `DOCKER_HOST` и `KUBECONFIG` **внутри** `sudo VAR=val …` — иначе root
  не увидит ни сокет podman, ни kubeconfig.
- Для **rootful podman machine** провайдер видит сеть `kind`; rootless — нет.
- На **Colima** контейнер envoy не стартует (`exit status 125`) — используйте podman.

Проверка:
```bash
kubectl get svc lb                         # EXTERNAL-IP станет реальным за ~30 сек
# найдём проброшенный на localhost порт балансировщика:
podman --connection <MACHINE>-root ps --filter name=kindccm     # колонка PORTS: 0.0.0.0:NNNNN->8080
curl http://127.0.0.1:NNNNN/               # трафик дойдёт до Pod-а
```

> `EXTERNAL-IP` напрямую с хоста на macOS **не курлится** — только через `127.0.0.1:<порт>`.

### Шаг 4b. Альтернатива — MetalLB

`cloud-provider-kind` — это «игрушечный» LB для разработки. В реальном bare-metal
кластере (без облака) LoadBalancer обычно реализует **MetalLB** — он раздаёт IP из
заданного пула и анонсирует их по ARP (L2) или BGP. Полезно показать студентам
«настоящий» механизм.

**Что выбрать:**

| | `cloud-provider-kind` | MetalLB |
|---|---|---|
| Назначение | локальная разработка на kind | bare-metal прод + обучение |
| Как раздаёт IP | контейнер envoy на сервис | пул IP + L2/BGP анонс |
| Доступ с хоста (macOS/podman) | ✅ через `127.0.0.1:<порт>` (`--enable-lb-port-mapping`) | ❌ IP пула не маршрутизируется с хоста (живёт внутри VM) |
| Доступ изнутри кластера/ноды | ✅ | ✅ |
| Нужен root | да (`sudo`) | нет |

> **Вывод для macOS/Windows + podman:** MetalLB корректно ставится и выдаёт IP, но
> с хоста этот IP **недостижим** (нода в VM; известный баг — `No route to host` на
> podman). Для доступа с ноутбука используйте `cloud-provider-kind`. MetalLB здесь —
> для демонстрации концепции и проверки изнутри кластера.

Установка MetalLB (проверено на kind + podman):
```bash
# 1. узнать подсеть сети kind — пул IP берём из неё
podman --connection <MACHINE>-root network inspect kind \
  --format '{{range .Subnets}}{{.Subnet}} {{end}}'           # напр. 10.89.0.0/24

# 2. поставить MetalLB (актуальную версию см. на github.com/metallb/metallb/releases)
kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.16.1/config/manifests/metallb-native.yaml
kubectl wait -n metallb-system --for=condition=ready pod --selector=app=metallb --timeout=120s
```

```bash
# 3. пул адресов + L2-анонс (диапазон из подсети kind, не пересекаясь с нодами)
cat <<EOF | kubectl apply -f -
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: kind-pool
  namespace: metallb-system
spec:
  addresses:
  - 10.89.0.200-10.89.0.250
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: kind-l2
  namespace: metallb-system
spec:
  ipAddressPools:
  - kind-pool
EOF
```

Проверка (изнутри кластера — работает; с хоста на macOS/podman — нет):
```bash
kubectl get svc lb                          # EXTERNAL-IP из пула, напр. 10.89.0.200
kubectl run lbtest --rm -i --restart=Never --image=curlimages/curl -- \
  curl -s http://10.89.0.200/               # ✅ ответ от Pod-а
```

---

## Шаг 5. Сравнение

```bash
kubectl get svc
# NAME   TYPE           CLUSTER-IP       EXTERNAL-IP   PORT(S)
# cip    ClusterIP      10.96.x.x        <none>        8080/TCP
# np     NodePort       10.96.x.x        <none>        8080:30080/TCP
# lb     LoadBalancer   10.96.x.x        localhost     8080:31755/TCP
```

LoadBalancer **построен поверх** NodePort, который **построен поверх** ClusterIP. Поэтому у LB есть и ClusterIP, и NodePort (даже если вы их не указывали).

---

## Шаг 6. EndpointSlice — список здоровых Pod-ов

```bash
kubectl get endpointslices
kubectl describe endpointslice <name>
# Раздел Endpoints — IP всех Pod-ов с матчингом selector
# Conditions:
#   Ready: true              ← только Ready Pod-ы получают трафик
```

Попробуйте удалить один Pod:
```bash
kubectl delete pod <pod-name>
# Через 2 секунды EndpointSlice обновится — там 4 Pod-а вместо 5
```

---

## Cleanup

```bash
kubectl delete -f loadbalancer.yaml -f nodeport.yaml -f clusterip.yaml -f deploy.yaml
```

---

## Что вы узнали

- 3 типа Service: ClusterIP (внутренний) / NodePort (порт ноды) / LoadBalancer (облачный LB)
- Каждый следующий type построен поверх предыдущего
- EndpointSlice — динамический список здоровых Pod-ов
- Только Pod-ы прошедшие readiness попадают в EndpointSlice
- Service ↔ Pod связь — через **label selector**
