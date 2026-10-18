# Lab 04 — Multi-container Pod (init + sidecar)

**Цель:** запустить Pod с init-контейнером и sidecar-контейнером, увидеть как они взаимодействуют с main app.

**Время:** 20–30 минут
**Prerequisites:** Lab 03.

---

## Часть A — Init container

### Pod, который ждёт появления Service

`initpod.yaml`:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: initpod
  labels:
    app: initializer
spec:
  initContainers:
  - name: init-ctr
    image: busybox:1.28.4
    command: ['sh', '-c', 'until nslookup k8sbook; do echo waiting for k8sbook service; sleep 1; done; echo Service found!']
  containers:
  - name: web-ctr
    image: nigelpoulton/web-app:1.0
    ports:
    - containerPort: 8080
```

```bash
kubectl apply -f initpod.yaml
kubectl get pods --watch
```

Состояние:
- `Init:0/1` — init-контейнер ещё работает
- `PodInitializing` — init завершился, main стартует
- `Running` — main работает

**На данный момент Pod зависнет в Init:0/1**, потому что Service `k8sbook` ещё не существует.

### Создадим Service — init-контейнер завершится

`initsvc.yaml`:
```yaml
apiVersion: v1
kind: Service
metadata:
  name: k8sbook
spec:
  selector:
    app: nonexistent
  ports:
  - port: 80
```

```bash
kubectl apply -f initsvc.yaml
kubectl get pods --watch
```

Через 5–10 секунд init-контейнер обнаружит Service и завершится, Pod перейдёт в Running.

```bash
kubectl describe pod initpod
# раздел Init Containers покажет terminated с exit code 0
```

---

## Часть B — Sidecar pattern

### Sidecar, синкающий контент из Git

`sidecarpod.yaml`:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: git-sync
  labels:
    app: git-sync
spec:
  initContainers:
  # Sidecar — синкает git
  - name: ctr-sync
    image: k8s.gcr.io/git-sync/git-sync:v3.6.5
    restartPolicy: Always
    env:
    - name: GIT_SYNC_REPO
      value: "https://github.com/nigelpoulton/ps-sidecar.git"
    - name: GIT_SYNC_BRANCH
      value: "main"
    - name: GIT_SYNC_DEST
      value: "html"
    - name: GIT_SYNC_ROOT
      value: "/tmp/git"
    volumeMounts:
    - name: html
      mountPath: /tmp/git
  containers:
  # Main app — отдаёт content
  - name: ctr-web
    image: nginx:1.27-alpine
    ports:
    - containerPort: 80
    volumeMounts:
    - name: html
      mountPath: /usr/share/nginx/html
  volumes:
  - name: html
    emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: svc-sidecar
spec:
  type: LoadBalancer
  selector:
    app: git-sync
  ports:
  - port: 8080
    targetPort: 80
```

```bash
kubectl apply -f sidecarpod.yaml
kubectl get pods
kubectl get svc svc-sidecar
```

В колонке `EXTERNAL-IP` будет `localhost` (Docker Desktop) или IP.

Откройте в браузере: `http://localhost:8080` — должна быть страничка "This is version 1.0".

### Посмотрите состояние обоих контейнеров

```bash
kubectl describe pod git-sync
# Containers section — будут оба
```

Логи по контейнеру:
```bash
kubectl logs git-sync -c ctr-web
kubectl logs git-sync -c ctr-sync
```

---

## Cleanup

```bash
kubectl delete -f sidecarpod.yaml
kubectl delete -f initpod.yaml
kubectl delete -f initsvc.yaml
```

---

## Что вы узнали

- `initContainers` запускаются **до** обычных контейнеров и должны завершиться успехом
- Native Sidecar — задаётся как `initContainer` с `restartPolicy: Always`. Он стартует до main app, но продолжает работать параллельно с ним.
- Контейнеры одного Pod-а делят `volumes` (тут — `emptyDir`)
- `kubectl logs -c <container>` — логи конкретного контейнера в multi-container Pod-е
- Sidecar pattern полезен для: log forwarding, git sync, service mesh proxies
