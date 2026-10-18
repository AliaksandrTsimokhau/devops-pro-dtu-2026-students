# Lab 23 — SecurityContext + Pod Security Admission

**Цель:** hardening Pod-а через SecurityContext и применение Pod Security Standards на уровне namespace.

**Время:** 25 минут
**Prerequisites:** module 02, Lab 03 — первый Pod, module 02, Lab 05 — labels и selectors.

---

## Шаг 1. Bad Pod — без security

`bad-pod.yaml`:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: bad-pod
spec:
  containers:
  - name: app
    image: nginx:1.27-alpine
    securityContext:
      privileged: true                  # ← полный root
```

```bash
kubectl apply -f bad-pod.yaml
kubectl get pod bad-pod
```

Pod успешно запустится в `default` namespace. Это плохо.

---

## Шаг 2. Включаем Pod Security Admission на namespace

```bash
kubectl create ns secure
kubectl label ns secure \
  pod-security.kubernetes.io/enforce=baseline \
  pod-security.kubernetes.io/warn=restricted \
  pod-security.kubernetes.io/audit=restricted
```

Что это значит:
- **enforce=baseline** — отклонит Pod-ы нарушающие baseline policy
- **warn=restricted** — warning при создании Pod-ов нарушающих restricted
- **audit=restricted** — логирует нарушения в audit log

---

## Шаг 3. Попробуем bad-pod в secure namespace

```bash
sed 's/name: bad-pod/name: bad-pod-2/' bad-pod.yaml | \
  kubectl apply -n secure -f -
# Error from server (Forbidden): pods "bad-pod-2" is forbidden:
# violates PodSecurity "baseline:latest": privileged
```

✅ PSA отклонил!

---

## Шаг 4. Hardened Pod — соответствует restricted

`hardened-pod.yaml`:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: hardened
spec:
  securityContext:
    runAsNonRoot: true
    runAsUser: 10001
    runAsGroup: 10001
    fsGroup: 10001
    seccompProfile:
      type: RuntimeDefault
  containers:
  - name: app
    image: nginxinc/nginx-unprivileged:1.30     # ← образ запускается non-root
    securityContext:
      allowPrivilegeEscalation: false
      readOnlyRootFilesystem: true
      capabilities:
        drop: ["ALL"]
    # readOnlyRootFilesystem=true → root ФС только для чтения.
    # nginx пишет во временные файлы, поэтому каждый writable путь
    # отдаём как emptyDir, иначе Pod упадёт с "Read-only file system".
    volumeMounts:
    - name: cache
      mountPath: /var/cache/nginx       # кэш и client/proxy temp
    - name: run
      mountPath: /var/run               # pid-файл
    - name: tmp
      mountPath: /tmp                    # nginx temp (/tmp/proxy_temp и т.д.)
  volumes:
  - name: cache
    emptyDir: {}
  - name: run
    emptyDir: {}
  - name: tmp
    emptyDir: {}
```

```bash
kubectl apply -n secure -f hardened-pod.yaml
kubectl get pod -n secure hardened
# Running ✅
```

---

## Шаг 5. Проверка settings внутри Pod

```bash
kubectl exec -n secure hardened -- id
# uid=10001 gid=10001 groups=10001 ✅ non-root

kubectl exec -n secure hardened -- cat /proc/self/status | grep -E 'Uid|CapEff'
# Uid: 10001 10001 10001 10001
# CapEff: 0000000000000000   ← все capabilities dropped

kubectl exec -n secure hardened -- touch /test.txt 2>&1
# touch: /test.txt: Read-only file system ✅
```

---

## Шаг 6. Audit log (если включён)

В managed-кластерах (EKS/GKE) audit события идут в CloudWatch / Stackdriver. В локальном setup-е audit log должен быть отдельно настроен.

Зато `warn=restricted` показывает warning прямо в терминале. Важно: для этого
нужен Pod, который **проходит** `enforce=baseline`, но **нарушает** `restricted`.
Привилегированный `bad-pod` тут не подойдёт — его отклонит сам `enforce=baseline`
(см. Шаг 3), warning вы не увидите.

Возьмём обычный nginx без securityContext — baseline ОК, но restricted нарушен:
```bash
cat <<EOF | kubectl apply -n secure -f -
apiVersion: v1
kind: Pod
metadata:
  name: baseline-ok
spec:
  containers:
  - name: app
    image: nginx:1.27-alpine
EOF
# Warning: would violate PodSecurity "restricted:latest": allowPrivilegeEscalation != false,
#   unrestricted capabilities, runAsNonRoot != true, seccompProfile ...
# pod/baseline-ok created      ← Pod ВСЁ РАВНО создан: warn только предупреждает

kubectl delete -n secure pod baseline-ok
```

---

## Шаг 7. Dry-run для тестирования миграции

```bash
# Проверить готовы ли вы перейти на restricted без реального enforce
kubectl label --dry-run=server --overwrite ns secure \
  pod-security.kubernetes.io/enforce=restricted
# показывает какие Pod-ы будут отклонены
```

---

## Шаг 8. PSA для всего кластера (теория, read-only)

> **Только чтение.** `pss.yaml` нужно передать API server при создании кластера. В kind для этого нужен kind config с `extraMounts` и `kubeadmConfigPatches` (проверь: актуальные поля в docs kind). Команды ниже в этой лабе не выполняйте; практикуйте PSA через namespace-метки из Шага 2.

В новых кластерах можно применять PSA на all-namespaces:
```bash
# Через API server flags при создании кластера:
# --admission-control-config-file=pss.yaml
```

`pss.yaml`:
```yaml
apiVersion: apiserver.config.k8s.io/v1
kind: AdmissionConfiguration
plugins:
- name: PodSecurity
  configuration:
    apiVersion: pod-security.admission.config.k8s.io/v1
    kind: PodSecurityConfiguration
    defaults:
      enforce: "baseline"
      enforce-version: "latest"
    exemptions:
      namespaces: [kube-system]
```

---

## Шаг 9. Beyond PSA — OPA Gatekeeper / Kyverno

PSA — built-in, но ограничен 3-я policies. Для гибких политик:
- **Kyverno** — policy-as-YAML (легче в изучении)
- **OPA Gatekeeper** — Rego language (мощнее)

Пример Kyverno policy: "все Pod-ы должны иметь requests/limits":
```yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: require-resources
spec:
  rules:
  - name: validate-resources
    match:
      any:
      - resources: {kinds: [Pod]}
    validate:
      failureAction: Enforce     # проверь: per-rule failureAction вместо устаревшего validationFailureAction, по docs Kyverno для вашей версии
      message: "CPU and memory resource requests are required"
      pattern:
        spec:
          containers:
          - resources:
              requests:
                memory: "?*"
                cpu: "?*"
```

---

## Cleanup

```bash
kubectl delete pod bad-pod
kubectl delete -n secure pod hardened
kubectl delete ns secure
```

---

## Что вы узнали

- **SecurityContext** — настройки на уровне Pod / Container:
  - `runAsNonRoot: true` + `runAsUser: <UID>`
  - `allowPrivilegeEscalation: false`
  - `readOnlyRootFilesystem: true`
  - `capabilities: drop: [ALL]`
  - `seccompProfile: type: RuntimeDefault`
- **Pod Security Standards** — 3 уровня: privileged / baseline / restricted
- **Pod Security Admission** — enforce/warn/audit через namespace labels
- Production migration: start `warn`, fix violations, switch to `enforce`
- Для гибких политик: Kyverno или OPA Gatekeeper
