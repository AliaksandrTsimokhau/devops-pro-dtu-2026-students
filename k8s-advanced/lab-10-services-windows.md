# Lab 10 (Windows) — Service types: ClusterIP, NodePort, LoadBalancer

**Цель:** разобраться с тремя основными типами Service, увидеть как они построены друг на друге. **Вариант для Windows (PowerShell + podman).**

**Время:** 25 минут
**Prerequisites:** Lab 08.

> **Окружение:** kind + podman на Windows. podman machine работает в WSL2.
> Команды `kubectl` идентичны Linux/macOS. Отличия — в синтаксисе оболочки
> (PowerShell) и в запуске `cloud-provider-kind` (Шаг 4a): на Windows нет `sudo`.

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

```powershell
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

```powershell
kubectl apply -f clusterip.yaml
kubectl get svc cip
kubectl describe svc cip
```

Обратите внимание на `Endpoints:` — там IP всех Pod-ов с label `chapter=services`.

```powershell
kubectl get endpointslices -l kubernetes.io/service-name=cip
```

### Проверка из кластера

```powershell
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

```powershell
kubectl apply -f nodeport.yaml
kubectl get svc np
```

Колонка PORT: `8080:30080/TCP`.

### Проверка

> На kind/podman нода живёт внутри VM, поэтому `<NODE-IP>` напрямую с Windows
> недостижим. Чтобы достучаться до NodePort, пробросьте порт при создании кластера
> через `extraPortMappings` (`containerPort: 30080` → `hostPort: 30080`) **или**
> используйте `kubectl port-forward`:

```powershell
kubectl port-forward svc/np 30080:8080
# в другом окне PowerShell:
curl.exe http://localhost:30080
```

> В PowerShell `curl` — это алиас `Invoke-WebRequest`. Используйте `curl.exe`,
> чтобы получить привычное поведение.

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

```powershell
kubectl apply -f loadbalancer.yaml
kubectl get svc lb
```

- **Docker Desktop**: `EXTERNAL-IP` = `localhost`
- **kind (podman)**: `<pending>` — пока не запущен провайдер LB (см. ниже)
- **Cloud**: реальный публичный IP через 30–60 сек

### Шаг 4a. Включаем LoadBalancer на kind + podman (Windows)

kind сам по себе **не реализует** LoadBalancer — Service остаётся в `<pending>`.
Нужен отдельный хостовый бинарь **`cloud-provider-kind`**: он следит за Service
типа LoadBalancer и поднимает контейнер-балансировщик (envoy) на каждый сервис.

> **Почему так на Windows:** ноды kind работают внутри VM (podman machine в WSL2).
> IP балансировщика **не маршрутизируется** с Windows-хоста. Поэтому обязателен
> флаг `--enable-lb-port-mapping` — он публикует сервис на `127.0.0.1:<порт>`.

Установка:
```powershell
go install sigs.k8s.io/cloud-provider-kind@latest
# или скачайте готовый бинарь со страницы релизов:
# https://github.com/kubernetes-sigs/cloud-provider-kind/releases
```

**Вариант A (рекомендуемый) — запуск внутри WSL2.** Так как ноды kind живут в WSL2,
проще запускать провайдер там же, как на Linux/macOS — через `sudo`:
```bash
# внутри дистрибутива WSL2:
sudo DOCKER_HOST="unix://$(podman machine inspect <MACHINE> --format '{{.ConnectionInfo.PodmanSocket.Path}}')" \
     KUBECONFIG="$HOME/.kube/config" \
     cloud-provider-kind --enable-lb-port-mapping
```

**Вариант B — запуск нативно на Windows.** Откройте PowerShell **от имени
администратора** (Run as administrator) — это обязательно, аналог `sudo`:
```powershell
# узнать сокет podman machine:
podman machine inspect <MACHINE> --format '{{.ConnectionInfo.PodmanSocket.Path}}'
$env:DOCKER_HOST = "npipe:////./pipe/<имя-пайпа>"   # из вывода выше
$env:KUBECONFIG  = "$HOME\.kube\config"
cloud-provider-kind --enable-lb-port-mapping
```

Подводные камни:
- **Права администратора обязательны** (Вариант B) либо `sudo` в WSL2 (Вариант A) —
  без них бинарь сразу падает с `please run this again with sudo`.
- Передавайте `DOCKER_HOST` и `KUBECONFIG` явно — провайдер должен видеть и сокет
  podman (где живут ноды kind), и kubeconfig.
- Для **rootful podman machine** провайдер видит сеть `kind`; rootless — нет.
- Запущенный провайдер держите в отдельном окне на всё время работы с LB.

Проверка:
```powershell
kubectl get svc lb                         # EXTERNAL-IP станет реальным за ~30 сек
# найдём проброшенный на localhost порт балансировщика:
podman --connection <MACHINE>-root ps --filter name=kindccm     # колонка PORTS: 0.0.0.0:NNNNN->8080
curl.exe http://127.0.0.1:NNNNN/           # трафик дойдёт до Pod-а
```

> `EXTERNAL-IP` напрямую с Windows **не курлится** — только через `127.0.0.1:<порт>`.

---

## Шаг 5. Сравнение

```powershell
kubectl get svc
# NAME   TYPE           CLUSTER-IP       EXTERNAL-IP   PORT(S)
# cip    ClusterIP      10.96.x.x        <none>        8080/TCP
# np     NodePort       10.96.x.x        <none>        8080:30080/TCP
# lb     LoadBalancer   10.96.x.x        10.89.x.x     8080:31755/TCP
```

LoadBalancer **построен поверх** NodePort, который **построен поверх** ClusterIP. Поэтому у LB есть и ClusterIP, и NodePort (даже если вы их не указывали).

---

## Шаг 6. EndpointSlice — список здоровых Pod-ов

```powershell
kubectl get endpointslices
kubectl describe endpointslice <name>
# Раздел Endpoints — IP всех Pod-ов с матчингом selector
# Conditions:
#   Ready: true              ← только Ready Pod-ы получают трафик
```

Попробуйте удалить один Pod:
```powershell
kubectl delete pod <pod-name>
# Через 2 секунды EndpointSlice обновится — там 4 Pod-а вместо 5
```

---

## Cleanup

```powershell
kubectl delete -f loadbalancer.yaml -f nodeport.yaml -f clusterip.yaml -f deploy.yaml
```

Провайдер LB остановите в его окне через `Ctrl+C`.

---

## Что вы узнали

- 3 типа Service: ClusterIP (внутренний) / NodePort (порт ноды) / LoadBalancer (облачный LB)
- Каждый следующий type построен поверх предыдущего
- EndpointSlice — динамический список здоровых Pod-ов
- Только Pod-ы прошедшие readiness попадают в EndpointSlice
- Service ↔ Pod связь — через **label selector**
- На kind + podman (Windows) LoadBalancer включается через `cloud-provider-kind`
  с `--enable-lb-port-mapping`, доступ — по `127.0.0.1:<порт>`
