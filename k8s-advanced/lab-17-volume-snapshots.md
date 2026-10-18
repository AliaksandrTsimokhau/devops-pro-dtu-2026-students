# Lab 17 — CSI Volume Snapshots: snapshot и restore PVC

**Цель:** делать point-in-time снимки PersistentVolume через CSI VolumeSnapshot и
восстанавливать данные в новый PVC из снимка.

**Время:** 35 минут
**Prerequisites:** Lab 16 (PV/PVC/StorageClass), Lab 21 (StatefulSet).

> Требуется CSI-драйвер **с поддержкой снапшотов**. Дефолтный в kind
> `local-path` (rancher) снапшоты **не умеет** — поэтому ставим тестовый
> csi-hostpath. В облаке снапшоты есть «из коробки» (EBS/GCE PD/Azure Disk).


---

## BLUF — три объекта снапшотов

| Объект | Аналогия с томами | Кто создаёт |
|---|---|---|
| **VolumeSnapshotClass** | как StorageClass | админ (1 раз) |
| **VolumeSnapshot** | как PVC (запрос) | пользователь |
| **VolumeSnapshotContent** | как PV (факт) | контроллер |

Снапшот — это **point-in-time** копия PVC на стороне хранилища. Из снапшота
можно создать **новый** PVC (`dataSource`) — типичный сценарий backup/restore,
клонирование окружений, «откат перед миграцией».

---

## Шаг 1. Снапшот-инфраструктура (cluster-wide, 1 раз)

Снапшот-CRD и контроллер **не входят** в кластер — это общий компонент:
```bash
S=https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/v8.6.0

# CRD: VolumeSnapshot / VolumeSnapshotContent / VolumeSnapshotClass
kubectl apply -f $S/client/config/crd/snapshot.storage.k8s.io_volumesnapshotclasses.yaml \
              -f $S/client/config/crd/snapshot.storage.k8s.io_volumesnapshotcontents.yaml \
              -f $S/client/config/crd/snapshot.storage.k8s.io_volumesnapshots.yaml

# snapshot-controller
kubectl apply -f $S/deploy/kubernetes/snapshot-controller/rbac-snapshot-controller.yaml
kubectl apply -f $S/deploy/kubernetes/snapshot-controller/setup-snapshot-controller.yaml
kubectl -n kube-system rollout status deploy/snapshot-controller --timeout=120s
```

---

## Шаг 2. CSI-драйвер с поддержкой снапшотов (csi-hostpath)

```bash
git clone --depth 1 --branch v1.17.0 \
  https://github.com/kubernetes-csi/csi-driver-host-path.git
cd csi-driver-host-path

# Официальный способ — скрипт (сам подтягивает RBAC всех sidecar-ов):
deploy/kubernetes-latest/deploy.sh

kubectl rollout status statefulset/csi-hostpathplugin --timeout=150s
kubectl get pod csi-hostpathplugin-0      # READY 8/8 (plugin + sidecar-ы)
```

> ⚠️ Если ставите манифесты **вручную** (без `deploy.sh`), а не скриптом — PVC
> зависнет в `Pending`, а в логах `csi-provisioner` будет
> `clusterrole "external-provisioner-runner" not found`. Манифест плагина создаёт
> только **RoleBinding-и**, а сами ClusterRole-ы живут в RBAC каждого sidecar-а
> (external-provisioner / attacher / resizer / snapshotter / health-monitor) —
> их надо применить отдельно. `deploy.sh` это делает за вас.

```bash
# StorageClass и VolumeSnapshotClass для hostpath
kubectl apply -f examples/csi-storageclass.yaml          # → csi-hostpath-sc
kubectl get volumesnapshotclass csi-hostpath-snapclass   # ставится драйвером
```

---

## Шаг 3. PVC с данными

```bash
kubectl apply -f - <<'EOF'
apiVersion: v1
kind: PersistentVolumeClaim
metadata: {name: data-pvc}
spec:
  storageClassName: csi-hostpath-sc
  accessModes: [ReadWriteOnce]
  resources: {requests: {storage: 1Gi}}
---
apiVersion: v1
kind: Pod
metadata: {name: writer}
spec:
  containers:
  - name: c
    image: busybox:1.37
    command: ["/bin/sh","-c","echo 'IMPORTANT DATA v1' > /data/file.txt; sleep 3600"]
    volumeMounts: [{name: vol, mountPath: /data}]
  volumes:
  - {name: vol, persistentVolumeClaim: {claimName: data-pvc}}
EOF
kubectl wait --for=condition=ready pod/writer --timeout=120s
kubectl exec writer -- cat /data/file.txt
# IMPORTANT DATA v1
```

---

## Шаг 4. Снять снапшот

```bash
kubectl apply -f - <<'EOF'
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshot
metadata: {name: data-snap}
spec:
  volumeSnapshotClassName: csi-hostpath-snapclass
  source:
    persistentVolumeClaimName: data-pvc
EOF

# Ждём, пока снапшот готов к использованию
kubectl get volumesnapshot data-snap -w
kubectl get volumesnapshot data-snap -o custom-columns=\
'NAME:.metadata.name,READY:.status.readyToUse,SRC:.spec.source.persistentVolumeClaimName,SIZE:.status.restoreSize'
# data-snap   true   data-pvc   1Gi
```

> `readyToUse: true` означает: снимок зафиксирован на стороне хранилища. Контроллер
> создал парный `VolumeSnapshotContent` (как PV для PVC).

---

## Шаг 5. Restore — новый PVC из снапшота

Сымитируем порчу данных и восстановимся из снимка:
```bash
# Портим оригинал ПОСЛЕ снапшота
kubectl exec writer -- /bin/sh -c "echo 'CORRUPTED v2' > /data/file.txt"

# Создаём НОВЫЙ PVC из снапшота (dataSource)
kubectl apply -f - <<'EOF'
apiVersion: v1
kind: PersistentVolumeClaim
metadata: {name: restored-pvc}
spec:
  storageClassName: csi-hostpath-sc
  dataSource:
    name: data-snap
    kind: VolumeSnapshot
    apiGroup: snapshot.storage.k8s.io
  accessModes: [ReadWriteOnce]
  resources: {requests: {storage: 1Gi}}
---
apiVersion: v1
kind: Pod
metadata: {name: restored-reader}
spec:
  containers:
  - name: c
    image: busybox:1.37
    command: ["/bin/sh","-c","sleep 3600"]
    volumeMounts: [{name: vol, mountPath: /data}]
  volumes:
  - {name: vol, persistentVolumeClaim: {claimName: restored-pvc}}
EOF
kubectl wait --for=condition=ready pod/restored-reader --timeout=120s

kubectl exec restored-reader -- cat /data/file.txt
# IMPORTANT DATA v1     ← данные на момент снапшота, НЕ "CORRUPTED v2"
```

✅ Восстановленный PVC содержит данные **на момент снимка** (`v1`), а не текущее
испорченное состояние оригинала (`v2`). Это и есть backup/restore через снапшоты.

---

## Шаг 6. В production / облаке

- **local-path** (дефолт kind) снапшоты **не поддерживает** — поэтому здесь
  csi-hostpath (тестовый драйвер).
- В облаке CSI-драйверы дисков умеют снапшоты нативно, со своим
  `VolumeSnapshotClass`:
  - AWS EBS → `ebs.csi.aws.com`
  - GCP PD → `pd.csi.storage.gke.io`
  - Azure Disk → `disk.csi.azure.com`
- Снапшоты обычно **incremental** на стороне провайдера (дёшево/быстро), но это
  **не замена** off-site backup — лежат в том же регионе/аккаунте. Для полноценного
  бэкапа кластера смотрите **Velero** (снапшоты + метаданные объектов).

---

## Cleanup

```bash
kubectl delete pod writer restored-reader --ignore-not-found --now
kubectl delete volumesnapshot data-snap --ignore-not-found
kubectl delete pvc data-pvc restored-pvc --ignore-not-found
# драйвер/контроллер (если больше не нужны) — см. deploy/util/destroy-hostpath.sh
```

---

## Что вы узнали

- 3 объекта: **VolumeSnapshotClass** (как SC), **VolumeSnapshot** (как PVC,
  запрос), **VolumeSnapshotContent** (как PV, факт).
- Снапшот — **point-in-time** копия PVC; `readyToUse: true` = зафиксирован.
- Restore — новый PVC с `dataSource: {kind: VolumeSnapshot}`; данные на момент
  снимка.
- Нужен CSI-драйвер **с поддержкой снапшотов** + установленные снапшот-CRD и
  snapshot-controller (cluster-wide).
- `local-path` снапшоты не умеет; в облаке — нативно (EBS/PD/Azure Disk).
- Снапшоты ≠ off-site backup; для полного бэкапа — Velero.
