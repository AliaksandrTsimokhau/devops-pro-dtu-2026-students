# Lab 33 — Job and CronJob

**Цель:** запустить одноразовый Job (с параллелизмом), потом расписание через CronJob.

**Время:** 20 минут
**Prerequisites:** Lab 03.

---

## Шаг 1. Simple Job

`job.yaml`:
```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: hello-job
spec:
  template:
    spec:
      restartPolicy: OnFailure
      containers:
      - name: hello
        image: busybox:1.37
        command: ["sh", "-c", "echo Hello from Job; date; sleep 5; echo Done"]
  backoffLimit: 3
  ttlSecondsAfterFinished: 300            # автоудаление через 5 мин
```

```bash
kubectl apply -f job.yaml
kubectl get jobs --watch
# Через 5-10 секунд: COMPLETIONS 1/1
```

Логи:
```bash
kubectl logs job/hello-job
```

---

## Шаг 2. Parallel Job

`parallel-job.yaml`:
```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: parallel-job
spec:
  completions: 10            # всего 10 успешных запусков
  parallelism: 3             # макс 3 одновременно
  backoffLimit: 5
  ttlSecondsAfterFinished: 300
  template:
    spec:
      restartPolicy: OnFailure
      containers:
      - name: worker
        image: busybox:1.37
        command: ["sh", "-c", "echo Processing item $HOSTNAME; sleep $((RANDOM % 5 + 2)); echo Done"]
```

```bash
kubectl apply -f parallel-job.yaml
kubectl get jobs parallel-job --watch
# COMPLETIONS будет расти: 0/10 → 3/10 → 6/10 → 10/10

# Сколько Pod-ов было создано
kubectl get pods -l job-name=parallel-job
```

---

## Шаг 3. Indexed Job — шардирование

`indexed-job.yaml`:
```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: indexed-job
spec:
  completions: 5
  parallelism: 2
  completionMode: Indexed     # ← каждый Pod получит JOB_COMPLETION_INDEX
  backoffLimit: 3
  ttlSecondsAfterFinished: 300
  template:
    spec:
      restartPolicy: Never
      containers:
      - name: worker
        image: busybox:1.37
        command:
        - sh
        - -c
        - 'echo "Shard $JOB_COMPLETION_INDEX of 5"; sleep 5; echo done'
```

```bash
kubectl apply -f indexed-job.yaml
kubectl get pods -l job-name=indexed-job
# Pod-ы будут называться indexed-job-<index>-...

# Логи каждого индекса
for i in 0 1 2 3 4; do
  POD=$(kubectl get pods -l job-name=indexed-job,batch.kubernetes.io/job-completion-index=$i -o jsonpath='{.items[0].metadata.name}')
  echo "--- Shard $i ---"
  kubectl logs $POD
done
```

> Indexed Jobs полезны для batch-обработки: каждый Pod обрабатывает свой шард данных.

---

## Шаг 4. CronJob

`cronjob.yaml`:
```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: hello-cron
spec:
  schedule: "*/1 * * * *"             # каждую минуту
  concurrencyPolicy: Forbid           # не запускать если предыдущий ещё работает
  successfulJobsHistoryLimit: 3
  failedJobsHistoryLimit: 1
  jobTemplate:
    spec:
      ttlSecondsAfterFinished: 300
      template:
        spec:
          restartPolicy: OnFailure
          containers:
          - name: hello
            image: busybox:1.37
            command: ["sh", "-c", "echo Hello at $(date)"]
```

```bash
kubectl apply -f cronjob.yaml
kubectl get cronjobs

# Подождём 1-2 минуты (schedule = каждую минуту)
sleep 90

# Появятся Job-ы с именем-префиксом <cronjob>-<timestamp>.
# ВАЖНО: Job-ы от CronJob НЕ имеют лейбла cronjob.kubernetes.io/name —
# фильтруем по ownerReference на CronJob:
kubectl get jobs -o jsonpath='{range .items[?(@.metadata.ownerReferences[0].name=="hello-cron")]}{.metadata.name}{"\n"}{end}'
# hello-cron-29702196
# hello-cron-29702197

# Логи последнего запуска (берём самый свежий Job и читаем его логи):
LATEST_JOB=$(kubectl get jobs \
  -o jsonpath='{range .items[?(@.metadata.ownerReferences[0].name=="hello-cron")]}{.metadata.name}{"\n"}{end}' \
  | tail -1)
kubectl logs job/$LATEST_JOB
# Hello at Mon Jun 22 12:39:00 UTC 2026
```

> ⚠️ Частая ошибка: `kubectl get jobs -l cronjob.kubernetes.io/name=hello-cron`
> вернёт **No resources found** — такого лейбла на Job-ах нет. Связь
> CronJob → Job хранится в `ownerReferences`, а не в лейблах. Фильтруем по owner
> (как выше) либо проще — по имени-префиксу: `kubectl get jobs | grep hello-cron`.

---

## Шаг 5. concurrencyPolicy variations

| Policy | Поведение |
|---|---|
| `Allow` (default) | Параллельные запуски разрешены |
| `Forbid` | Skip новый запуск если предыдущий не завершён |
| `Replace` | Убить текущий, запустить новый |

```yaml
spec:
  schedule: "*/2 * * * *"
  concurrencyPolicy: Replace
  # Хорошо для tasks где важна актуальность, не накопление
```

---

## Шаг 6. activeDeadlineSeconds — таймаут

```bash
kubectl apply -f - <<'EOF'
apiVersion: batch/v1
kind: Job
metadata: {name: timeout-job}
spec:
  activeDeadlineSeconds: 30       # если не завершился за 30 сек — fail
  backoffLimit: 0                 # без повторов
  template:
    spec:
      restartPolicy: Never
      containers:
      - name: long
        image: busybox:1.37
        command: ["sh", "-c", "sleep 60"]    # 60 сек — будет терминирован
EOF

# ждём перехода в Failed (≈60-70 сек: deadline 30с + терминирование пода)
kubectl wait --for=condition=failed job/timeout-job --timeout=90s
kubectl get jobs timeout-job
# NAME          STATUS   COMPLETIONS   ...
# timeout-job   Failed   0/1
kubectl get job timeout-job -o jsonpath='{.status.conditions[?(@.type=="Failed")].reason}'; echo
# DeadlineExceeded
```

> Не используйте `sleep 35` — на 35-й секунде Job ещё в промежуточном состоянии
> (`FailureTarget`), и `kubectl get` покажет не `Failed`. Дождитесь условия через
> `kubectl wait` (как выше): deadline срабатывает на 30с, но финальный переход в
> `Failed` занимает суммарно ~60-70 секунд.

---

## Шаг 7. Real-world CronJob — backup

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: db-backup
spec:
  schedule: "0 2 * * *"               # каждый день в 02:00
  timeZone: "Europe/Moscow"           # K8s 1.27+
  concurrencyPolicy: Forbid
  successfulJobsHistoryLimit: 7
  failedJobsHistoryLimit: 3
  jobTemplate:
    spec:
      backoffLimit: 1
      ttlSecondsAfterFinished: 86400  # 24 часа
      template:
        spec:
          restartPolicy: OnFailure
          containers:
          - name: backup
            image: postgres:16
            env:
            - name: PGPASSWORD
              valueFrom:
                secretKeyRef: {name: db-creds, key: password}
            command:
            - sh
            - -c
            - |
              pg_dump -h db -U postgres mydb | gzip > /backups/$(date +%Y%m%d-%H%M%S).sql.gz
            volumeMounts:
            - name: backups
              mountPath: /backups
          volumes:
          - name: backups
            persistentVolumeClaim:
              claimName: backup-storage
```

---

## Cleanup

```bash
kubectl delete -f cronjob.yaml
kubectl delete -f indexed-job.yaml
kubectl delete -f parallel-job.yaml
kubectl delete -f job.yaml
kubectl delete job timeout-job --ignore-not-found    # из Шага 6 (создавался inline)
```

---

## Что вы узнали

- **Job** — одноразовая задача, гарантирует успешное завершение
- `parallelism` + `completions` → параллельная обработка
- `completionMode: Indexed` → каждый Pod получает `JOB_COMPLETION_INDEX` (для шардирования)
- `backoffLimit` — макс. число неудачных запусков перед Failed
- `activeDeadlineSeconds` — общий timeout
- `ttlSecondsAfterFinished` — автоудаление (must в production)
- **CronJob** — Job по расписанию (cron syntax)
- `concurrencyPolicy: Forbid` — для backup, ETL
- `timeZone` — K8s 1.27+ (по умолчанию UTC)
- Production use cases: миграции БД, backup, аналитика, очистка
