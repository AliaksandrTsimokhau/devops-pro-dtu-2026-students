# Lab 22 — RBAC + ServiceAccount

**Цель:** создать ServiceAccount, Role, RoleBinding и проверить через `kubectl auth can-i`.

**Время:** 25 минут
**Prerequisites:** module 02, Lab 17 — namespaces, module 02, Lab 26 — RoleBinding.

---

## Шаг 1. Прошлое — admin кластера

```bash
kubectl auth whoami           # K8s 1.27+
kubectl auth can-i '*' '*'    # cluster-admin может всё
```

Скорее всего — да. Сейчас мы создадим **ограниченного** subject.

---

## Шаг 2. Namespace и ServiceAccount

`rbac-setup.yaml`:
```yaml
apiVersion: v1
kind: Namespace
metadata: {name: app}
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: developer
  namespace: app
```

```bash
kubectl apply -f rbac-setup.yaml
kubectl get sa -n app
```

---

## Шаг 3. Role — права для SA

`role.yaml`:
```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  namespace: app
  name: pod-reader
rules:
- apiGroups: [""]                  # core
  resources: ["pods"]
  verbs: ["get", "list", "watch"]
- apiGroups: [""]
  resources: ["pods/log"]
  verbs: ["get"]
```

```bash
kubectl apply -f role.yaml
kubectl describe role -n app pod-reader
```

---

## Шаг 4. RoleBinding — связываем

`rolebinding.yaml`:
```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: read-pods
  namespace: app
subjects:
- kind: ServiceAccount
  name: developer
  namespace: app
roleRef:
  kind: Role
  name: pod-reader
  apiGroup: rbac.authorization.k8s.io
```

```bash
kubectl apply -f rolebinding.yaml
```

---

## Шаг 5. Проверим through can-i

```bash
# От имени SA
kubectl auth can-i list pods -n app --as=system:serviceaccount:app:developer
# yes

kubectl auth can-i create pods -n app --as=system:serviceaccount:app:developer
# no

# В другом namespace?
kubectl auth can-i list pods -n default --as=system:serviceaccount:app:developer
# no
```

---

## Шаг 6. Запустим Pod от имени SA

`app-pod.yaml`:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  namespace: app
  labels:
    app: rbac
spec:
  replicas: 1
  selector:
    matchLabels: {app: rbac}
  template:
    metadata:
      labels: {app: rbac}
    spec:
      serviceAccountName: developer    # ← SA для аутентификации
      containers:
      - name: ctr
        image: alpine/k8s:1.35.9     # проверь: тег есть на Docker Hub (alpine/k8s) и совпадает с версией кластера; образ с shell и sleep
        command: ["sleep", "3600"]
```

```bash
kubectl apply -f app-pod.yaml
kubectl get pods -n app
```

### Проверим что внутри Pod-а

```bash
POD=$(kubectl get pods -n app -l app=rbac -o jsonpath='{.items[0].metadata.name}')
kubectl exec -n app $POD -- ls /var/run/secrets/kubernetes.io/serviceaccount/
# ca.crt  namespace  token

kubectl exec -n app $POD -- cat /var/run/secrets/kubernetes.io/serviceaccount/namespace
# app
```

### Pod может делать kubectl

```bash
# Внутри Pod-а:
kubectl exec -n app $POD -- kubectl get pods -n app
# должно работать (есть Role pod-reader)

kubectl exec -n app $POD -- kubectl get deployments -n app
# Error: forbidden — у нас нет права читать deployments
```

---

## Шаг 7. ClusterRole — для cluster-wide прав

`clusterrole.yaml`:
```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: namespace-lister
rules:
- apiGroups: [""]
  resources: ["namespaces"]
  verbs: ["get", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: developer-list-ns
subjects:
- kind: ServiceAccount
  name: developer
  namespace: app
roleRef:
  kind: ClusterRole
  name: namespace-lister
  apiGroup: rbac.authorization.k8s.io
```

```bash
kubectl apply -f clusterrole.yaml

# Теперь SA может listить namespaces
kubectl exec -n app $POD -- kubectl get ns
```

---

## Шаг 8. Best practice — `automountServiceAccountToken: false`

Если Pod **не должен** ходить в API:
```yaml
spec:
  automountServiceAccountToken: false
  containers:
  - name: app
    image: ...
```

Так не будет залезать в `/var/run/secrets/.../token` — никаких credentials внутри.

---

## Cleanup

```bash
kubectl delete -f app-pod.yaml
kubectl delete -f rolebinding.yaml
kubectl delete -f role.yaml
kubectl delete -f clusterrole.yaml
kubectl delete -f rbac-setup.yaml         # удалит namespace и SA каскадно
```

---

## Что вы узнали

- RBAC = 4 объекта: Role / ClusterRole / RoleBinding / ClusterRoleBinding
- Role + RoleBinding — права в namespace
- ClusterRole + ClusterRoleBinding — права во всём кластере
- ClusterRole + RoleBinding — переиспользуем cluster-определение в namespace
- ServiceAccount — identity для Pod-а в API
- `kubectl auth can-i --as=...` — тестируем права
- `automountServiceAccountToken: false` — отключить SA token если не нужен
- Pod-token автоматически в `/var/run/secrets/kubernetes.io/serviceaccount/token`
