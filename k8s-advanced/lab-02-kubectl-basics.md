# Lab 02 — kubectl basics and kubeconfig

**Цель:** уверенно работать с `kubectl` и `kubeconfig`: контексты, namespace-switching, инспекция кластера.

**Время:** 15–20 минут
**Prerequisites:** Lab 01 (рабочий локальный кластер).

---

## Шаг 1. Inspect cluster

```bash
kubectl version --client                # клиент
kubectl cluster-info                    # endpoint API server
kubectl get nodes -o wide               # ноды + их версии + IP
kubectl api-resources | wc -l           # сколько типов ресурсов
kubectl api-resources --namespaced=true | head
kubectl api-resources --namespaced=false
```

**Что замечаем:**
- `NAMESPACED` колонка: `true` — namespaced, `false` — cluster-scoped
- `Node`, `PersistentVolume`, `StorageClass` — cluster-scoped
- `Pod`, `Service`, `Deployment` — namespaced

---

## Шаг 2. Working with kubeconfig

```bash
kubectl config view                      # текущий kubeconfig (секреты замаскированы)
kubectl config get-contexts              # все контексты
kubectl config current-context           # активный
```

### Полный путь до kubeconfig

macOS/Linux: `~/.kube/config`
Windows: `%USERPROFILE%\.kube\config`

```bash
cat ~/.kube/config | head -30
```

---

## Шаг 3. Switch context (если их несколько)

```bash
# Переключиться на конкретный
kubectl config use-context kind-dtu      # или docker-desktop, minikube, etc.

# Сделать namespace по умолчанию
kubectl config set-context --current --namespace=kube-system

# Теперь get pods без -n покажет kube-system
kubectl get pods

# Вернуться в default
kubectl config set-context --current --namespace=default
```

---

## Шаг 4. Imperative commands

```bash
# Поднять Pod на лету (без YAML)
kubectl run mypod --image=nginx --restart=Never

# Посмотреть
kubectl get pods
kubectl describe pod mypod

# Удалить
kubectl delete pod mypod
```

> Это **imperative** подход — для экспериментов. В production — declarative.

---

## Шаг 5. Declarative — YAML + apply

`hello.yaml`:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: hello
  labels:
    app: hello
spec:
  containers:
  - name: hello
    image: nginx:1.27-alpine
    ports:
    - containerPort: 80
```

```bash
kubectl apply -f hello.yaml
kubectl get pods --show-labels
kubectl describe pod hello
kubectl delete -f hello.yaml
```

---

## Шаг 6. Useful kubectl flags

```bash
kubectl get pods -A                       # all namespaces
kubectl get pods -o wide                  # extra columns
kubectl get pods -o yaml | head -50       # full spec
kubectl get pods -o json | jq '.items[0].metadata.name'

kubectl get pods --watch                  # follow changes
kubectl get pods --selector=app=hello     # by label

kubectl explain pod.spec.containers       # docs in your terminal
kubectl explain pod.spec.containers.resources --recursive
```

---

## Шаг 7. Aliases (must-have)

Добавьте в `~/.zshrc` или `~/.bashrc`:
```bash
alias k=kubectl
alias kgp='kubectl get pods'
alias kgs='kubectl get svc'
alias kgn='kubectl get nodes'
alias kdp='kubectl describe pod'
alias kaf='kubectl apply -f'
alias kd='kubectl delete'
```

И установите autocompletion:
```bash
source <(kubectl completion zsh)         # для zsh
source <(kubectl completion bash)        # для bash
```

---

## Что вы узнали

- `kubectl` — это HTTP client поверх REST API
- `kubeconfig` хранит кластеры + users + контексты
- Imperative (`kubectl run`) vs declarative (`kubectl apply -f`)
- `kubectl explain` — встроенная документация
- `-A`, `-o wide`, `--watch`, `--selector` — самые полезные флаги
