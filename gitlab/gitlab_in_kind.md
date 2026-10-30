# GitLab в kind на Apple Silicon

Полная инструкция по развёртыванию self-hosted GitLab в локальном Kubernetes-кластере (kind) с использованием Podman на macOS Apple Silicon (M1/M2/M3).

**Время:** ~40 минут (из них ~20 мин — ожидание запуска GitLab)

> **Студентам стенда Servercore эта инструкция не нужна:** GitLab там уже установлен (HTTPS, доверенный сертификат), см. [servercore_gitlab.md](servercore_gitlab.md). Эта установка локальная.

---

## Требования к железу

| Ресурс | Минимум | Рекомендовано |
|--------|---------|---------------|
| CPU | 6 ядер | 8 ядер |
| RAM | 16 ГБ | 16 ГБ |
| Диск | 40 ГБ свободно | 60 ГБ |
| macOS | 13 (Ventura) | 14+ |

> GitLab в Kubernetes — ресурсоёмкий стек: webservice, sidekiq, gitaly, postgresql, redis, runner + nginx + registry. 16 ГБ RAM — реальный минимум. На 8 ГБ запустить не получится.

---

## Зависимости

```bash
brew install kind helm kubectl podman
```

---

## Шаг 1. Podman VM (rootful, 16 ГБ)

kind запускается внутри Podman VM. Нужен **rootful** режим — только он позволяет биндить порт 80 на хосте.

```bash
# Создаём VM с нужными ресурсами
podman machine init kind --cpus 6 --memory 16384 --disk-size 60

# Первый запуск — стартуем, переключаем в rootful, рестартуем
podman machine start kind
podman machine stop kind
podman machine set --rootful kind
podman machine start kind
```

Проверяем, что ресурсы применились:

```bash
podman info | grep -E "memTotal|cpus:"
# cpus: 6
# memTotal: 16722067456
```

> **Rootless vs rootful.** В rootless-режиме podman не может биндить порты < 1024 (порт 80). Переключение в rootful решает это без прав суперпользователя на хосте — ограничение убирается только внутри VM.

---

## Шаг 2. GitLab Charts v9.x

GitLab Helm chart **v10.0.0+ убрал** встроенные Redis, PostgreSQL и object storage — для локальной разработки они требуют отдельной настройки. Используем v9.x, где всё ещё есть bundled subcharts.

```bash
git clone --depth 1 --branch v9.11.7 \
  https://gitlab.com/gitlab-org/charts/gitlab.git /tmp/gitlab-charts
```

---

## Шаг 3. kind кластер

Конфиг из репозитория charts настраивает NodePort mapping: порт 80 хоста → NodePort 32080 внутри кластера.

```bash
kind create cluster --name kind-dtu \
  --config /tmp/gitlab-charts/examples/kind/kind-no-ssl.yaml
```

Проверяем:

```bash
kubectl cluster-info --context kind-kind-dtu
```

**Expect:** `Kubernetes control plane is running at https://127.0.0.1:...`.

---

## Шаг 4. Находим IP хоста

GitLab будет доступен через nip.io DNS по IP вашей машины в локальной сети.

```bash
ipconfig getifaddr en0   # Wi-Fi (en0 или en1 в зависимости от интерфейса)
# Пример: 192.168.3.35
```

> nip.io — магический DNS-сервис: `gitlab.192.168.3.35.nip.io` резолвится в `192.168.3.35`. Ничего устанавливать не нужно.

---

## Шаг 5. Values файл

Создаём `/tmp/gitlab-kind-values.yaml` со всеми необходимыми настройками и фиксами:

```yaml
global:
  hosts:
    domain: 192.168.3.35.nip.io   # ← замените на ваш IP
    https: false
  ingress:
    configureCertmanager: false
    tls:
      enabled: false
  shell:
    port: 32022
  kas:
    enabled: false
  pages:
    enabled: false

installCertmanager: false
certmanager:
  install: false

# Обязателен даже при отключённом certmanager — без него helm install падает
certmanager-issuer:
  email: admin@example.com

nginx-ingress:
  controller:
    replicaCount: 1
    service:
      type: NodePort
      nodePorts:
        http: 32080
        gitlab-shell: 32022

prometheus:
  install: false

# arm64: runner без этой настройки пытается скачать x86_64 helper image и зависает
gitlab-runner:
  runners:
    privileged: true
    config: |
      [[runners]]
        [runners.kubernetes]
          image = "alpine:3.22"
          helper_image = "registry.gitlab.com/gitlab-org/gitlab-runner/gitlab-runner-helper:arm64-v18.11.2"
          privileged = true

registry:
  hpa:
    minReplicas: 1
    maxReplicas: 1

gitlab:
  gitlab-shell:
    minReplicas: 1
    maxReplicas: 1
  gitlab-exporter:
    enabled: false
  webservice:
    minReplicas: 1
    maxReplicas: 1
    # По умолчанию 2 Puma worker'а × ~1.2 ГБ RSS = 2.5 ГБ → OOMKill при лимите 2 ГБ.
    # 1 worker решает проблему при небольшой нагрузке.
    extraEnv:
      GITLAB_PUMA_WORKER_PROCESSES: "1"
    resources:
      limits:
        memory: 3.5Gi
        cpu: 2
      requests:
        memory: 1.5Gi
        cpu: 300m
  sidekiq:
    resources:
      limits:
        memory: 1.5Gi
        cpu: 1
      requests:
        memory: 700Mi
        cpu: 100m
  gitaly:
    resources:
      limits:
        memory: 1Gi
        cpu: 1
      requests:
        memory: 200Mi
        cpu: 50m

postgresql:
  primary:
    resources:
      limits:
        memory: 1Gi
      requests:
        memory: 256Mi

redis:
  master:
    resources:
      limits:
        memory: 256Mi
      requests:
        memory: 64Mi
```

---

## Шаг 6. Установка GitLab

Добавляем helm repo и запускаем install. Процесс занимает **15–20 минут**.

```bash
helm repo add gitlab https://charts.gitlab.io/
helm repo update gitlab

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

> Замените `192.168.3.35` на ваш IP везде в команде.

---

## Шаг 7. Ожидаем готовности

```bash
kubectl -n gitlab get pods -w
```

Порядок готовности подов:

| Pod | Статус | Что делает |
|-----|--------|-----------|
| `gitlab-migrations-*` | Completed | DB-миграции, запускается первым |
| `gitlab-postgresql-*` | Running | База данных |
| `gitlab-redis-master-*` | Running | Cache |
| `gitlab-gitaly-*` | Running | Git-операции |
| `gitlab-webservice-*` | 2/2 Running | Web UI |
| `gitlab-sidekiq-*` | 1/1 Running | Background jobs |
| `gitlab-gitlab-runner-*` | 1/1 Running | CI Runner |

Когда `gitlab-webservice-*` показывает `2/2 Running` — GitLab готов.

Для разовой проверки (без `-w`):

```bash
kubectl -n gitlab get pods | grep -v Completed
```

---

## Шаг 8. Доступ

```
http://gitlab.192.168.3.35.nip.io   # ← ваш IP
```

Получаем пароль root:

```bash
kubectl -n gitlab get secret gitlab-gitlab-initial-root-password \
  -o jsonpath='{.data.password}' | base64 -d && echo
```

Логин: `root` / `<пароль из команды выше>`

**Expect:** открывается страница входа GitLab, после логина виден список проектов.

> Эта установка работает по HTTP без TLS. Container Registry и OCI push из CI (build/push-лабы, full-cycle) на ней не заработают без доверенного HTTPS: для них используйте стенд Servercore или gitlab.com.

---

## Шаг 9. Проверка runner'а

В GitLab → **Admin Area → CI/CD → Runners** должен появиться зарегистрированный runner со статусом **online**.

Если runner не появился через 2–3 минуты после того, как webservice запущен:

```bash
kubectl -n gitlab logs -l app=gitlab-runner --tail=30
```

---

## Известные проблемы

| Симптом | Причина | Решение |
|---------|---------|---------|
| `helm install` падает с "certmanager email required" | Template certmanager валидирует email даже когда он отключён | Добавить `certmanager-issuer.email: admin@example.com` (уже в values выше) |
| Chart v10+ падает с "external Redis/PostgreSQL required" | В v10.0.0 убрали bundled subcharts | Пинить `--version 9.11.7` |
| Порт 80 не биндится | Podman rootless не может < 1024 | `podman machine set --rootful kind` |
| Runner зависает на "Pulling image" для helper | На arm64 runner пытается скачать x86_64 helper | Добавить `helper_image = "...arm64-..."` в runner config (уже в values выше) |
| Webservice падает с OOMKill (exit 137) | 2 Puma worker'а × ~1.2 ГБ = 2.5 ГБ > лимит | `GITLAB_PUMA_WORKER_PROCESSES: "1"` + `memory limit: 3.5Gi` (уже в values выше) |
| Pipeline падает: "no image specified" | Свой `runners.config` полностью заменяет конфиг из chart, вместе с default `image` | Задать `image = "alpine:3.22"` в `[runners.kubernetes]` (уже в values выше) или указывать `image:` в каждом job |
| pip install падает с SSL error | Корпоративный SSL inspection подменяет сертификаты | Не использовать `pip3 install` в компонентах — используйте stdlib или собственный Docker-образ |

---

## Очистка

```bash
kind delete cluster --name kind-dtu
podman machine stop kind
```

Полное удаление VM:

```bash
podman machine rm kind
```
