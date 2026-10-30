# LAB: Деплой из GitLab pipeline в Kubernetes через GitLab Agent

**Duration:** 60–90 min
**Difficulty:** ★★★☆☆
**Requires:** kind-кластер, GitLab с включённым **KAS** (см. Шаг 0; на стенде Servercore — [servercore_gitlab.md](servercore_gitlab.md)), Helm-чарт из [helm_gitlab_cicd_lab.md](../helm/helm_gitlab_cicd_lab.md), `helm`, `kubectl`, опционально `glab`

> Пометка **проверь:** означает факт, который не подтверждён запуском при написании лабы. Сверьтесь с документацией вашей версии GitLab/Helm или выполните указанную команду.

В [helm_gitlab_cicd_lab.md](../helm/helm_gitlab_cicd_lab.md) деплой-джоба получала доступ к кластеру через переменную `KUBE_CONFIG` — base64-kubeconfig, лежащий в CI/CD variables. Это работает, но **любой, кто может запустить pipeline, получает админский ключ от кластера**. В этой лабе заменяем его на **GitLab Agent for Kubernetes (agentk)**: агент живёт внутри кластера и сам подключается к GitLab, а kubeconfig и креды кластера **не хранятся в CI/CD variables**. Агент не делает деплой безопасным сам по себе: тот, кто может запускать pipeline в авторизованном проекте, деплоит с правами агента (см. сравнение в конце).

```text
 Подход 1: KUBE_CONFIG (push)                Подход 2: Agent (pull / tunnel)

 runner ──kubeconfig──► kube-apiserver       runner ──► GitLab KAS ◄── agentk ──► kube-apiserver
   │  (cluster creds в CI variable)                     (постоянных кредов кластера в CI нет;
   └─ API кластера должен быть доступен                  агенту нужен только исходящий доступ)
      из сети runner-а
```

---

## Что мы построим

1. Зарегистрируем агента в проекте и установим его в kind через Helm.
2. Авторизуем CI-доступ (`ci_access`) и деплоим чарт из pipeline в `staging`.
3. Добавим `environment:` с URL, ручной гейт на `production` и откат.
4. Научимся смотреть состояние pipeline и настроим сигнал о падении.

---

## Шаг 0. Предусловия: KAS

Агент подключается не к GitLab напрямую, а к **KAS** (GitLab Agent Server for Kubernetes).

| GitLab | KAS |
|--------|-----|
| **gitlab.com** | Работает из коробки, адрес показывает UI на шаге регистрации (**проверь:** `wss://kas.gitlab.com`) |
| **Servercore / Linux package** | KAS включён по умолчанию; используйте URL из UI и [Servercore hostAliases](servercore_gitlab.md#деплой-в-kind). Шаги 0.1-0.2 про установку GitLab Helm chart пропустите, начните с Шага 1. |
| **Self-hosted (Helm chart)** | Нужно `global.kas.enabled: true` |
| **Self-hosted в kind из [gitlab_in_kind.md](gitlab_in_kind.md)** | В values стоит `global.kas.enabled: false` — **включите**, иначе регистрация агента не завершится |
| **Стенд Servercore** (GitLab CE 19.x, Linux package, HTTPS) | KAS уже включён. **Шаги 0.1 и 0.2 пропустите.** Адрес KAS: `wss://<gitlab-host>/-/kubernetes-agent/` |

**Стенд Servercore: проверка KAS.** С вашей VM:

```sh
curl -sI https://<gitlab-host>/-/kubernetes-agent/ | head -1
```

**Expect:** `HTTP/2 426` (KAS отвечает и ждёт WebSocket). Если ответа нет или другой код, обратитесь к оператору. Если имя GitLab не резолвится внутри Pod агента, используйте `hostAliases` из [servercore_gitlab.md](servercore_gitlab.md).

Агент также должен иметь исходящий доступ до GitLab/KAS по HTTP(S)/WebSocket. За корпоративным прокси или с SSL-инспекцией соединение может не подняться (см. «Известные проблемы» в [gitlab_in_kind.md](gitlab_in_kind.md)).

### 0.1. Включить KAS в GitLab из `gitlab_in_kind` (только self-hosted в kind; на Servercore пропустить)

Нужны те же файлы и IP, что в `gitlab_in_kind.md` (Шаг 5–6). Если `/tmp/gitlab-kind-values.yaml` и `/tmp/gitlab-charts` уже удалены — воссоздайте их по тем шагам.

1. В `/tmp/gitlab-kind-values.yaml` установите **только** ключ `global.kas.enabled: true` (внутри
   существующего блока `global:`). Остальные значения блока `global` не трогайте — не заменяйте весь блок:

   ```yaml
   global:
     kas:
       enabled: true
   ```

2. Повторите ту же команду Helm — версия чарта и список `-f` те же, что в Шаге 6 `gitlab_in_kind.md`:

   ```bash
   helm upgrade --install gitlab gitlab/gitlab \
     --version 9.11.7 \
     --namespace gitlab --create-namespace \
     --kube-context kind-kind-dtu \
     --set global.hosts.domain=192.168.3.35.nip.io \
     --set certmanager-issuer.email=admin@example.com \
     -f /tmp/gitlab-charts/examples/kind/values-base.yaml \
     -f /tmp/gitlab-charts/examples/kind/values-no-ssl.yaml \
     -f /tmp/gitlab-kind-values.yaml \
     --timeout 20m
   ```

   > Замените `192.168.3.35` на ваш IP. Не используйте `--reuse-values`: он смешает старые значения с новыми.

3. Проверьте, что KAS поднялся и появился Ingress:

   ```bash
   kubectl -n gitlab get pods | grep kas
   # gitlab-kas-...   1/1   Running
   kubectl -n gitlab get ingress | grep kas
   # проверь: хост вида kas.<ваш-IP>.nip.io
   ```

   Если Pod-ы в `Pending` — не хватает памяти: `gitlab_in_kind.md` рассчитан на 16 GiB, KAS добавляет ещё один Pod. Смотрите `kubectl -n gitlab describe pod <pod>`.

### 0.2. Проверить, что агент достучится до KAS (только kind без TLS; на Servercore пропустить)

Агент ходит к KAS **изнутри Pod-а**, поэтому адрес должен резолвиться и открываться из кластера, а не только с вашего ноутбука:

```bash
kubectl run kas-check --rm -it --restart=Never --image=curlimages/curl --context kind-kind-dtu -- \
  curl -sI http://kas.192.168.3.35.nip.io/
```

Любой HTTP-ответ (даже 4xx) означает, что сеть и DNS работают. `Could not resolve host` или таймаут — проблема сети/DNS; тогда **проверь:** адрес Service KAS через `kubectl -n gitlab get svc | grep kas` и используйте его в `kasAddress`.

Для kind без TLS адрес KAS начинается с `ws://`, а не `wss://`. Точное значение для `--set config.kasAddress` показывает UI на шаге регистрации (Шаг 2).

---

## Шаг 1. Конфиг агента в репозитории

Агент описывается файлом в проекте. Создайте `.gitlab/agents/dtu-agent/config.yaml`:

```yaml
ci_access:
  projects:
    - id: <your-group>/<your-project>   # путь проекта, которому разрешено ходить в кластер
```

`ci_access` — это «белый список»: **только перечисленные проекты/группы** смогут использовать агента из CI. Без этого блока pipeline получит ошибку вида `no agents available` (точный текст зависит от версии).

> Имя папки `.gitlab/agents/<имя>/` должно **в точности** совпадать с именем агента, которого вы выберете в UI на Шаге 2 (здесь `dtu-agent`). Иначе конфигурации нет, `ci_access` не действует и в job пустой kubeconfig.

```sh
git add .gitlab/agents/dtu-agent/config.yaml
git commit -m "feat: register dtu-agent"
git push
```

---

## Шаг 2. Регистрация и установка агента

1. В GitLab: **Operate → Kubernetes clusters → Connect a cluster**.
2. Выберите агента `dtu-agent` из списка (он найден по папке из шага 1).
3. GitLab покажет **токен** (показывается один раз) и адрес KAS.

Версию чарта не берите из памяти — узнайте актуальную и зафиксируйте:

```sh
helm repo add gitlab https://charts.gitlab.io
helm repo update
helm search repo gitlab/gitlab-agent --versions | head -5
export AGENT_CHART_VERSION=<версия-из-вывода>
```

> Версия chart агента должна совпадать с minor-версией GitLab: колонка `APP VERSION` должна быть вашей minor (стенд: GitLab 19.5 -> chart `2.33.x`, агент `v19.5`). Агент новее сервера может не подключиться. На gitlab.com KAS обновляет GitLab; на self-hosted в kind (chart `9.11.7`, GitLab 18.11) берите chart агента с APP VERSION 18.11, **проверь** по `helm search`.

Токен **не** передавайте в `--set` (попадёт в историю shell и `ps`). Сохраните его во временный файл и передайте через `--set-file`:

```sh
read -rs AGENT_TOKEN              # вставьте токен из UI (ввод не отображается), Enter
umask 077
printf '%s' "$AGENT_TOKEN" > /tmp/agent-token
unset AGENT_TOKEN

export KAS_ADDRESS=<адрес-из-UI>   # Servercore: wss://<gitlab-host>/-/kubernetes-agent/; kind без TLS: ws://kas.192.168.3.35.nip.io

helm upgrade --install dtu-agent gitlab/gitlab-agent \
  --version "$AGENT_CHART_VERSION" \
  --namespace dtu-agent --create-namespace \
  --set-file config.token=/tmp/agent-token \
  --set config.kasAddress="$KAS_ADDRESS"

rm -f /tmp/agent-token
```

> **проверь:** имена ключей `config.token` и `config.kasAddress` по `helm show values gitlab/gitlab-agent --version "$AGENT_CHART_VERSION"`. Helm всё равно хранит значения релиза в Secret namespace `dtu-agent`, поэтому токен виден тем, кто читает Secrets этого namespace.

Проверка:

```sh
kubectl -n dtu-agent get pods
# dtu-agent-gitlab-agent-...   1/1   Running
```

**Expect:** Pod `1/1 Running`, а в **Operate → Kubernetes clusters** статус агента `dtu-agent` сменился на **Connected**. Если нет — смотрите логи: `kubectl -n dtu-agent logs deploy/dtu-agent-gitlab-agent`.

> **Права агента.** Chart по умолчанию создаёт ClusterRoleBinding на `cluster-admin`: любой pipeline проекта из `ci_access` получает полный доступ к кластеру. Для этой лабы допустимо. Лаба [полного цикла](gitlab_ci_lab_full_cycle.md) требует **отдельного** агента с ограниченным ServiceAccount (RBAC в одном namespace и lease-права в namespace агента); этого агента там не переиспользуйте. Здесь агент стоит в namespace `dtu-agent`, чтобы не пересекаться с `gitlab-agent` из full-cycle.

> Токен агента — это секрет. Не коммитьте его и не вставляйте в чат; в UI он показывается один раз, при потере — создайте новый токен.

---

## Шаг 3. Как CI «видит» кластер

Когда проект авторизован в `ci_access`, GitLab кладёт в джобы этого проекта `KUBECONFIG` с контекстом вида:

```text
<path/to/agent-project>:<agent-name>
# например: mygroup/my-project:dtu-agent
```

(Только проекты и группы из `ci_access` получают этот контекст. **проверь:** детали по документации вашей версии GitLab.)

Выберите контекст одним из двух способов:

```yaml
# вариант A: переменная + helm/kubectl флаг (не маскируйте KUBE_CONTEXT, если вынесете её в CI/CD variables: это не секрет, а значение с `:` и `/` не пройдёт masked)
variables:
  KUBE_CONTEXT: mygroup/my-project:dtu-agent
# ... helm upgrade --install ... --kube-context "$KUBE_CONTEXT"

# вариант B: переключить контекст в самой джобе
script:
  - kubectl config get-contexts
  - kubectl config use-context mygroup/my-project:dtu-agent
```

Никакого `base64 -d > kubeconfig` больше нет: `KUBECONFIG` готов до запуска `script:`.

---

## Шаг 4. Pipeline: деплой через агента

Берём pipeline из [helm_gitlab_cicd_lab.md](../helm/helm_gitlab_cicd_lab.md) (стадии `lint → validate → package → publish`) **без изменений** и заменяем только блок `deploy`. **Не копируйте** deploy-джобу из той лабы: она читает `KUBE_CONFIG`, а здесь доступ идёт через агента.

Образ Helm — `alpine/helm:4.2.0` (Helm 4): флаг `--rollback-on-failure` есть в Helm 4. Тег вынесен в переменную `HELM_IMAGE_TAG`. Проверка версии образа:

```sh
podman run --rm --entrypoint helm docker.io/alpine/helm:4.2.0 version   # на VM Podman; где есть Docker: docker run ...
```

```yaml
variables:
  KUBE_CONTEXT: <your-group>/<your-project>:dtu-agent
  HELM_IMAGE_TAG: 4.2.0

.deploy:
  image:
    name: alpine/helm:$HELM_IMAGE_TAG
    entrypoint: [""]
  stage: deploy
  script:
    - echo "$CI_REGISTRY_PASSWORD" | helm registry login "$CI_REGISTRY" -u "$CI_REGISTRY_USER" --password-stdin
    - CHART_NAME=$(grep '^name:' "$CHART_DIR/Chart.yaml" | awk '{print $2}')
    - CHART_VERSION=$(grep '^version:' "$CHART_DIR/Chart.yaml" | awk '{print $2}')
    - |
      helm upgrade --install "$RELEASE" "$CHART_REGISTRY/$CHART_NAME" \
        --version "$CHART_VERSION" \
        --kube-context "$KUBE_CONTEXT" \
        --namespace "$NAMESPACE" --create-namespace \
        ${IMAGE_TAG:+--set image.tag=$IMAGE_TAG} \
        --rollback-on-failure --timeout 3m --wait
    - helm status "$RELEASE" --kube-context "$KUBE_CONTEXT" --namespace "$NAMESPACE"

deploy:staging:
  extends: .deploy
  variables: { RELEASE: app-staging, NAMESPACE: staging }
  environment:
    name: staging
    url: http://staging.example.local        # ссылка на странице Environments
  rules:
    - if: '$CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH'

deploy:prod:
  extends: .deploy
  variables: { RELEASE: app-prod, NAMESPACE: prod }
  environment:
    name: production
    url: http://prod.example.local
  rules:
    - if: '$CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH'
      when: manual                           # ручной запуск (не защита, см. Шаг 5)
  allow_failure: false
```

`CHART_VERSION` берётся из `Chart.yaml` коммита этого pipeline: деплоится тот чарт, что опубликовала стадия `publish`. Поэтому **поднимайте `version:` в `Chart.yaml` при каждом изменении чарта**. Если версия уже опубликована, `helm push` может завершиться ошибкой: тогда поднимите `version:`. Не добавляйте `|| true` к `helm push`, иначе скроете реальные ошибки публикации. **проверь:** перезаписывает ли GitLab OCI-реестр существующую версию при повторном push (на стенде не проверяли).

Запушьте в `main`, затем проверьте результат в кластере:

```sh
kubectl get deploy,pods -n staging
helm list -A
```

**Expect:** pipeline зелёный (`deploy:staging` выполнен, `deploy:prod` ждёт клика); в `staging` Pods `Running`; `helm list -A` показывает `app-staging`; **Operate → Environments** содержит `staging`.

> **проверь:** утверждение «`alpine/helm` не содержит `kubectl`»: `podman run --rm --entrypoint sh docker.io/alpine/helm:<тег> -c 'command -v kubectl'`. Если `kubectl` нужен (вариант B из Шага 3), используйте образ с `kubectl`; для Helm достаточно `--kube-context`.

---

## Шаг 5. Environments и откат

**Operate → Environments** показывает для каждого окружения: последний деплой, коммит, автора, ссылку (`url:`) и кнопку **Rollback**.

Откат двумя способами:

| Способ | Что происходит | Когда |
|--------|----------------|-------|
| **Re-deploy старого pipeline** (Environments → deployment → Rollback) | Повторно запускает deploy-джобу того pipeline: его `IMAGE_TAG` (образ, если задавался) и `version:` из его `Chart.yaml` (чарт, через `--version`). Версия чарта различается, только если вы поднимали `version:` | Релиз «зелёный», но плохой по бизнес-логике |
| **`--rollback-on-failure`** в самой джобе | Helm сам откатывает, pipeline красный | Релиз не стал healthy за `--timeout` |

Если бы в команде не было `--version`, повторный запуск поставил бы **последний** чарт из реестра, а не чарт того pipeline — откатился бы только образ.

Проверьте второй способ. Предусловия:

1. **Сначала** запустите обычный деплой в `staging` (без `IMAGE_TAG`) и дождитесь **успешной** ревизии (`helm history app-staging -n staging`). Без здоровой предыдущей ревизии откатывать не на что: при самой первой установке с `--rollback-on-failure` Helm удаляет релиз (**проверь:** поведение вашей версии Helm). Эксперимент запускайте только после этого.
2. Сломать образ нужно через переменную pipeline, а не правкой джобы: в **Build → Pipelines → Run pipeline** задайте `IMAGE_TAG=does-not-exist` (джоба передаёт `image.tag` только когда `IMAGE_TAG` задан).

**Expect:**

- джоба красная, `helm history app-staging -n staging` показывает неудачную ревизию и откат на предыдущую;
- в кластере остаётся рабочая версия, Pods в `Running`.

**Защита прода.** `when: manual` — это только кнопка: любой пользователь, который может запускать pipeline на `main`, нажмёт её. Это не согласование. Для реальной защиты нужны protected environments, approvals и CODEOWNERS (в Free доступно не всё, **проверь:** ваш тариф): [gitlab_ci_lab_five_envs.md](gitlab_ci_lab_five_envs.md).

---

## Шаг 6. Мониторинг pipeline

| Что смотрим | Где |
|-------------|-----|
| Статус pipeline, длительность стадий | **Build → Pipelines**, граф стадий |
| Логи конкретной джобы | Клик по джобе; для деплоя ищите вывод `helm status` |
| Состояние окружений | **Operate → Environments** |
| То же из терминала | `glab ci status`, `glab ci view`, `glab ci trace <job>` (**проверь:** флаги через `glab ci --help` вашей версии) |
| Агент жив? | **Operate → Kubernetes clusters** (Connected / Not connected) |

```sh
glab ci status          # статус pipeline текущей ветки
glab ci trace deploy:staging   # лог джобы в терминал
```

**Сигнал о падении (идея).** Pipeline упал на `main` — об этом нужно узнать без захода в UI:

- уведомления о событии *Failed pipeline*: **проверь:** путь в UI вашей версии (интеграции проекта Slack/Email или персональные уведомления в профиле — это разные настройки);
- либо джоба с `when: on_failure` в последней стадии, которая шлёт `curl` в webhook чата.

```yaml
notify:failure:
  stage: .post
  image: { name: curlimages/curl:latest, entrypoint: [""] }
  script:
    - 'curl -s -X POST "$CHAT_WEBHOOK" -d "pipeline $CI_PIPELINE_URL failed on $CI_COMMIT_REF_NAME"'
  rules:
    - if: '$CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH'
      when: on_failure
```

`CHAT_WEBHOOK` — masked + protected переменная.

---

## Агент vs `KUBE_CONFIG`: сравнение

| Критерий | `KUBE_CONFIG` variable | GitLab Agent |
|----------|------------------------|--------------|
| Где хранятся креды кластера | В CI/CD variables проекта | Токен агента — в Secret кластера и в момент установки в вашем shell; kubeconfig в CI не хранится |
| Направление соединения | Runner → API кластера (push) | agentk → KAS (исходящее из кластера) |
| API кластера открыт наружу | Да, должен быть доступен runner-у | Нет |
| Кто может использовать | Любой, кто запустит pipeline с доступом к переменной | Любой, кто запускает pipeline в проекте из `ci_access`: он деплоит с правами агента |
| Права в кластере | Обычно `cluster-admin` из kubeconfig | По умолчанию `cluster-admin` (chart создаёт ClusterRoleBinding; убедитесь: `kubectl get clusterrolebinding -o wide \| grep gitlab-agent`). Сузить через impersonation (`access_as` в `ci_access`) — функция **Premium** (**проверь тариф**, ключи — по документации вашей версии GitLab). На Free: ограничьте ServiceAccount агента вручную через RBAC (Role/RoleBinding только на нужные namespace; имя SA — `kubectl -n dtu-agent get sa`, **проверь**) |
| Ротация | Заменить переменную в каждом проекте | Новый токен агента |
| Сложность старта | Минимальная | Нужен KAS и установка агента |

---

## Типичные ошибки

Тексты ошибок ниже — ориентировочные: точная формулировка зависит от версии GitLab.

- **`no agents available` / `agent not authorized`** — в `config.yaml` нет `ci_access`, либо `id` проекта указан неверно (нужен полный путь `group/project`).
- **Агент `Not connected`** — не включён KAS (self-hosted), неверный `kasAddress` (`ws://` vs `wss://`), либо Pod агента не видит GitLab по DNS (проверка в Шаге 0.2). Смотрите логи Pod-а агента.
- **Контекст не найден** — имя контекста собирается как `<путь-проекта-агента>:<имя-агента>`; проект, где лежит `.gitlab/agents/`, может отличаться от проекта с pipeline.
- **`kubectl: not found` в джобе** — возможно, в образе `alpine/helm` нет `kubectl` (проверка — в Шаге 4); используйте `helm --kube-context` или другой образ.
- **Токен агента попал в историю** — отзовите его в UI и создайте новый.
- **Прод задеплоился без клика** — забыли `when: manual` в `rules:` (в `rules` оно ставится внутри элемента, а не рядом).
- **`--rollback-on-failure` не распознан** — образ с Helm 3; используйте `alpine/helm:4.2.0` (Helm 4).
- **`ImagePullBackOff` при деплое** — `IMAGE_TAG` задан, но образ с таким тегом не собран. Уберите `IMAGE_TAG` из запуска pipeline.

## Cleanup

Порядок важен: сначала релизы и namespace, потом агент, потом токен и KAS.

```sh
# 1. Релизы, созданные pipeline
helm uninstall app-staging -n staging
helm uninstall app-prod -n prod
kubectl delete ns staging prod                      # только если namespace создан лабой

# 2. Агент в кластере
helm -n dtu-agent uninstall dtu-agent
kubectl delete ns dtu-agent                          # namespace gitlab-agent из full-cycle не трогаем

# 3. Временный файл с токеном (если остался)
rm -f /tmp/agent-token
```

- **Отзыв токена агента:** GitLab UI → **Operate → Kubernetes clusters** → агент `dtu-agent` → вкладка с токенами → **Revoke**. Без этого токен остаётся действующим. **проверь:** путь в UI вашей версии.
- **Переменная `KUBE_CONFIG`:** если вы использовали её из [helm_gitlab_cicd_lab.md](../helm/helm_gitlab_cicd_lab.md), удалите её: **Settings → CI/CD → Variables**. Агентский путь её не использует, но старый kubeconfig с админскими правами не должен оставаться в проекте.
- **Конфиг агента в репозитории:** `git rm -r .gitlab/agents/dtu-agent` и коммит, если он больше не нужен.
- **Откат KAS (только self-hosted):** в `/tmp/gitlab-kind-values.yaml` верните `global.kas.enabled: false` (только этот ключ) и повторите `helm upgrade` из Шага 0.1. Если `/tmp`-файлы удалены, установите значение в исходном values-файле [gitlab_in_kind.md](gitlab_in_kind.md).

---

## Что вы узнали

- Как GitLab Agent заменяет `KUBE_CONFIG`: kubeconfig не лежит в CI/CD variables, соединение идёт из кластера наружу, а права определяются агентом
- Как зарегистрировать агента (`.gitlab/agents/<name>/config.yaml`, `ci_access`) и установить его через `gitlab/gitlab-agent`, не светя токен
- Как выбрать контекст в CI (`KUBE_CONTEXT`, `kubectl config use-context`, `helm --kube-context`)
- Как деплоить `helm upgrade --install --version … --rollback-on-failure` в `staging` автоматически и в `production` по клику
- Как пользоваться Environments, откатом и следить за pipeline из UI и через `glab`
- Чем отличаются gitlab.com и self-hosted (KAS)
