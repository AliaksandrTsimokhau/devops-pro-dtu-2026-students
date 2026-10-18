# Lab 34 — Disaster Recovery: три сценария, в которых бэкап ≠ восстановление

**Цель:** на двух kind-кластерах воспроизвести три отказа, в которых бэкап есть,
а восстановиться нельзя: пустой бэкап, GitOps без данных и
рассинхронизация нескольких томов.

**Время:** 120 минут (самостоятельно)
**Повторный прогон** — только после полного Cleanup (имена кластеров, подов и бэкапов фиксированы).
**Prerequisites:**
- [lab-17-volume-snapshots](lab-17-volume-snapshots.md) (модуль 6, справочная: не в порядке курса; нужны VolumeSnapshot и snapshot-класс). Номер 17 здесь не путать с lab-17-namespaces из модуля 2.
- [lab-21-statefulset](lab-21-statefulset.md) (модуль 6).
- Только podman: Docker не используется. kind запускается на podman через `KIND_EXPERIMENTAL_PROVIDER=podman`. Нужны также `velero` (CLI, версия v1.18.x) и `jq`.
- ~6 GiB RAM для podman-машины (проверьте: `podman machine inspect --format '{{.Resources.Memory}}'`; менять память — **до** лабы: `podman machine stop` остановит все ваши kind-кластеры).
**Источник:** [CNCF — Kubernetes disaster recovery: three reproducible failure scenarios](https://www.cncf.io/blog/2026/09/10/kubernetes-disaster-recovery-guidance-from-three-reproducible-failure-scenarios/)

> Лаба использует **kind на podman**. Все команды помечены контекстом
> (`kind-dr-prod`, `kind-dr-recovery`) — другие ваши кластеры не затрагиваются.
> Все выводы ниже — реальные захваты из этой лабы (версии: kind node v1.36.4,
> Velero v1.18.4, aws-plugin v1.14.4, external-snapshotter v8.6.0, csi-hostpath v1.18.0).

---

## BLUF

| Сценарий | Что ломается | Урок |
|---|---|---|
| **1. Бэкап без данных** | `Completed` не значит «байты тома ушли из кластера» | проверяйте `DataUpload.bytesDone`, а не статус |
| **2. GitOps-ловушка** | Git хранит *декларации*, не *состояние* | нужны оба: Git (intent) + backup (state) |
| **3. Torn snapshot** | два снапшота с разницей в секунды дают состояние, которого не было | `VolumeGroupSnapshot` — один срез на все тома |

**Порядок выполнения: 1 → 3 → 2** — сценарий 2 выключает prod, а сценарию 3 нужен живой prod.

**RPO** — сколько данных можно потерять (возраст последней точки восстановления); **RTO** — время от аварии до *проверенных* данных.

Четыре слоя восстановления: **кластер → данные → приложение → трафик**. Каждый слой
по отдельности восстанавливается нормально; ломается на **стыках**. Velero и GitOps
восстанавливают только два слоя и **не создают** сам кластер (это Terraform / Cluster API).

```
 prod (kind)                      recovery (kind)
 ┌────────────┐                   ┌────────────────┐
 │ postgres   │──Velero──┐        │ Velero (пусто) │
 │ ledger     │          ▼        │ git apply ─► пустая БД
 └────────────┘     ┌─────────┐   └───────▲────────┘
   podman stop      │ S3 store│───────────┘ restore
                    │(podman) │   вне ОБОИХ кластеров
                    └─────────┘
```

---

## Шаг 0. Подготовка: S3-хранилище вне кластеров

Хранилище бэкапов должно жить **вне обоих кластеров**, иначе потеря кластера
уносит и точки восстановления. Поднимаем S3 в отдельном podman-контейнере.

> Официальные образы MinIO больше не публикуются, поэтому берём **SeaweedFS**
> (S3-совместимый). Два нюанса, на которых легко застрять:
> 1. Velero (kopia) подписывает запросы → нужен **S3-пользователь** (`s3.json`).
> 2. Поды kind **не резолвят** имена podman-контейнеров → фиксируем **IP**.

```bash
export KIND_EXPERIMENTAL_PROVIDER=podman
mkdir -p ~/dr-lab/git/guestbook && cd ~/dr-lab
kubectl config current-context > ~/dr-lab/ORIG_CTX 2>/dev/null    # вернём в Cleanup

cat > s3.json <<'EOF'
{"identities":[{"name":"velero","credentials":[{"accessKey":"velero","secretKey":"velero12345"}],"actions":["Admin","Read","Write","List","Tagging"]}]}
EOF
printf '[default]\naws_access_key_id=velero\naws_secret_access_key=velero12345\n' > velero-creds

# сеть "kind" создаётся при первом kind create cluster — создадим кластеры до S3
cat > kind-dr.yaml <<'EOF'
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
EOF
kind create cluster --name dr-prod     --image kindest/node:v1.36.4 --config kind-dr.yaml
kind create cluster --name dr-recovery --image kindest/node:v1.36.4 --config kind-dr.yaml

# фиксированный IP в IPv4-подсети сети kind (/24 → .250); у сети есть и IPv6-подсеть — берём только IPv4
SUBNET=$(podman network inspect kind | jq -r '.[0].subnets[].subnet | select(test("^[0-9.]+/"))' | head -1)
export S3_IP=${SUBNET%.0/*}.250 ; echo "S3_IP=$S3_IP"      # например 10.89.0.250

podman run -d --name dr-s3 --network kind --ip $S3_IP -p 19000:8333 \
  -v $PWD/s3.json:/etc/s3.json:ro docker.io/chrislusf/seaweedfs:4.48 \
  server -dir=/data -s3 -s3.config=/etc/s3.json -volume.max=0 -master.volumeSizeLimitMB=128

# ждём готовности S3 и создаём bucket (повторяем, пока не получится)
s3cli(){ podman run --rm --network kind -e AWS_ACCESS_KEY_ID=velero -e AWS_SECRET_ACCESS_KEY=velero12345 \
  docker.io/amazon/aws-cli:<проверь тег> --endpoint-url http://$S3_IP:8333 --region local "$@"; }
for i in $(seq 1 30); do s3cli s3 mb s3://velero 2>/dev/null; s3cli s3 ls 2>/dev/null | grep -q velero && break; sleep 4; done
s3cli s3 ls          # должен показать bucket velero

brew install velero        # CLI; Linux/WSL: tarball с GitHub Releases
velero version --client-only   # ожидаем v1.18.x (того же минора, что и сервер ниже)
```

> ⚠️ `S3_IP` понадобится в новых терминалах — задайте его заново (`podman inspect dr-s3`
> покажет адрес). Если пересоздавали S3 **после** установки Velero: поправьте
> `s3Url` в `BackupStorageLocation` и удалите stale `BackupRepository`
> (`kubectl -n velero delete backuprepositories --all`), иначе будет
> `The specified bucket does not exist`.

---

## Шаг 1. Prod: снапшот-стек + csi-hostpath

Data mover берёт CSI-снапшот и выгружает байты в S3 — значит, на prod нужен драйвер
со снапшотами (как в [lab-17-volume-snapshots](lab-17-volume-snapshots.md)), плюс **group-snapshot** для сценария 3.

```bash
kp(){ kubectl --context kind-dr-prod "$@"; }
S=https://raw.githubusercontent.com/kubernetes-csi/external-snapshotter/v8.6.0

# CRD: обычные + group (v1 отдаётся, storage-версия v1beta2)
kp apply \
  -f $S/client/config/crd/snapshot.storage.k8s.io_volumesnapshotclasses.yaml \
  -f $S/client/config/crd/snapshot.storage.k8s.io_volumesnapshotcontents.yaml \
  -f $S/client/config/crd/snapshot.storage.k8s.io_volumesnapshots.yaml \
  -f $S/client/config/crd/groupsnapshot.storage.k8s.io_volumegroupsnapshotclasses.yaml \
  -f $S/client/config/crd/groupsnapshot.storage.k8s.io_volumegroupsnapshotcontents.yaml \
  -f $S/client/config/crd/groupsnapshot.storage.k8s.io_volumegroupsnapshots.yaml

kp apply -f $S/deploy/kubernetes/snapshot-controller/rbac-snapshot-controller.yaml \
                 -f $S/deploy/kubernetes/snapshot-controller/setup-snapshot-controller.yaml
# манифест тянет образ v8.5.0 и не включает group-снапшоты — исправляем
kp -n kube-system set image deploy/snapshot-controller \
  snapshot-controller=registry.k8s.io/sig-storage/snapshot-controller:v8.6.0
kp -n kube-system patch deploy snapshot-controller --type=json \
  -p='[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--feature-gates=CSIVolumeGroupSnapshot=true"}]'
kp -n kube-system rollout status deploy/snapshot-controller --timeout=240s
```

```bash
git clone --depth 1 --branch v1.18.0 https://github.com/kubernetes-csi/csi-driver-host-path.git
kubectl config use-context kind-dr-prod           # deploy.sh работает с ТЕКУЩИМ контекстом (меняет его глобально!)
csi-driver-host-path/deploy/kubernetes-latest/deploy.sh
kp rollout status sts/csi-hostpathplugin --timeout=240s

# sidecar csi-snapshotter тоже нужен feature gate (контейнер ищем по имени, не по индексу)
IDX=$(kp get sts csi-hostpathplugin -o json | jq '.spec.template.spec.containers|map(.name)|index("csi-snapshotter")')
kp patch sts csi-hostpathplugin --type=json \
  -p="[{\"op\":\"add\",\"path\":\"/spec/template/spec/containers/$IDX/args/-\",\"value\":\"--feature-gates=CSIVolumeGroupSnapshot=true\"}]"
kp rollout status sts/csi-hostpathplugin --timeout=240s

kp apply -f csi-driver-host-path/examples/csi-storageclass.yaml       # csi-hostpath-sc
kp apply -f csi-driver-host-path/examples/csi-volumesnapshotclass.yaml   # csi-hostpath-snapclass
# Velero выбирает VolumeSnapshotClass по этой метке
kp label volumesnapshotclass csi-hostpath-snapclass velero.io/csi-volumesnapshot-class=true
kp apply -f - <<'EOF'
apiVersion: groupsnapshot.storage.k8s.io/v1
kind: VolumeGroupSnapshotClass
metadata: {name: csi-hostpath-groupsnapclass}
driver: hostpath.csi.k8s.io
deletionPolicy: Delete
EOF
```

> Поддержка group-снапшотов — **свойство драйвера**. Обычные `VolumeSnapshot`
> ничего не доказывают: CSI group RPC — отдельная реализация. Большинство облачных
> драйверов (на момент статьи, середина 2026) их не реализуют.

---

## Шаг 2. Velero на обоих кластерах

```bash
cd ~/dr-lab
for C in kind-dr-prod kind-dr-recovery; do
  velero install --kubecontext $C --provider aws \
    --plugins velero/velero-plugin-for-aws:v1.14.4 --image velero/velero:v1.18.4 \
    --bucket velero --secret-file ./velero-creds \
    --use-node-agent --features=EnableCSI --use-volume-snapshots=false \
    --backup-location-config region=local,s3ForcePathStyle=true,s3Url=http://$S3_IP:8333,publicUrl=http://localhost:19000 \
    --wait
done
velero --kubecontext kind-dr-prod backup-location get      # PHASE Available
```

> `s3Url` — адрес для **подов** (внутри сети kind), `publicUrl` — для **CLI на хосте**
> (`velero backup logs/describe` качают файлы по presigned URL). Оба кластера смотрят
> в один bucket — recovery-кластер увидит бэкапы prod автоматически (через ~1 минуту).

---

## Сценарий 1. Бэкап содержит данные?

### 1.1. Приложение с известным содержимым

Манифесты лежат в «git»-каталоге — позже из него будет «синкаться» recovery.

```bash
cd ~/dr-lab/git/guestbook
cat > app.yaml <<'EOF'
apiVersion: v1
kind: Namespace
metadata: {name: guestbook}
---
apiVersion: v1
kind: Service
metadata: {name: postgres, namespace: guestbook}
spec:
  clusterIP: None
  selector: {app: postgres}
  ports: [{port: 5432}]
---
apiVersion: apps/v1
kind: StatefulSet
metadata: {name: postgres, namespace: guestbook}
spec:
  serviceName: postgres
  replicas: 1
  selector: {matchLabels: {app: postgres}}
  template:
    metadata:
      labels: {app: postgres}
      annotations:
        pre.hook.backup.velero.io/container: postgres
        pre.hook.backup.velero.io/command: '["/bin/sh","-c","psql -U postgres -c CHECKPOINT"]'
    spec:
      securityContext: {fsGroup: 70}
      containers:
      - name: postgres
        image: postgres:17-alpine
        env:
        - {name: POSTGRES_PASSWORD, value: lab-only}
        - {name: PGDATA, value: /var/lib/postgresql/data/pgdata}
        ports: [{containerPort: 5432}]
        readinessProbe: {exec: {command: [pg_isready, -U, postgres]}, periodSeconds: 3}
        volumeMounts: [{name: data, mountPath: /var/lib/postgresql/data}]
  volumeClaimTemplates:
  - metadata: {name: data}
    spec:
      storageClassName: csi-hostpath-sc
      accessModes: [ReadWriteOnce]
      resources: {requests: {storage: 1Gi}}
EOF

kubectl --context kind-dr-prod apply -f app.yaml
kubectl --context kind-dr-prod -n guestbook rollout status sts/postgres --timeout=180s
kubectl --context kind-dr-prod -n guestbook exec postgres-0 -- psql -U postgres \
  -c "CREATE TABLE attendees(id serial primary key, name text);" \
  -c "INSERT INTO attendees(name) VALUES ('alice'),('bob'),('carol'),('dave');" \
  -c "SELECT count(*) FROM attendees;"
#  count = 4
```

> Два осознанных момента: образ **запинен** (`17-alpine`; у PG 18+ другой layout каталога
> данных), а аннотация `pre.hook.backup.velero.io/*` — **хук перед снапшотом**: Postgres делает
> `CHECKPOINT`, и при старте из бэкапа меньше WAL-replay. Это ещё **не** application-consistent:
> записи после `CHECKPOINT` в снапшот всё равно попадут как после «падения». Настоящая
> согласованность — pre+post хуки с заморозкой записи (`fsfreeze`, `pg_backup_start/stop`) или логический дамп.

### 1.2. Бэкап **с** data mover

```bash
cd ~/dr-lab
B=guestbook-$(date +%H%M%S); echo $B > BACKUP
velero --kubecontext kind-dr-prod backup create $B --include-namespaces guestbook \
  --snapshot-move-data --wait

# статус Completed — ещё не доказательство. Смотрим байты:
kubectl --context kind-dr-prod -n velero get datauploads -l velero.io/backup-name=$B \
  -o custom-columns='NAME:.metadata.name,PHASE:.status.phase,BYTES:.status.progress.bytesDone'
# NAME                     PHASE       BYTES
# guestbook-130313-r9sz8   Completed   40533044

velero --kubecontext kind-dr-prod backup describe $B | grep -E "Phase|HooksAttempted|HooksFailed"
# HooksAttempted: 1   HooksFailed: 0       ← хук реально отработал
```

✅ 40 533 044 байт тома покинули кластер и лежат во внешнем S3.

### 1.3. Ловушка: `Completed` без данных

```bash
velero --kubecontext kind-dr-prod backup create $B-nodata --include-namespaces guestbook --wait
kubectl --context kind-dr-prod -n velero get datauploads -l velero.io/backup-name=$B-nodata
# No resources found   ← бэкап Completed, но байтов в S3 нет
```

> Без `--snapshot-move-data` в S3 уходит **только YAML**: байтов тома во внешнем хранилище
> нет. Статус при этом тот же `Completed`. Единственное доказательство, что данные покинули
> кластер, — `DataUpload` с `bytesDone > 0`.

**Три способа защитить том:** CSI-снапшот (остаётся в кластере), **data mover**
(снапшот → node-agent выгружает байты в S3 → `DataUpload`) и **fs-backup**
(`--default-volumes-to-fs-backup`: kopia читает файлы из пода → `PodVolumeBackup`).
`--use-node-agent` нужен двум последним.

**Граница.** Velero восстанавливает ресурсы в **уже существующий** кластер. Кластер, ноды,
сеть, балансировщики и DNS создаёт IaC / Cluster API. План DR «восстановить из бэкапа»
обязан сказать, **во что**.

---

## Сценарий 3 (делаем сейчас, пока prod жив). Multi-volume consistency

Приложение «ledger» пишет пары: `order n` в один PVC и `payment n` в другой, 5 раз в секунду.
Инвариант: **у каждого платежа есть заказ**.

```bash
kp(){ kubectl --context kind-dr-prod "$@"; }
kp apply -f - <<'EOF'
apiVersion: v1
kind: Namespace
metadata: {name: ledger}
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata: {name: orders, namespace: ledger, labels: {group: ledger}}
spec:
  storageClassName: csi-hostpath-sc
  accessModes: [ReadWriteOnce]
  resources: {requests: {storage: 100Mi}}
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata: {name: payments, namespace: ledger, labels: {group: ledger}}
spec:
  storageClassName: csi-hostpath-sc
  accessModes: [ReadWriteOnce]
  resources: {requests: {storage: 100Mi}}
---
apiVersion: v1
kind: Pod
metadata: {name: writer, namespace: ledger}
spec:
  containers:
  - name: w
    image: busybox:1.37
    command: ["/bin/sh","-c"]
    args:
    - |
      n=0
      while true; do
        if [ ! -f /tmp/pause ]; then
          n=$((n+1))
          echo $n >> /orders/log;   sync
          echo $n >> /payments/log; sync
        fi
        sleep 0.2
      done
    volumeMounts: [{name: o, mountPath: /orders}, {name: p, mountPath: /payments}]
  volumes:
  - {name: o, persistentVolumeClaim: {claimName: orders}}
  - {name: p, persistentVolumeClaim: {claimName: payments}}
EOF
kp -n ledger wait --for=condition=ready pod/writer --timeout=120s
```

Хелперы оформлены функциями: в zsh команда в переменной (`K="kubectl --context …"; $K get`) не разбивается на слова.

```bash
kc(){ kubectl --context kind-dr-prod -n ledger "$@"; }

# restore PVC из VolumeSnapshot:  restore_pvc <новый-pvc> <snapshot>
restore_pvc(){ kc apply -f - <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata: {name: $1}
spec:
  storageClassName: csi-hostpath-sc
  dataSource: {name: $2, kind: VolumeSnapshot, apiGroup: snapshot.storage.k8s.io}
  accessModes: [ReadWriteOnce]
  resources: {requests: {storage: 100Mi}}
EOF
}

# проверка инварианта по двум PVC:  verify <имя-пода> <pvc-orders> <pvc-payments>
verify(){ kc apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata: {name: $1}
spec:
  restartPolicy: Never
  containers:
  - name: v
    image: busybox:1.37
    command: ["/bin/sh","-c"]
    args:
    - |
      o=\$(tail -1 /orders/log); p=\$(tail -1 /payments/log)
      echo "last order committed   : \$o"; echo "last payment committed : \$p"
      if [ "\$p" -gt "\$o" ]; then echo "[FAIL] \$((p-o)) payments have NO matching order."
      else echo "[OK] Every payment has a matching order."; fi
    volumeMounts: [{name: o, mountPath: /orders}, {name: p, mountPath: /payments}]
  volumes:
  - {name: o, persistentVolumeClaim: {claimName: $2}}
  - {name: p, persistentVolumeClaim: {claimName: $3}}
EOF
kc wait pod/$1 --for=jsonpath='{.status.phase}'=Succeeded --timeout=120s >/dev/null; kc logs $1; }
```

### 3.1. Два снапшота с разницей в 5 секунд (torn)

```bash
snap(){ kc apply -f - <<EOF
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshot
metadata: {name: $1}
spec: {volumeSnapshotClassName: csi-hostpath-snapclass, source: {persistentVolumeClaimName: $2}}
EOF
kc wait vs/$1 --for=jsonpath='{.status.readyToUse}'=true --timeout=60s; }

snap torn-orders orders; sleep 5; snap torn-payments payments    # каждый ReadyToUse и «идеален»
restore_pvc torn-orders   torn-orders
restore_pvc torn-payments torn-payments
verify verify-torn torn-orders torn-payments
# last order committed   : 112
# last payment committed : 140
# [FAIL] 28 payments have NO matching order.
```

❌ Каждая операция вернула успех, а точка восстановления описывает момент, которого **не
существовало**. В проде «5 секунд» — это backup-тул, обходящий 100 PVC по очереди.

### 3.2. `VolumeGroupSnapshot` — один срез на оба тома

Для детерминированности демо **ставим writer на паузу** на время снапшота (hostpath-драйвер
архивирует тома последовательно; гарантия «одного момента» в проде — свойство СХД драйвера).

> Честно: с паузой и два отдельных снапшота совпали бы. Демо показывает **механизм** — один
> объект, один запрос к драйверу, member-снапшоты с ownerReference; гарантию без паузы даёт
> только хранилище продового драйвера.

```bash
kc exec writer -- touch /tmp/pause; sleep 1
kc apply -f - <<'EOF'
apiVersion: groupsnapshot.storage.k8s.io/v1
kind: VolumeGroupSnapshot
metadata: {name: ledger-group-snap}
spec:
  volumeGroupSnapshotClassName: csi-hostpath-groupsnapclass
  source:
    selector:
      matchLabels: {group: ledger}
EOF
kc wait vgs/ledger-group-snap --for=jsonpath='{.status.readyToUse}'=true --timeout=90s
kc exec writer -- rm /tmp/pause

# member-снапшоты получают сгенерированные имена; их находим по ownerReference,
# а исходный PVC — в spec.source:
kc get vs -o json | jq -r '.items[]|select(.metadata.ownerReferences[0].name=="ledger-group-snap")
  |"\(.spec.source.persistentVolumeClaimName) \(.metadata.name)"' \
 | while read pvc snapname; do restore_pvc grp-$pvc $snapname; done

verify verify-group grp-orders grp-payments
# last order committed   : 212
# last payment committed : 212
# [OK] Every payment has a matching order.
```

**Оговорки (применимы и вне лабы):**
- Поддержка зависит от **драйвера** (см. шаг 1); нужны CRD и feature gate на snapshot-controller **и** на sidecar.
- Это **crash-consistent**, а не application-consistent: API убирает рассинхрон между
  томами, но не делает flush/quiesce БД.

---

## Сценарий 2. Declared state ≠ stored state (GitOps-ловушка)

### 2.1. «Катастрофа»: выключаем prod

```bash
podman stop dr-prod-control-plane        # prod = одна выключенная машина
date +%s > ~/dr-lab/T0                   # запускаем часы RTO
```

### 2.2. Recovery «синкает» приложение из Git

Recovery-кластер существовал заранее, но приложение не запускал. Чтобы манифесты из Git
применились без ошибок, ему нужен StorageClass с тем же именем — **но другой бэкенд**
(local-path вместо CSI hostpath): так же будет в реальном DR на другой инфраструктуре.

```bash
kr(){ kubectl --context kind-dr-recovery "$@"; }
kr apply -f - <<'EOF'
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata: {name: csi-hostpath-sc}
provisioner: rancher.io/local-path
volumeBindingMode: WaitForFirstConsumer
reclaimPolicy: Delete
EOF

kr apply -f ~/dr-lab/git/guestbook/app.yaml        # = «GitOps sync»
kr -n guestbook rollout status sts/postgres --timeout=180s
# partitioned roll out complete: 1 new pods have been updated...
kr -n guestbook get pod postgres-0                  # 1/1 Running — все дашборды зелёные
kr -n guestbook exec postgres-0 -- psql -U postgres -c "SELECT count(*) FROM attendees;"
# ERROR:  relation "attendees" does not exist      (psql вернёт exit 1 — это ожидаемо)
```

❌ База **работает и пуста**. Ничего не сломалось: Git содержал только декларации, и
Kubernetes сделал ровно то, что написано в YAML — создал StatefulSet и **новый пустой** том.

> **Stretch (опционально):** то же поведение даст Argo CD / Flux. Если подключите
> контроллер — перед restore **отключите auto-sync/selfHeal**, иначе он будет бороться с
> Velero за те же объекты.

### 2.3. Наивный restore поверх пустого приложения

```bash
vr(){ velero --kubecontext kind-dr-recovery "$@"; }
B=$(cat ~/dr-lab/BACKUP)
vr backup get                 # бэкапы prod появились в recovery (синк BSL ~1 мин)

vr restore create naive-$B --from-backup $B --wait
# Restore completed with status: Completed
kr -n guestbook exec postgres-0 -- psql -U postgres -c "SELECT count(*) FROM attendees;"
# ERROR:  relation "attendees" does not exist       ← данных всё равно нет!
vr restore describe naive-$B | grep -A10 Warnings
#   could not restore, PersistentVolumeClaim:data-postgres-0 already exists ...
```

❌ Velero **пропускает уже существующие** объекты: restore `Completed`, предупреждения в
`Warnings`, а пустой PVC остался. Это вторая версия того же урока: статус ≠ данные.
`--existing-resource-policy=update` не спасёт: он обновляет метаданные объектов, но не перезаливает данные в существующий PVC.

> В реальном DR переключите recovery на чтение до restore, иначе GC/`backup delete` на нём
> удалит точки восстановления prod: `vr backup-location set default --access-mode ReadOnly`.

### 2.4. Правильный порядок

```bash
# 1. удалить пустое приложение, созданное sync-ом
kr delete ns guestbook --wait=true
# 2. восстановить приложение ВМЕСТЕ с томами из бэкапа
vr restore create fix-$B --from-backup $B --wait
# 3. валидировать по ожидаемому содержимому, а не по статусу
kr -n guestbook wait pod/postgres-0 --for=condition=ready --timeout=180s
kr -n guestbook exec postgres-0 -- psql -U postgres -c "SELECT name FROM attendees ORDER BY id;"
#  alice / bob / carol / dave   (4 rows)
echo "RTO (скриптовая часть): $(( $(date +%s) - $(cat ~/dr-lab/T0) ))s"
```

✅ Четыре строки вернулись; данные приехали с **другого бэкенда хранения** (hostpath → local-path).

> **Замер.** Это только скриптованный срез. Реальный RTO добавляет обнаружение, принятие
> решения, переключение трафика и failback. Момент, когда дашборды позеленели, — **не**
> восстановление. Восстановление — когда данные вернулись **и проверены**.

**Портируемость на другой StorageClass под другим именем** — тоже надо тестировать, а не
предполагать: Velero умеет маппинг через ConfigMap
`velero.io/change-storage-class` (см. [docs](https://velero.io/docs/main/restore-reference/#changing-pv/pvc-storage-classes)).

---

## Шаг 3. Что такое настоящий recovery-тест

1. Восстановить **весь** stateful-стек в **чистую** цель, которая его никогда не запускала.
2. Проверить **данные и пользовательский путь** против ожидаемого содержимого, а не статусы ресурсов.
3. **Замерить** всё секундомером.

Удалить под и увидеть, что он вернулся, — это проверка reconcile, а не DR.

Пробелы экосистемы (из статьи): нет общего контракта cross-cluster failover; нет стандартной
«единицы восстановления» приложения; успех бэкапа принимают за доказательство восстановления
(метрики бэкапов мониторят, результаты restore-репетиций — почти никогда).

---

## Cleanup

```bash
export KIND_EXPERIMENTAL_PROVIDER=podman      # иначе kind может обратиться к docker
kind delete cluster --name dr-prod
kind delete cluster --name dr-recovery
podman rm -f dr-s3
kind get clusters                              # dr-* не должно остаться
kubectl config use-context "$(cat ~/dr-lab/ORIG_CTX)" 2>/dev/null   # deploy.sh менял контекст
rm -rf ~/dr-lab
```

> Удаляйте кластеры **по имени**. Не используйте `kind delete clusters --all` — заденет
> другие ваши кластеры.

---

## Что вы узнали

- Бэкап k8s = **YAML + данные томов**; проверяйте байты (`DataUpload.bytesDone`), `Completed` этого не гарантирует.
- Без `--snapshot-move-data` байты тома в S3 не попадают — потеря кластера = потеря данных при `Completed`.
- **Git = intent, backup = state.** GitOps идеально восстановит декларации и выдаст *пустую* БД.
- Velero **пропускает существующие** объекты: пустое приложение надо удалить до restore.
- Два отдельных снапшота = точка во времени, которой не было; `VolumeGroupSnapshot` даёт один срез (crash-consistent, не application-consistent; нужна поддержка драйвера).
- Хуки (pre/post) — ваша зона ответственности; `CHECKPOINT` ≠ quiesce.
- Backup-тул не создаёт кластер: «что мы восстанавливаем *во что*» — часть DR-плана.
- Настоящий recovery-тест: чистая цель → проверка данных → секундомер.
