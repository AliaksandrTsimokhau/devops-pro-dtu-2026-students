# Lab 01 — Install a local Kubernetes cluster

**Цель:** поднять рабочий локальный кластер Kubernetes и проверить связь с ним через `kubectl`.

**Время:** 20–30 минут

---

## Prerequisites

- macOS / Linux / Windows
- ≥ 6 ГБ свободной RAM (multi-node kind = 4 контейнера-ноды; лучше 8 ГБ)
- Docker Desktop, Podman Desktop или другой container engine

> 💡 Дальнейшие лабы рассчитаны на **один multi-node kind кластер** (1
> control-plane + 3 worker), который мы поднимаем здесь и используем до конца
> курса. Особенно он нужен для Lab 27 (scheduling). Создаём один раз — больше
> топологию не трогаем.

---

## Optional — Podman Desktop вместо Docker Desktop

[Podman Desktop](https://podman-desktop.io/) — open-source альтернатива Docker Desktop. Подходит для локальной работы с контейнерами и для запуска `kind` через Podman provider.

### Установка Podman Desktop

macOS:
```bash
brew install --cask podman-desktop
```

Windows:
```powershell
winget install -e --id RedHat.Podman-Desktop
```

Linux (Flatpak / Flathub):
```bash
flatpak install flathub io.podman_desktop.PodmanDesktop
```

Альтернатива для всех OS: скачать installer с официальной страницы https://podman-desktop.io/downloads.

### Первый запуск

1. Откройте **Podman Desktop**.
2. Перейдите в **Settings → Resources**.
3. В блоке **Podman** нажмите **Create new**.
4. Задайте ресурсы для VM:
   - CPU: `2–4`
   - Memory: `4 GB` или больше
   - Disk: `20 GB` или больше
5. Нажмите **Create** и дождитесь статуса `Running`.

На macOS и Windows Podman engine работает внутри Linux VM, поэтому Podman machine должна быть запущена перед `kind`, `minikube` или обычными `podman run`.

Проверка:
```bash
podman machine list
podman info
podman run --rm quay.io/podman/hello
```

Если предпочитаете CLI:
```bash
podman machine init --cpus 4 --memory 4096 --disk-size 20
podman machine start
```

### Если используете `kind` поверх Podman

На Linux/macOS:
```bash
KIND_EXPERIMENTAL_PROVIDER=podman kind create cluster --config kind-config.yaml
kubectl get nodes
```

На Windows/WSL для `kind` может потребоваться rootful Podman machine:
```bash
podman machine stop
podman machine set --rootful
podman machine start
```

Official references:
- Podman Desktop installation: https://podman-desktop.io/docs/installation
- Podman Desktop downloads: https://podman-desktop.io/downloads
- Podman machine setup: https://podman-desktop.io/docs/podman/creating-a-podman-machine
- kind with Podman: https://kind.sigs.k8s.io/docs/user/rootless/

---

## Вариант 1 — Docker Desktop (самый простой)

1. Установите **Docker Desktop**: https://www.docker.com/products/docker-desktop
2. Откройте **Settings → Kubernetes** → ☑ **Enable Kubernetes** → **Apply & restart**.
3. Подождите 1–3 минуты, пока кластер запустится.
4. Убедитесь:
   ```bash
   kubectl version
   kubectl cluster-info
   kubectl get nodes
   ```

**Ожидаемый вывод:** одна нода в состоянии `Ready`.

> ⚠️ Встроенный Kubernetes в Docker Desktop — **single-node**, на нём не
> сработает Lab 27 (scheduling). Для курса рекомендуем **Вариант 2 (kind)** —
> он даёт настоящий multi-node кластер.

---

## Вариант 2 — kind (Kubernetes in Docker)

[kind](https://kind.sigs.k8s.io/) — самый быстрый способ поднять кластер с несколькими нодами.

### Установка

macOS:
```bash
brew install kind kubectl
```

Linux:
```bash
[ $(uname -m) = x86_64 ] && curl -Lo ./kind https://kind.sigs.k8s.io/dl/v0.22.0/kind-linux-amd64
chmod +x ./kind && sudo mv ./kind /usr/local/bin/
```

### Создаём multi-node кластер

`kind-config.yaml` — 1 control-plane + **3 worker** (с метками зон для topology
spread в Lab 27):
```yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: kind-dtu
nodes:
  - role: control-plane
  - role: worker
    labels: {topology.kubernetes.io/zone: zone-a}
  - role: worker
    labels: {topology.kubernetes.io/zone: zone-b}
  - role: worker
    labels: {topology.kubernetes.io/zone: zone-c}
```

> Почему именно 3 worker? Lab 27 (scheduling) использует pod anti-affinity с
> `replicas: 3` и `requiredDuringScheduling` по hostname — для этого нужно
> **3 schedulable ноды** (control-plane в kind затейнчен и не считается).

```bash
# Docker / Colima:
kind create cluster --config kind-config.yaml

# Podman provider (см. раздел выше):
KIND_EXPERIMENTAL_PROVIDER=podman kind create cluster --config kind-config.yaml

kubectl get nodes
```

Должны увидеть **4 ноды** (1 control-plane + 3 worker). Контекст — `kind-kind-dtu`
(kind добавляет префикс `kind-` к имени `kind-dtu`).

---

## Вариант 3 — minikube

```bash
brew install minikube              # macOS
minikube start --nodes=4 --driver=docker   # 4 ноды для Lab 27 (scheduling)
kubectl get nodes
```

> Лабы по scheduling (Lab 27) написаны и проверены на **kind**. minikube тоже
> работает, но имена нод и taint-ы control-plane могут отличаться.

---

## Проверка кластера

```bash
# Все компоненты кластера
kubectl get pods -n kube-system

# Какой контекст активен
kubectl config current-context

# Все доступные ресурсы
kubectl api-resources | head -30
```

---

## Cleanup (после следующих лабораторных)

```bash
# Docker Desktop: Settings → Kubernetes → Disable / Reset
# kind:
kind delete cluster --name kind-dtu
# minikube:
minikube delete
```

---

## Что вы узнали

- 3 способа получить локальный K8s
- Команды `kubectl version`, `cluster-info`, `get nodes`
- Multi-node кластер через kind (приближено к production-сетапу)
- Что control-plane компоненты живут в `kube-system` namespace
