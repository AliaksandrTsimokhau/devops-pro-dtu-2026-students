# Lab 26 — Operators и Custom Controllers (reconcile loop)

**Цель:** понять паттерн «оператор» = **CRD** (свой API) + **контроллер**
(reconcile loop). Lab 25 только *определял* CRD — здесь мы пишем контроллер,
который **действует** на ресурс: разворачивает Deployment+Service по объекту
`Website`, чинит дрейф и убирает за собой через ownerReferences.

**Время:** 35 минут
**Prerequisites:** Lab 22 (RBAC), Lab 25 (CRD).

> Хендс-он строим на «лёгком» контроллере (shell reconcile loop) — чтобы увидеть
> механику без Go-компиляции. Реальные операторы пишут на kubebuilder/operator-sdk
> (см. Шаг 6).

---

## BLUF — что такое оператор

**Оператor = расширение Kubernetes, кодирующее операционные знания.**

```
CRD         — новый тип объекта (желаемое состояние), напр. Website
Controller  — процесс, который в цикле:
                1. читает все Website (desired)
                2. смотрит, что реально есть в кластере (actual)
                3. приводит actual → desired  (reconcile)
```

Это та же модель, по которой работают встроенные контроллеры (Deployment →
ReplicaSet → Pod). Оператор = «свой» контроллер для своего CRD.

---

## Шаг 1. CRD — наш API `Website`

`website-crd.yaml`:
```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: {name: websites.example.com}
spec:
  group: example.com
  scope: Namespaced
  names: {plural: websites, singular: website, kind: Website, shortNames: [web]}
  versions:
  - name: v1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        type: object
        properties:
          spec:
            type: object
            properties:
              image: {type: string}
              replicas: {type: integer, default: 1}
            required: [image]
    additionalPrinterColumns:
    - {name: Image, type: string, jsonPath: .spec.image}
    - {name: Replicas, type: integer, jsonPath: .spec.replicas}
```

```bash
kubectl apply -f website-crd.yaml
kubectl get crd websites.example.com
# теперь `kubectl get websites` работает, но НИЧЕГО не происходит — нет контроллера
```

---

## Шаг 2. Контроллер — RBAC + reconcile loop

Контроллеру нужны права: читать `websites`, создавать/править `deployments` и
`services`. Сам loop — shell-скрипт в Pod-е (образ `alpine/k8s` = kubectl + jq).

`website-controller.yaml`:
```yaml
apiVersion: v1
kind: ServiceAccount
metadata: {name: website-controller, namespace: default}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata: {name: website-controller}
rules:
- {apiGroups: ["example.com"], resources: ["websites"], verbs: ["get","list","watch"]}
- {apiGroups: ["apps"], resources: ["deployments"], verbs: ["get","list","create","update","patch"]}
- {apiGroups: [""], resources: ["services"], verbs: ["get","list","create","update","patch"]}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata: {name: website-controller}
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: website-controller}
subjects: [{kind: ServiceAccount, name: website-controller, namespace: default}]
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: website-controller, namespace: default}
spec:
  replicas: 1
  selector: {matchLabels: {app: website-controller}}
  template:
    metadata: {labels: {app: website-controller}}
    spec:
      serviceAccountName: website-controller
      containers:
      - name: controller
        image: alpine/k8s:1.31.0
        command: ["/bin/sh","-c"]
        args:
        - |
          while true; do
            kubectl get websites -A -o json | jq -c '.items[]' | while read -r w; do
              ns=$(echo "$w"   | jq -r .metadata.namespace)
              name=$(echo "$w" | jq -r .metadata.name)
              uid=$(echo "$w"  | jq -r .metadata.uid)
              image=$(echo "$w"| jq -r .spec.image)
              reps=$(echo "$w" | jq -r '.spec.replicas // 1')
              echo "reconcile $ns/$name image=$image replicas=$reps"
              cat <<YAML | kubectl apply -f - >/dev/null
          apiVersion: apps/v1
          kind: Deployment
          metadata:
            name: ${name}
            namespace: ${ns}
            ownerReferences:
            - {apiVersion: example.com/v1, kind: Website, name: ${name}, uid: ${uid}, controller: true, blockOwnerDeletion: true}
          spec:
            replicas: ${reps}
            selector: {matchLabels: {website: ${name}}}
            template:
              metadata: {labels: {website: ${name}}}
              spec:
                containers:
                - {name: web, image: ${image}, ports: [{containerPort: 80}]}
          ---
          apiVersion: v1
          kind: Service
          metadata:
            name: ${name}
            namespace: ${ns}
            ownerReferences:
            - {apiVersion: example.com/v1, kind: Website, name: ${name}, uid: ${uid}, controller: true, blockOwnerDeletion: true}
          spec:
            selector: {website: ${name}}
            ports: [{port: 80, targetPort: 80}]
          YAML
            done
            sleep 5
          done
```

```bash
kubectl apply -f website-controller.yaml
kubectl rollout status deploy/website-controller --timeout=180s   # образ большой
```

> 🔑 `ownerReferences` ставит `Website` владельцем Deployment/Service. Это даёт
> **бесплатную сборку мусора**: удалят Website → Kubernetes сам удалит дочерние
> объекты. Контроллеру не нужна логика удаления.

---

## Шаг 3. Создаём Website → контроллер реагирует

```bash
kubectl apply -f - <<'EOF'
apiVersion: example.com/v1
kind: Website
metadata: {name: blog}
spec:
  image: nginx:1.27-alpine
  replicas: 2
EOF

# через ~5 сек контроллер развернёт Deployment+Service
kubectl get website blog
# NAME   IMAGE        REPLICAS
# blog   nginx:1.27-alpine   2

kubectl get deploy,svc blog
# deployment.apps/blog   2/2 ...
# service/blog           ClusterIP ...

kubectl get deploy blog -o jsonpath='owner={.metadata.ownerReferences[0].kind}/{.metadata.ownerReferences[0].name}{"\n"}'
# owner=Website/blog

kubectl logs deploy/website-controller --tail=3
# reconcile default/blog image=nginx:1.27-alpine replicas=2
```

---

## Шаг 4. Reconciliation — самовосстановление при дрейфе

Контроллер каждые 5 сек приводит actual → desired. Поломаем «руками»:
```bash
kubectl scale deploy blog --replicas=5     # дрейф: вручную меняем
sleep 8
kubectl get deploy blog -o jsonpath='{.spec.replicas}{"\n"}'
# 2     ← контроллер вернул к desired (spec.replicas из Website)
```

> Это суть оператора: **desired state в CRD — источник истины**. Любой дрейф
> (ручной `kubectl scale`, чужой apply) откатывается на следующем reconcile.
> Менять надо **Website**, а не managed-объекты.

---

## Шаг 5. ownerReferences → каскадное удаление

```bash
kubectl delete website blog
sleep 6
kubectl get deploy blog    # NotFound — удалён автоматически
kubectl get svc blog       # NotFound — тоже
```

✅ Удалили только `Website` — Kubernetes garbage collector снёс Deployment и
Service по ownerReference. Декларативно и без утечек.

---

## Шаг 6. Как это делают в production

Shell-loop хорош для понимания, но реальные операторы:

| Инструмент | Язык | Когда |
|---|---|---|
| **kubebuilder** | Go | стандарт для serious-операторов (controller-runtime) |
| **Operator SDK** | Go / Ansible / Helm | то же + Ansible/Helm-операторы без Go |
| **kopf** | Python | быстрый старт, Python-команды |
| **Metacontroller** | любой (hooks) | контроллер как webhook, без фреймворка |

Готовые операторы из реального мира (тот же паттерн CRD+контроллер):
- **cert-manager** — CRD `Certificate`/`Issuer`, выписывает TLS-серты
- **Prometheus Operator** — CRD `Prometheus`/`ServiceMonitor`
- **operator-ы БД** (CloudNativePG, Strimzi/Kafka) — CRD `Cluster`/`Kafka`

> Реальные контроллеры используют **informer-ы/watch** (а не poll каждые 5 сек),
> **work queue** с rate-limiting, обновляют **`.status`** ресурса и пишут
> **Events**. Принцип тот же — наш loop его демонстрирует в миниатюре.

---

## 👀 Наблюдение и диагностика

**Смотреть reconcile вживую** — два окна рядом:
```bash
# окно 1: что создаёт контроллер
watch -n2 'kubectl get website,deploy,svc'
# окно 2: лог самого контроллера (видно строки "reconcile ...")
kubectl logs deploy/website-controller -f
```
k9s (`:pods`, `l` — логи) тоже удобен — см. [appendix-accessing-uis.md](appendix-accessing-uis.md).

✅ **Checkpoint:** создание `Website` → через ~5 сек появляются Deployment+Service
с `ownerReferences → Website`; ручной `scale` откатывается; удаление `Website`
каскадно сносит дочерние объекты.

🔧 **Troubleshooting:**
- ничего не создаётся → контроллер не `Running` (большой образ `alpine/k8s`
  ещё тянется) или не хватает RBAC — смотрите `kubectl logs deploy/website-controller`.
- реакция с задержкой до 5 сек → это poll-loop (`sleep 5`), а не watch — норма
  для учебного контроллера.
- дочерние объекты не удаляются с `Website` → проверьте `ownerReferences` в
  Deployment (`kubectl get deploy blog -o yaml | grep -A5 ownerReferences`).

---

## Cleanup

```bash
kubectl delete website --all
kubectl delete -f website-controller.yaml --ignore-not-found
kubectl delete crd websites.example.com --ignore-not-found
```

---

## Что вы узнали

- **Оператор = CRD (desired state) + контроллер (reconcile loop)**, кодирующий
  операционные знания.
- Reconcile: читать desired → сравнить с actual → привести к desired; в цикле.
- **ownerReferences** дают бесплатную каскадную сборку мусора дочерних объектов.
- **Самовосстановление**: дрейф (ручные изменения) откатывается; источник
  истины — CRD.
- В production: informer/watch + workqueue + `.status` + Events; пишут на
  kubebuilder / Operator SDK / kopf.
- Готовые операторы (cert-manager, Prometheus Operator, operator-ы БД) — тот же
  паттерн в боевом исполнении.
