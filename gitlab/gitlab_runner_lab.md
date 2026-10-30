# Lab: GitLab Runner — установка, регистрация, executors

Runner — отдельный бинарь, не часть GitLab-сервера. Он long-poll'ит Jobs API («есть джобы для меня?») и исполняет их в одном из executors. В этой лабе вы поднимаете два runner'а руками: **docker executor** на своей машине и **kubernetes executor** через Helm chart в kind — и доказываете, что job выполнился именно на них.

На Servercore сначала пройдите [подготовку](servercore_gitlab.md). Встроенный group runner уже использует Kubernetes executor. Эта лаба создаёт дополнительные runner'ы с тегами `lab-docker` / `lab-k8s`; обычные untagged jobs может забрать встроенный group runner. Эксперимент с pending требует временно отключить его для проекта. Для Kubernetes-части используйте существующий kind-кластер VM и пропустите `kind create cluster --name runner-lab`; в cleanup не удаляйте кластер стенда.

**Время:** 40–60 минут
**Сложность:** ★★☆☆☆
**Требует:** проект в GitLab (gitlab.com free или self-hosted из [`gitlab_in_kind.md`](gitlab_in_kind.md)), `docker` или `podman`, `kind`, `kubectl`, `helm`

> **Версия GitLab.** Лаба использует authentication token `glrt-...` (создание runner'а в UI). Проверь на своём GitLab, что в **Settings → CI/CD → Runners** есть кнопка **New project runner**. Если её нет, версия слишком старая для этого flow.
>
> **Ресурсы.** Шаг 5 создаёт второй kind-кластер. Если у тебя уже работает GitLab из `gitlab_in_kind.md` (ему нужно 16 GiB RAM), второй кластер рядом, скорее всего, не поместится. Либо используй gitlab.com, либо останови кластер с GitLab на время шага 5.
>
> **Встроенный runner.** GitLab из `gitlab_in_kind.md` уже содержит свой runner (instance-scoped, privileged). В списке runner'ов их будет два: встроенный и ваш. Job'ы с тегами `lab-docker` / `lab-k8s` встроенный runner не возьмёт, но untagged job'ы может забрать он. Смотри `$CI_RUNNER_DESCRIPTION` в логе.
>
> **Стенд Servercore.** GitLab: `https://<gitlab-host>` (доверенный сертификат, TLS не отключайте), git только по HTTPS. В вашей группе уже есть unprivileged group runner (kubernetes executor, теги `lab-1`/`lab-2` и `kubernetes`, untagged включён; точные теги смотрите в **Settings > CI/CD > Runners**). Docker daemon на VM нет (там Podman для kind), поэтому для шага 2 используйте Podman-вариант или пропустите его и сделайте шаг 5. Имя GitLab резолвится в приватный IP в job Pod'ах встроенного runner; в вашем новом kind-кластере и в docker executor этого может не быть (см. шаги 2 и 5).

---

## Модель за 30 секунд

| Понятие | Что это |
|---------|---------|
| Runner | процесс, который забирает jobs у GitLab |
| Executor | как именно runner исполняет job: `shell`, `docker`, `kubernetes`, ... |
| Tags | метки runner'а; job с `tags:` попадёт только на runner с этими метками |
| Scope | instance / group / project runner |
| `config.toml` | локальный конфиг runner'а: `concurrent`, executor, image, privileged |

> Shell executor запускает код прямо на хосте — в проде не используем. Docker и Kubernetes дают чистое окружение на каждый job.

---

## Шаг 1. Создаём runner в UI и получаем токен

Регистрация идёт через **authentication token** (`glrt-...`), а не через старый registration token.

1. Проект → **Settings → CI/CD → Runners → New project runner**
2. Tags: `lab-docker` (для второго runner'а позже — `lab-k8s`)
3. Снимите **Run untagged jobs** — runner берёт только job'ы со своим тегом
4. **Create runner** → скопируйте токен `glrt-...` (показывается один раз)

```bash
export GITLAB_URL=https://gitlab.com            # self-hosted из gitlab_in_kind.md: http://gitlab.<IP>.nip.io; стенд Servercore: https://<gitlab-host>
export RUNNER_TOKEN=glrt-xxxxxxxxxxxxxxxxxxxx   # не коммитьте и не вставляйте токен в командную строку register
```

> Теги, `run untagged`, `locked` и `protected` задаются **при создании runner'а в UI**, а не флагами `register`. Это отличие нового flow от старого.

---

## Шаг 2. Docker executor на локальной машине

> На VM стенда `docker` нет, есть Podman. Можно попробовать `alias docker=podman` (**не проверено** для вашей VM; сокет настраивается по врезке про Podman ниже) или пропустить этот шаг и сделать шаг 5.

Runner сам запускается в контейнере, а job'ы он порождает как соседние контейнеры через сокет.

```bash
docker volume create gitlab-runner-config

docker run -d --name gitlab-runner --restart always \
  -v gitlab-runner-config:/etc/gitlab-runner \
  -v /var/run/docker.sock:/var/run/docker.sock \
  gitlab/gitlab-runner:latest
```

> **Безопасность.** Монтирование docker-сокета даёт job'ам root-доступ к хосту. Для лабы допустимо, на общем runner'е — нет.
>
> **Podman.** Команды выше написаны для `docker`. Если у тебя Podman:
> - Linux: включи сокет (`systemctl --user enable --now podman.socket`) и смонтируй его путь из `podman info --format '{{.Host.RemoteSocket.Path}}'`.
> - macOS: этот путь лежит на хосте, а контейнеры работают внутри Podman VM, поэтому прямой bind mount обычно не работает. Проверь путь к сокету внутри VM через `podman machine ssh` и смонтируй его. Не заработало: используй gitlab.com с shared runner'ами или kubernetes executor из шага 5.
>
> `:latest` годится только для лабы. В реальной установке пинят версию runner'а, равную версии GitLab-сервера (major.minor).

Регистрируем. Токен не передаём флагом `--token`: он попал бы в список процессов и историю shell. Команда спросит его интерактивно, вставьте значение `$RUNNER_TOKEN` в приглашение (`echo "$RUNNER_TOKEN"` в соседнем терминале; ввод может отображаться на экране):

```bash
docker exec -it gitlab-runner gitlab-runner register \
  --url "$GITLAB_URL" \
  --executor docker \
  --docker-image alpine:3.22 \
  --description "lab-docker-runner"
```

> На стенде, если имя GitLab не резолвится из job-контейнеров, добавьте флаг `--docker-extra-hosts "<gitlab-host>:<приватный-IP>"` (проверьте имя флага через `docker exec gitlab-runner gitlab-runner register --help`).

Проверяем в UI: **Settings → CI/CD → Runners** — runner со статусом **online** (зелёная точка).

**Expect:** в `docker logs gitlab-runner` нет ошибок регистрации; `docker exec gitlab-runner cat /etc/gitlab-runner/config.toml` содержит блок `[[runners]]` с `executor = "docker"`; в UI runner `lab-docker-runner` online.

---

## Шаг 3. Читаем `config.toml`

```bash
docker exec gitlab-runner cat /etc/gitlab-runner/config.toml
```

```toml
concurrent = 1
check_interval = 0

[[runners]]
  name = "lab-docker-runner"
  url = "https://gitlab.com"
  token = "glrt-..."
  executor = "docker"
  [runners.docker]
    image = "alpine:3.22"
    privileged = false
    volumes = ["/cache"]
```

| Поле | Зачем |
|------|-------|
| `concurrent` (верхний уровень) | сколько jobs этот процесс гоняет одновременно, по всем `[[runners]]` |
| `[runners.docker] image` | default-образ, если в job нет `image:` |
| `privileged` | нужен для Docker-in-Docker (см. лабы [Trivy](gitlab_ci_lab_basic_trivy_scan.md) и [Cosign](gitlab_ci_lab_cosign.md)) |

Поднимем параллелизм:

```bash
docker exec gitlab-runner sed -i 's/^concurrent = .*/concurrent = 4/' /etc/gitlab-runner/config.toml
```

Применить изменение надёжнее всего перезапуском контейнера (config.toml не перечитывается при каждом изменении файла, проверь в логах `docker logs gitlab-runner`):

```bash
docker restart gitlab-runner
```

Для Docker-in-Docker (лабы Trivy и Cosign) нужен `privileged = true` в `[runners.docker]`:

```bash
docker exec gitlab-runner sed -i 's/privileged = false/privileged = true/' /etc/gitlab-runner/config.toml
docker restart gitlab-runner
```

> Это включать нужно только для DinD. Верни `false`, когда эти лабы закончены.

---

## Шаг 4. Доказываем, что job выполнился на нашем runner'е

Создайте в проекте `.gitlab-ci.yml`:

```yaml
stages: [test]

on-docker:
  stage: test
  tags: [lab-docker]
  image: alpine:3.22
  script:
    - echo "runner: $CI_RUNNER_DESCRIPTION  (id=$CI_RUNNER_ID)"
    - echo "tags: $CI_RUNNER_TAGS"
    - cat /etc/os-release | head -2
    - hostname
```

Push → **Build → Pipelines**. В логе job'а первые строки содержат `Running with gitlab-runner ... on lab-docker-runner`. `hostname` — ID контейнера, а не имя вашей машины.

**Expect:** job `on-docker` зелёный; в логе `runner: lab-docker-runner`, `tags: lab-docker`, а `hostname` не совпадает с именем вашей VM.

Эксперимент с тегами: сначала отключите **Enable instance runners for this project** (Settings → CI/CD → Runners), а на стенде Servercore ещё и **Enable group runners**. Иначе встроенный runner (instance runner из `gitlab_in_kind.md` или group runner стенда) возьмёт untagged job, и `pending` не будет. Затем уберите `tags:` из job'а и запушьте. Job повиснет в `pending` с пометкой «stuck» (точный текст зависит от версии GitLab, проверь на своём) — ни один из ваших runner'ов не берёт untagged. После эксперимента включите instance и group runners обратно, если они нужны другим лабам. **Expect:** untagged job остаётся в `pending`; после возврата `tags: [lab-docker]` и push он выполняется.

---

## Шаг 5. Kubernetes executor через Helm chart в kind

Создайте второй runner в UI (Шаг 1) с тегом `lab-k8s` и сохраните токен:

```bash
export RUNNER_TOKEN_K8S=glrt-xxxxxxxxxxxxxxxxxxxx   # не коммитьте токен в git
```

Выберите адрес GitLab, доступный **из Pod'а** (не с вашей машины):

- gitlab.com: `https://gitlab.com`
- self-hosted из `gitlab_in_kind.md`: тот же адрес `http://gitlab.<IP>.nip.io`, что и в той лабе, где `<IP>` — IP вашего хоста (не `127.0.0.1`)
- стенд Servercore: `https://<gitlab-host>`. Если проверка ниже не резолвит имя, нужны два места (спросите приватный IP GitLab у оператора): для manager-пода `hostAliases` в values chart (точный ключ проверьте по `helm show values gitlab/gitlab-runner`), а для job-подов, которые делают git clone, блок `[[runners.kubernetes.host_aliases]]` с `ip = "<приватный IP>"` и `hostnames = ["<gitlab-host>"]` внутри `runners.config` под `[runners.kubernetes]`

Создайте кластер и проверьте доступность GitLab из него:

```bash
kind create cluster --name runner-lab
kubectl run curl --rm -it --restart=Never --image=curlimages/curl -- curl -sI "$GITLAB_URL"
```

**Expect:** первая строка ответа `HTTP/2 200` или `302`. `Could not resolve host` означает, что нужны `host_aliases` (см. выше).

```bash
helm repo add gitlab https://charts.gitlab.io/
helm repo update gitlab

# версию chart'а не берём из головы — смотрим доступные и выбираем
helm search repo gitlab/gitlab-runner --versions | head -5
```

Перед записью values проверьте имена ключей для выбранной версии chart'а (они зависят от версии):

```bash
helm show values gitlab/gitlab-runner --version <ВЕРСИЯ_ИЗ_search> | grep -nE 'gitlabUrl|runnerToken|runners:|config:'
```

Файл `runner-values.yaml` (токен сюда не пишем, он передаётся в `helm` через `--set`):

```yaml
gitlabUrl: https://gitlab.com          # ← ваш GitLab; адрес из проверки выше
rbac:
  create: true
concurrent: 4
runners:
  privileged: false
  config: |
    [[runners]]
      [runners.kubernetes]
        image = "alpine:3.22"
        namespace = "{{ default .Release.Namespace .Values.runners.jobNamespace }}"
        # Apple Silicon: раскомментируйте и возьмите helper той же версии, что и runner
        # helper_image = "registry.gitlab.com/gitlab-org/gitlab-runner/gitlab-runner-helper:arm64-<APP_VERSION>"
```

Строку `namespace = "{{ default .Release.Namespace .Values.runners.jobNamespace }}"` берём из default-конфига chart (`helm show values gitlab/gitlab-runner --version <ВЕРСИЯ>`, строка с `namespace =` в блоке `config`; в chart 0.93.0 это около строки 539). Она задаёт namespace, в котором создаются Pod'ы jobs. Без неё свой `runners.config` её теряет, поэтому пишем её явно.

Для Apple Silicon `<APP_VERSION>` — версия runner'а из chart'а, а не из головы:

```bash
helm show chart gitlab/gitlab-runner --version <ВЕРСИЯ_ИЗ_search> | grep appVersion
```

(Пример формата тега из `gitlab_in_kind.md`: `arm64-v18.11.2`; берите версию, которая совпадает с appVersion вашего chart'а.)

Токен передаём через временный файл, чтобы он не попал в историю shell:

```bash
umask 077
RUNNER_TOKEN_FILE=$(mktemp)
printf '%s' "$RUNNER_TOKEN_K8S" > "$RUNNER_TOKEN_FILE"
helm upgrade --install gitlab-runner gitlab/gitlab-runner \
  --namespace gitlab-runner --create-namespace \
  --version <ВЕРСИЯ_ИЗ_search> \
  -f runner-values.yaml \
  --set-file runnerToken="$RUNNER_TOKEN_FILE"
rm -f "$RUNNER_TOKEN_FILE"

kubectl -n gitlab-runner get pods
```

Runner-manager Pod должен стать `1/1 Running`, а в UI runner `lab-k8s` — online.

> Свой `runners.config` **полностью заменяет** конфиг из chart, включая default `image` и `namespace`. Поэтому `image = "alpine:3.22"` и строка `namespace` заданы явно.

Добавьте job:

```yaml
on-k8s:
  stage: test
  tags: [lab-k8s]
  image: alpine:3.22
  script:
    - echo "runner: $CI_RUNNER_DESCRIPTION"
    - hostname      # имя Pod'а runner-...-project-...
```

В отдельном терминале во время выполнения job'а:

```bash
kubectl -n gitlab-runner get pods -w
```

Для каждого job'а появляется новый Pod `runner-<id>-project-<id>-concurrent-<n>`, который исчезает после завершения. Это и есть «Pod per job».

**Expect:** job `on-k8s` зелёный; в `kubectl get pods -w` появился и исчез Pod `runner-...`; в логе `runner: ...` совпадает с описанием runner `lab-k8s`, `hostname` — имя этого Pod'а.

---

## Шаг 6. Protected и locked runners

В **Settings → CI/CD → Runners → ✏️ (edit)** у runner'а:

| Опция | Эффект |
|-------|--------|
| **Protected** | берёт jobs только с protected branches/tags — для runner'ов с доступом к prod |
| **Lock to current projects** (название в UI зависит от версии) | нельзя переиспользовать в других проектах |
| **Run untagged jobs** | брать jobs без `tags:` |

Упражнение: включите **Protected** на `lab-docker`, запустите pipeline из feature-ветки (не protected) — job зависнет. Запустите из `main` — отработает.

**Expect:** job из feature-ветки остаётся `pending`, из `main` проходит; после упражнения снимите **Protected**.

---

## Известные проблемы

| Симптом | Причина | Решение |
|---------|---------|---------|
| Job висит `pending`, «stuck» | нет online runner'а с нужными `tags:` либо runner не берёт untagged | совпадение тегов или включить **Run untagged jobs** |
| `ERROR: no image specified and no default image configured` | свой `config.toml`/`runners.config` затёр default `image` | задать `image` в `[runners.docker]` / `[runners.kubernetes]` или `image:` в каждом job |
| `Cannot connect to the Docker daemon` в job с `docker build` | DinD не настроен | runner с `privileged = true` + `services: docker:dind`, либо монтировать сокет (небезопасно); на стенде Servercore попросите оператора включить privileged на вашей VM или собирайте образы через buildah/kaniko (без Docker) |
| Runner в kind (arm64) висит на `Pulling image` для helper | пытается скачать x86_64 helper | `helper_image = "...gitlab-runner-helper:arm64-<версия>"` в `[runners.kubernetes]` |
| Runner online в UI, но job не стартует в kind-GitLab | Pod не резолвит `gitlab.<IP>.nip.io` или нет маршрута | проверить `kubectl -n gitlab-runner logs <pod>`; использовать адрес, достижимый из кластера |
| `403 / invalid token` при `register` | старый registration token в новом flow (или наоборот) | создать runner в UI и использовать `glrt-...` токен |

---

## Очистка

Строки с `docker` нужны только если вы делали шаг 2; `kind delete cluster --name runner-lab` нужен только если вы создавали этот кластер (на стенде его нет: кластер VM не удаляйте, вместо этого `helm uninstall` ниже).

```bash
docker rm -f gitlab-runner && docker volume rm gitlab-runner-config
docker ps -a --filter name=runner- -q | xargs -r docker rm -f   # job-контейнеры docker executor
helm -n gitlab-runner uninstall gitlab-runner
kind delete cluster --name runner-lab
unset RUNNER_TOKEN RUNNER_TOKEN_K8S
rm -f runner-values.yaml
```

В UI: **Settings → CI/CD → Runners →** удалите оба runner'а, иначе в списке останутся «offline».

Верните проект в исходное состояние: уберите `tags:` и `.gitlab-ci.yml`, созданные в лабе, если они вам больше не нужны, и снимите **Protected** (шаг 6) с runner'а, если он остаётся в использовании.

---

## Что вы узнали

- Runner ≠ GitLab: отдельный процесс, подключается по токену, executor определяет среду выполнения.
- Теги и `run untagged` — единственный способ направить job на конкретный runner.
- `concurrent` в `config.toml` — реальный потолок параллелизма, а не количество stages.
- Kubernetes executor = Pod на каждый job; свой `runners.config` заменяет default целиком.
