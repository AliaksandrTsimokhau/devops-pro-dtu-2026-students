# Lab 16 — PV / PVC / StorageClass: dynamic provisioning

**Цель:** создать PVC, использовать StorageClass для динамического создания PV, замонтировать в Pod.

**Время:** 20 минут
**Prerequisites:** module 02, Lab 03 — первый Pod; для фона по PVC/StorageClass — module 02, Lab 20.

> ⚠️ На kind / Docker Desktop по умолчанию доступен только `standard` StorageClass (с local-path или hostPath). В production — это AWS EBS / GCP PD / Azure Disk.
>
> minikube: default-класс `standard` использует провиженер `k8s.io/minikube-hostpath` с binding `Immediate` (проверьте: `kubectl get sc`). Кастомный класс `fast` из Шага 6 с `rancher.io/local-path` на minikube останется `Pending` — на minikube замените `provisioner` на `k8s.io/minikube-hostpath` (проверь: `kubectl get sc`) или выполняйте лабу только на kind.

---

## Шаг 1. Какие StorageClass-ы есть в кластере?

```bash
kubectl get sc
# NAME                 PROVISIONER             RECLAIMPOLICY   VOLUMEBINDINGMODE
# standard (default)   rancher.io/local-path   Delete          WaitForFirstConsumer
```

(или похожее, зависит от вашего kind/Docker Desktop/cloud setup)

```bash
kubectl describe sc standard
```

---

## Шаг 2. Создаём PVC

`pvc.yaml`:
```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: my-pvc
spec:
  accessModes:
  - ReadWriteOnce
  storageClassName: standard
  resources:
    requests:
      storage: 1Gi
```

```bash
kubectl apply -f pvc.yaml
kubectl get pvc my-pvc
```

**С `WaitForFirstConsumer`** — STATUS `Pending`, потому что нет Pod-а который бы его использовал.

```bash
kubectl get pv                    # пусто пока никто не клеймит
```

---

## Шаг 3. Pod, использующий PVC

`pod-with-pvc.yaml`:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: data-pod
spec:
  containers:
  - name: ubuntu
    image: ubuntu:latest
    command: ["/bin/bash", "-c", "sleep 3600"]
    volumeMounts:
    - mountPath: /data
      name: data
  volumes:
  - name: data
    persistentVolumeClaim:
      claimName: my-pvc
```

```bash
kubectl apply -f pod-with-pvc.yaml
kubectl get pods
kubectl get pvc my-pvc
# Теперь STATUS = Bound

kubectl get pv
# Появится PV с автогенерированным именем pvc-<uuid>
```

---

## Шаг 4. Запись в volume

```bash
kubectl exec -it data-pod -- bash
# внутри:
df -h /data
echo "Hello, persistent storage!" > /data/hello.txt
ls -la /data
exit
```

---

## Шаг 5. Volume переживает Pod

Удалим Pod (но НЕ PVC):
```bash
kubectl delete pod data-pod
```

Создадим новый Pod, использующий тот же PVC:
```bash
kubectl apply -f pod-with-pvc.yaml      # тот же манифест
kubectl exec data-pod -- cat /data/hello.txt
# Hello, persistent storage!
```

> Volume сохранился! Файл вернулся.

---

## Шаг 6. Создадим свою StorageClass

`sc-custom.yaml`:
```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: fast
provisioner: rancher.io/local-path        # local-path provisioner; для cloud — ebs.csi.aws.com / pd.csi.storage.gke.io
volumeBindingMode: WaitForFirstConsumer
reclaimPolicy: Retain
```

```bash
kubectl apply -f sc-custom.yaml
kubectl get sc
```

Создайте PVC, использующий `fast`:
```yaml
# pvc-fast.yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata: {name: pvc-fast}
spec:
  accessModes: [ReadWriteOnce]
  storageClassName: fast
  resources:
    requests: {storage: 1Gi}
```

```bash
kubectl apply -f pvc-fast.yaml
```

---

## Шаг 7. Retain — volume переживает удаление PVC

```bash
# Привязаться к Pod-у
kubectl run test --image=alpine --restart=Never --overrides='
{
  "spec": {
    "containers": [{
      "name": "test",
      "image": "alpine",
      "command": ["sleep", "60"],
      "volumeMounts": [{"mountPath": "/data", "name": "data"}]
    }],
    "volumes": [{"name": "data", "persistentVolumeClaim": {"claimName": "pvc-fast"}}]
  }
}'

kubectl wait pod/test --for=condition=Ready --timeout=120s

# Подождать пока PV создастся
kubectl get pv
# Удалить Pod и PVC
kubectl delete pod test
kubectl delete pvc pvc-fast

# Volume не удалён — потому что reclaimPolicy: Retain
kubectl get pv
# Status: Released — но физический volume остался, нужно удалять вручную:
kubectl delete pv <pv-name>
```

> В production: `Retain` для критических данных, `Delete` для эфемерных.

---

## Cleanup

```bash
kubectl delete --ignore-not-found -f pod-with-pvc.yaml
kubectl delete --ignore-not-found -f pvc.yaml
kubectl delete --ignore-not-found -f pvc-fast.yaml
kubectl delete --ignore-not-found pod test
kubectl delete --ignore-not-found -f sc-custom.yaml
# PV с reclaimPolicy: Retain, оставшиеся после удаления pvc-fast (Released)
kubectl get pv -o jsonpath='{range .items[?(@.spec.claimRef.name=="pvc-fast")]}{.metadata.name}{"\n"}{end}' \
  | xargs -r kubectl delete pv --ignore-not-found
```

---

## Что вы узнали

- Цепочка: Pod → PVC → PV → StorageClass → CSI driver
- `WaitForFirstConsumer` — PV создаётся только когда Pod запрашивает
- `accessModes`: RWO (один node), RWX (много nodes), ROX (read-only mass)
- `reclaimPolicy`: `Delete` (default) vs `Retain` (production data)
- Volume переживает Pod (если использован PVC, не emptyDir)
- StorageClass — `immutable` после создания
