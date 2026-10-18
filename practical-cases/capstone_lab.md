# Capstone: от коммита до алерта

**Длительность:** 3–4 часа · **Сложность:** ★★★★★ · **Формат:** индивидуально или пара

## BLUF

Вы берёте одно маленькое приложение и проводите его через весь путь, который прошли по частям:
**commit → pipeline (lint, build, secret-detection, trivy) → Helm-чарт → deploy в курсовой kind-кластер через GitLab Agent → probes и HPA → метрики, алерт, логи**.
Затем мы ломаем систему тремя способами, а вы находите причину и чините.

Новых технологий здесь нет. Проверяется другое: можете ли вы **соединить** уже знакомые куски и **диагностировать** поломку на стыке.

> Правило лаборатории: команды, которыми владеет другая лабораторная, здесь не повторяются. Открывайте ссылку и выполняйте шаг оттуда.

## Архитектура

```
 developer
    │ git push
    ▼
┌───────────────────────── GitLab CI pipeline ─────────────────────────┐
│ lint ─► build image ─► secret-detection ─► trivy ─► (cosign*) ─►     │
│                                                    helm package/push │
└──────────────────────────────────────────────────────────┬───────────┘
                                                           │ helm upgrade --install
                                                           ▼  (через GitLab Agent)
┌──────────────── курсовой kind-кластер (контекст kind-book) ──────────┐
│  namespace: capstone                                                 │
│   Deployment ─► Pods (probes) ◄── HPA        Service ─► Ingress      │
│        │                                                             │
│   ServiceMonitor ─► Prometheus ─► Alertmanager ─► receiver           │
│   Pod logs ─► Alloy ─► Loki ─► Grafana (Explore)                     │
└──────────────────────────────────────────────────────────────────────┘
```

`*` подпись cosign необязательна и в обязательный pipeline не входит.

**Один кластер на весь capstone.** Приложение, GitLab Agent, мониторинг и Loki живут в одном курсовом kind-кластере: имя задано в lab-01-cluster-bootstrap (`kind-book`), один kubectl-контекст. Кластер **отдельный** от self-hosted GitLab из [gitlab_in_kind](../gitlab/gitlab_in_kind.md): тот нужен только если вы используете self-hosted GitLab, на gitlab.com его не бывает.

## Ресурсы

- **Ноутбук на 16 GB:** используйте gitlab.com, GitLab Agent и один локальный kind-кластер (VM 6–8 GiB). Self-hosted GitLab здесь не запускайте.
- **Self-hosted GitLab вместе с capstone:** хост на 32 GB+, VM не меньше 16 GiB RAM и 60 GiB диска.

## Чек-лист перед стартом

Все пункты ниже должны быть выполнены. Если пункт не готов, вернитесь в указанную лабораторную.

- [ ] Курсовой kind-кластер работает (`kind-book`): lab-01-cluster-bootstrap
- [ ] Умеете писать Helm-чарт: [helm_chart_lab](../helm/helm_chart_lab.md)
- [ ] Умеете делать Deployment и откатывать его: lab-08-deployment-rolling-update
- [ ] Знаете, как работают probes: lab-09-readiness-probe-rollback
- [ ] Умеете включать автомасштабирование: lab-29-requests-limits-hpa
- [ ] Работает metrics-server (нужен HPA): lab-28-metrics-server-top
- [ ] Есть GitLab (gitlab.com или self-hosted: [gitlab_in_kind](../gitlab/gitlab_in_kind.md)) и runner: [gitlab_runner_lab](../gitlab/gitlab_runner_lab.md). Для job `build` (Docker-in-Docker) нужен **privileged** runner.
- [ ] Прошли сканирование образа: [gitlab_ci_lab_basic_trivy_scan](../gitlab/gitlab_ci_lab_basic_trivy_scan.md)
- [ ] Прошли DevSecOps-лабораторную: [gitlab_ci_lab_devsecops](../gitlab/gitlab_ci_lab_devsecops.md)
- [ ] Прошли публикацию чарта из CI: [helm_gitlab_cicd_lab](../helm/helm_gitlab_cicd_lab.md)
- [ ] Подключили кластер через GitLab Agent: [gitlab_agent_k8s_lab](../gitlab/gitlab_agent_k8s_lab.md)
- [ ] Стек метрик установлен: [lab-01-kube-prometheus-stack](../prometheus-monitoring/lab-01-kube-prometheus-stack.md)
- [ ] Умеете подключать метрики и алерты: [lab-02-servicemonitor-and-alerts](../prometheus-monitoring/lab-02-servicemonitor-and-alerts.md)
- [ ] Умеете маршрутизировать алерты: [lab-04-alertmanager-routing](../prometheus-monitoring/lab-04-alertmanager-routing.md)
- [ ] Установлен Loki и сборщик логов Alloy: [lab-08-loki-logs](../prometheus-monitoring/lab-08-loki-logs.md)

Опционально для подписи образа: [gitlab_ci_lab_cosign](../gitlab/gitlab_ci_lab_cosign.md). Требует privileged runner и gitlab.com.

**gitlab.com и privileged runner.** Shared runners gitlab.com поддерживают privileged-режим для DinD, но для их использования нужна верификация аккаунта, а бесплатный тариф ограничен квотой compute minutes. **проверь:** верификацию аккаунта и остаток квоты в Settings → Usage quotas перед стартом.

## Исходные данные

Создайте новый проект `capstone-app` в GitLab. Приложение: **podinfo**, закреплённый тег `ghcr.io/stefanprodan/podinfo:6.1.4` (тег есть в списке тегов ghcr.io; **проверь** перед стартом: `docker manifest inspect ghcr.io/stefanprodan/podinfo:6.1.4`).

Что нужно от приложения (всё есть у podinfo):

- `/healthz` и `/readyz` для probes;
- `/metrics` с HTTP-метриками (для ServiceMonitor и алерта);
- `/status/{code}` для ручной проверки кодов ответа;
- `POST /fault_injection/enable` переводит все прикладные эндпоинты в HTTP 500, а пробы и `/metrics` остаются здоровыми. Этим вы создаёте искусственную серию 5xx. **проверь** на вашем теге: `curl -X POST localhost:9898/fault_injection/enable`.

Не пишите приложение с нуля: цель лабораторной в инфраструктуре. Dockerfile в `app/` — тонкая обёртка над закреплённым образом (`FROM ghcr.io/stefanprodan/podinfo:6.1.4`). Порт HTTP по умолчанию 9898 — **проверь** по конфигурации вашего образа.

Структура репозитория:

```
capstone-app/
├── app/                # Dockerfile (FROM ghcr.io/stefanprodan/podinfo:6.1.4)
├── chart/              # Helm-чарт (по helm_chart_lab)
└── .gitlab-ci.yml
```

## Этапы

Каждый этап закрывается проверкой **«Готово, когда»**. Не переходите дальше, пока проверка не прошла.

### Этап 1. Pipeline: lint, build, scan (30–40 мин)

Соберите pipeline из четырёх job: `lint`, `build`, `secret-detection`, `trivy`. Каждый job указывает `image:` явно.
Образец для `build` и `trivy`: [gitlab_ci_lab_basic_trivy_scan](../gitlab/gitlab_ci_lab_basic_trivy_scan.md). Образы закрепите тегами: `docker:27` и `docker:27-dind` для `build`, `aquasec/trivy:0.65.0` для `trivy` (в образце стоит `latest`, не копируйте его).
Образец для `secret-detection`: [gitlab_ci_lab_devsecops](../gitlab/gitlab_ci_lab_devsecops.md) (gitleaks-job; это сканер секретов, а не SAST).

Стадии: `lint` → `build` → `scan` (`secret-detection`, `trivy`) → `package` → `publish` → `deploy`. Job `deploy` обязан иметь `needs: [secret-detection, trivy, publish]`, иначе провал скана не остановит поставку.

`build` собирает образ через Docker-in-Docker, как в trivy-лабе. Этому job нужен **privileged runner**: на gitlab.com он есть (см. оговорку выше), на локальном kind-runner включите `privileged: true` (см. [gitlab_runner_lab](../gitlab/gitlab_runner_lab.md)).

Порог для Trivy: pipeline падает на `CRITICAL`.
Образ тегируйте по `$CI_COMMIT_SHORT_SHA`, не `latest`. Базовый образ в `app/Dockerfile` закреплён тегом (`6.1.4`), не `latest`. Не копируйте `hello-world:latest` из trivy-лабы. **проверь:** прогоните `trivy image --severity CRITICAL ghcr.io/stefanprodan/podinfo:6.1.4` до первого запуска pipeline. Если CRITICAL есть, выберите более новый тег podinfo или оформите исключение по кейсу 2 из деки.

**Готово, когда:**
- [ ] pipeline зелёный, все четыре job выполнены;
- [ ] в Container Registry лежит образ с тегом из SHA коммита;
- [ ] артефакт отчёта Trivy скачивается из job;
- [ ] на тестовой ветке с поддельным секретом job `secret-detection` красный (используйте gitleaks-job из devsecops-лабы: он работает на Free).

### Этап 2. Чарт и публикация (30 мин)

Добавьте job `package` и `publish` по образцу [helm_gitlab_cicd_lab](../helm/helm_gitlab_cicd_lab.md). Берите оттуда **только** `package` и `publish`: deploy-job той лабы использует `KUBE_CONFIG` и здесь запрещён. Для job с Helm используйте образ `alpine/helm:4.2.0` (Helm 4).

Правило версий: чарт пакуется как `helm package chart --version "0.1.${CI_PIPELINE_IID}" --app-version "$CI_COMMIT_SHORT_SHA"`. Так каждый pipeline публикует новую версию чарта и повторный `helm push` не падает на уже существующей.

В `values.yaml` обязательно задайте: `image.repository`, `image.tag` (значение по умолчанию, например `dev`), `readinessProbe` типа `httpGet` на `/healthz`, `livenessProbe`, `resources.requests`, `replicaCount: 2`. Без requests HPA на следующем этапе работать не будет.

Приоритет: `--set image.tag=...` в deploy-job **перекрывает** `values.yaml`. Реальный тег всегда приходит из pipeline, а не из файла.

**Готово, когда:**
- [ ] чарт опубликован в реестр GitLab;
- [ ] `helm lint` в CI проходит без предупреждений;
- [ ] версия чарта и версия приложения различаются и обе видны в `helm show chart` для опубликованной версии.

### Этап 3. Deploy через Agent (30–40 мин)

Добавьте job `deploy` в environment `staging`. Команда взята из job `.deploy` в [gitlab_agent_k8s_lab](../gitlab/gitlab_agent_k8s_lab.md) и отличается только значениями:

```yaml
variables:
  KUBE_CONTEXT: <your-group>/capstone-app:dtu-agent   # контекст GitLab Agent курсового кластера

deploy:staging:
  stage: deploy
  image:
    name: alpine/helm:4.2.0
    entrypoint: [""]
  needs: [secret-detection, trivy, publish]
  environment:
    name: staging
  variables: { RELEASE: capstone, NAMESPACE: capstone }
  script:
    - helm registry login "$CI_REGISTRY" -u "$CI_REGISTRY_USER" -p "$CI_REGISTRY_PASSWORD"
    - |
      helm upgrade --install "$RELEASE" "oci://$CI_REGISTRY_IMAGE/charts/<name>" \
        --version "0.1.${CI_PIPELINE_IID}" \
        --kube-context "$KUBE_CONTEXT" \
        --namespace "$NAMESPACE" --create-namespace \
        --set image.tag="$CI_COMMIT_SHORT_SHA" \
        --rollback-on-failure --timeout 3m --wait
    - helm status "$RELEASE" --kube-context "$KUBE_CONTEXT" --namespace "$NAMESPACE"
```

`<name>` — имя чарта из `Chart.yaml`. Не копируйте deploy-job из helm_gitlab_cicd_lab: он читает kubeconfig из `KUBE_CONFIG`, а это запрещено и штрафуется.

**Готово, когда:**
- [ ] в разделе Operate → Environments виден `staging` со ссылкой на последний deploy;
- [ ] `kubectl -n capstone get pods` показывает все поды `Running` и `READY 1/1`;
- [ ] `helm -n capstone history <release>` показывает минимум одну ревизию со статусом `deployed`.

### Этап 4. Probes и HPA (20–30 мин)

Настройте probes по lab-09-readiness-probe-rollback. Там показан `exec`-вариант, а в этом capstone readiness-probe должна быть `httpGet` на `/healthz`: на ней строится отказ F2. HPA настройте по lab-29-requests-limits-hpa; `minReplicas` и `replicaCount` не меньше 2 (по желанию добавьте PodDisruptionBudget). HPA задаётся в чарте, а не руками через `kubectl`: иначе следующий deploy его сотрёт.

**Готово, когда:**
- [ ] `kubectl -n capstone get hpa` показывает реальные значения метрики, а не `<unknown>`;
- [ ] под нагрузкой число реплик растёт, после её снятия возвращается к минимуму;
- [ ] при удалении одного из двух подов сервис не теряет запросы (проверьте циклом `curl`).

### Этап 5. Метрики, алерт и логи (40–50 мин)

1. Подключите метрики приложения через ServiceMonitor: [lab-02-servicemonitor-and-alerts](../prometheus-monitoring/lab-02-servicemonitor-and-alerts.md). Label `release: monitoring` обязателен, иначе Prometheus не подхватит цель:

   ```yaml
   apiVersion: monitoring.coreos.com/v1
   kind: ServiceMonitor
   metadata:
     name: capstone-app
     labels:
       release: monitoring
   spec:
     selector:
       matchLabels: <labels Service из чарта>
     endpoints:
       - port: http
         path: /metrics
   ```

2. Добавьте правило алерта `CapstoneHighErrorRate` (доля 5xx) в `PrometheusRule` чарта. У правила тоже должен быть label `release: monitoring`. Направьте алерт в webhook-receiver `alert-sink` из [lab-04-alertmanager-routing](../prometheus-monitoring/lab-04-alertmanager-routing.md). Ловушка из lab-04: по умолчанию `alertmanagerConfigMatcherStrategy: OnNamespace` молча отбрасывает алерт, если namespace `AlertmanagerConfig` не совпадает с namespace алерта (`capstone`). Решение описано в шаге 2 той лабы.

   ```yaml
   apiVersion: monitoring.coreos.com/v1
   kind: PrometheusRule
   metadata:
     name: capstone-app
     labels:
       release: monitoring
   spec:
     groups:
       - name: capstone
         rules:
           - alert: CapstoneHighErrorRate
             expr: |
               sum(rate(http_request_duration_seconds_count{namespace="capstone",status=~"5.."}[5m]))
                 /
               sum(rate(http_request_duration_seconds_count{namespace="capstone"}[5m])) > 0.05
             for: 2m
             labels:
               severity: warning
   ```

   **проверь:** имя метрики `http_request_duration_seconds_count` и имя label `status` на вашем теге. Выполните `kubectl -n capstone port-forward svc/capstone-app 9898:9898` и затем `curl -s localhost:9898/metrics | grep http_request`. Если имена другие, поправьте выражение.

3. Подключите сбор логов в Loki через Alloy: [lab-08-loki-logs](../prometheus-monitoring/lab-08-loki-logs.md). Используйте Alloy, а не Promtail (чарт Promtail помечен как deprecated).

ServiceMonitor и правило алерта лежат **в чарте**, а не применяются вручную.

**Готово, когда:**
- [ ] цель `capstone-app` в Prometheus Targets имеет статус `UP`;
- [ ] алерт переходит в `Firing` при искусственной серии 5xx (`POST /fault_injection/enable`) и приходит в receiver;
- [ ] логи подов видны в Grafana Explore по фильтру `{namespace="capstone"}`.

## Инъекции отказов

Теперь преподаватель (или ваш напарник) вносит три поломки по очереди. Для каждой вы **замечаете** симптом, **находите причину** по данным, а не по догадке, **чините через Git**, а не правкой в кластере, и записываете разбор по четырём пунктам ниже.

| # | Поломка | Что внести | Ожидаемый симптом |
|---|---------|------------|-------------------|
| F1 | Неверный тег образа | В `.gitlab-ci.yml` в job `deploy` заменить `--set image.tag="$CI_COMMIT_SHORT_SHA"` на несуществующий тег (правка в `values.yaml` не сработает: `--set` её перекрывает) | Новые поды в `ImagePullBackOff`, deploy красный, Helm откатывает релиз |
| F2 | Падающая readiness probe | В `values.yaml` изменить `path` у `httpGet`-probe на несуществующий, например `/ready-x` | Новые поды `Running`, но `0/1 READY`, rollout не завершается, через `--timeout 3m` deploy красный и Helm откатывает релиз. Старые поды продолжают обслуживать трафик |
| F3 | CVE quality gate | Взять базовый образ со старой версией, где есть `CRITICAL` | Job `trivy` красный, до deploy pipeline не доходит |

**Предусловие для F1 и F2:** до поломки должна существовать хотя бы одна успешная ревизия `deployed`. На первой установке `--rollback-on-failure` не откатывает, а удаляет релиз, и `helm history` будет выглядеть иначе.

### Как разбирать

Запишите для каждой поломки по четырём пунктам:

1. **Симптом:** что вы увидели и где (pipeline, `kubectl`, Grafana).
2. **Доказательство:** какая команда или график показали причину. Например, `kubectl describe pod`, события, лог job, отчёт Trivy.
3. **Исправление:** какой коммит вернул систему в рабочее состояние.
4. **Предотвращение:** какая проверка в pipeline или алерт поймали бы это раньше.

### Подсказки по направлению

- **F1:** смотрите на события пода и на то, что сделал `helm` с релизом. Сравните `helm history` до и после: там появится неудачная ревизия и откат.
- **F2:** во время rollout сравните `READY` и `STATUS`, затем посмотрите endpoints у Service и `helm history` (неудачная ревизия и откат). Старые поды продолжают обслуживать трафик и отвечают 200, поэтому алерт `CapstoneHighErrorRate` молчит: серии 5xx нет. Это не сбой алерта, а свойство этой поломки. Сигнал здесь другой: rollout висит, Deployment имеет недоступные реплики. Добавьте в чарт правило на недоступные реплики (метрика kube-state-metrics `kube_deployment_status_replicas_unavailable > 0` в течение 2 минут) и впишите его в «Предотвращение».
- **F3:** прочитайте отчёт Trivy. Выберите: обновить базовый образ или явно принять риск через документированное исключение. Второй вариант допустим только с обоснованием в MR.

## Приёмка и демонстрация

Перед сдачей пройдите чек-лист. Демо длится 10 минут: вы показываете путь коммита до алерта на живой системе.

- [ ] Один коммит в `main` запускает весь pipeline без ручных шагов, кроме подтверждения deploy в production (если есть).
- [ ] В кластере нет ресурсов, созданных вручную: всё приехало из чарта.
- [ ] Деплой идёт через Agent, секретов кластера в CI-переменных нет.
- [ ] Версии образов и инструментов закреплены, нигде нет `latest`.
- [ ] Откат деплоя проверен руками: `helm rollback` или повторный запуск прошлого pipeline.
- [ ] Для F1, F2, F3 заполнен разбор по четырём пунктам.

## Оценка (100 баллов)

| Критерий | Баллы |
|----------|-------|
| Pipeline: lint, build, secret-detection, Trivy с порогом | 15 |
| Чарт публикуется из CI, версии закреплены | 10 |
| Deploy через Agent, environment, автооткат | 15 |
| Probes, requests и HPA заданы в чарте и работают | 10 |
| ServiceMonitor, алерт и receiver работают | 10 |
| Логи в Loki, найдены нужные строки | 10 |
| Разбор F1 (тег образа) | 10 |
| Разбор F2 (readiness) | 10 |
| Разбор F3 (CVE quality gate) | 10 |
| **Итого** | **100** |

**Шкала:** 90–100 отлично · 75–89 хорошо · 60–74 зачёт · меньше 60 доработка.

**Штрафы:** −10 за каждый ручной `kubectl apply` в обход чарта, −10 за kubeconfig в переменной CI, −5 за `latest`.

## Частые проблемы

- **Pipeline зелёный, а в кластере старая версия.** Проверьте, что deploy использует новый тег и `--version "0.1.${CI_PIPELINE_IID}"`: без `--version` Helm берёт последнюю версию чарта, а не ту, что собрал этот pipeline.
- **`<unknown>` в HPA.** Нет `resources.requests` или не установлен metrics-server: см. lab-28-metrics-server-top.
- **Target в Prometheus отсутствует.** У ServiceMonitor не совпал label релиза Prometheus (`release: monitoring`): см. [lab-02-servicemonitor-and-alerts](../prometheus-monitoring/lab-02-servicemonitor-and-alerts.md).
- **Job `build` падает с `Cannot connect to the Docker daemon`.** Runner не privileged: см. [gitlab_runner_lab](../gitlab/gitlab_runner_lab.md). Ошибка `docker: not found` означает, что в образе job нет docker CLI: задайте образ с docker CLI в `image:`.
- **Не хватает памяти.** Остановите всё лишнее; GitLab и мониторинг вместе занимают больше всего.

## Очистка

После демо выполните очистку в таком порядке:

1. Полная очистка курсового кластера и связанных ресурсов: [module-teardown](../prometheus-monitoring/module-teardown.md).
2. В GitLab остановите и удалите environment `staging` (Operate → Environments).
3. Удалите GitLab Agent `dtu-agent` (Operate → Kubernetes clusters).
4. Удалите проект `capstone-app`, если он больше не нужен.

## Что вы проверили

Вы прошли полный путь поставки: от кода до наблюдаемого сервиса в кластере. Вы также показали, что умеете диагностировать отказы на стыке инструментов, а это и есть повседневная работа DevOps-инженера.
