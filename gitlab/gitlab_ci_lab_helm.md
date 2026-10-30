# Пример GitLab CI/CD пайплайна для сборки, тестирования и отправки Helm чарта в GitLab Package Repository

> **Справочная лаба.** Образ `alpine/helm:3.13.3` — Helm 3, примеры рассчитаны на него; флаги Helm 4 здесь не используются. Для рабочего пайплайна с Helm 4 и деплоем см. [helm_gitlab_cicd_lab.md](../helm/helm_gitlab_cicd_lab.md).
>
> **GitLab стенда** работает по HTTPS с доверенным сертификатом, git только по HTTPS. Скрипт использует `$CI_API_V4_URL` (схема и хост уже правильные), поэтому `curl` работает без изменений и флаги `--plain-http` или пропуск проверки TLS не нужны.
GitLab CI/CD пайплайн автоматизирует процесс сборки, тестирования и отправки Helm чарта в GitLab Package Repository. Используя команду `helm lint` на этапе тестирования, вы можете убедиться, что ваш Helm чарт соответствует стандартам качества перед его отправкой в репозиторий

## Инструкция по созданию GitLab репозитория
Создание репозитория в GitLab — это первый шаг для начала работы с системой контроля версий и автоматизации CI/CD процессов. Следуйте этой инструкции, чтобы создать новый репозиторий в GitLab.

## 1. Регистрация и вход в GitLab
Откройте GitLab стенда: `https://<gitlab-host>/` (адрес указан в материалах курса).
Войдите под выданной вам учётной записью.

## 2. Создание нового проекта
После входа в систему нажмите на кнопку "New project" на главной странице или в меню навигации.
Выберите "Create blank project" для создания нового пустого проекта.

## 3. Настройка проекта
- `Project name`: Введите имя вашего проекта. Например, my-helm-chart.
- `Project slug`: Это автоматически сгенерированное поле на основе имени проекта. Оно будет использоваться в URL вашего репозитория.
- `Project description`: (Необязательно) Введите описание вашего проекта.
- `Visibility level`: Выберите уровень видимости проекта:
    - `Private`: Проект виден только вам и пользователям, которым вы предоставите доступ.
    - `Internal`: Проект виден всем зарегистрированным пользователям GitLab.
    - `Public`: Проект виден всем, включая незарегистрированных пользователей.

Нажмите кнопку `Create project` для создания нового проекта.
После создания проекта вы будете перенаправлены на страницу репозитория. Вам нужно клонировать репозиторий на локальную машину для дальнейшей работы.

На странице репозитория нажмите на кнопку `Clone` и скопируйте URL **HTTPS** (SSH на стенде закрыт; вместо пароля используйте personal access token: Edit profile > Access tokens, scope `write_repository`).
Откройте терминал на вашей локальной машине и выполните команду для клонирования репозитория:
```bash
git clone <URL вашего репозитория>
```
Например:
```
git clone https://<gitlab-host>/your-username/my-helm-chart.git
```
## Добавление файлов в репозиторий
Перейдите в каталог вашего репозитория:
```bash
cd my-helm-chart
```
Добавьте файлы вашего проекта в репозиторий. Например, создайте файл README.md:
```bash
echo "# My Helm Chart" > README.md
```

###  Установите Helm
Если у вас еще не установлен Helm, установите его, следуя инструкциям на официальном сайте Helm. https://helm.sh/docs/intro/install/

### Создайте новый Helm Chart
Используйте команду `helm create` для создания нового Helm Chart:
Эта команда создаст структуру каталогов и файлов для вашего Helm Chart.
```bash
helm create demo-chart
```
Эта команда создаст структуру каталогов и файлов для вашего Helm Chart.

### Структура Helm Chart
После выполнения команды helm create, у вас будет следующая структура каталогов:

```
demo-chart/
├── Chart.yaml
├── values.yaml
├── charts/
├── templates/
│   ├── deployment.yaml
│   ├── _helpers.tpl
│   ├── hpa.yaml
│   ├── ingress.yaml
│   ├── NOTES.txt
│   ├── service.yaml
│   └── serviceaccount.yaml
└── .helmignore

```

### Внесите изменения в Helm Chart

Файл `Chart.yaml` содержит метаданные о вашем Helm Chart. Добавьте в чарт поле `maintainers` :
```yaml
maintainers: # (optional)
  - name: The maintainers name (required for each maintainer)
    email: The maintainers email (optional for each maintainer)
    url: A URL for the maintainer (optional for each maintainer)
``` 
измените версию вашего чарта:
```yaml
version: 0.1.0
```

Пример содержимого:

```yaml
apiVersion: v2
name: demo-chart
description: A Helm chart for Kubernetes

# A chart can be either an 'application' or a 'library' chart.
type: application

# This is the chart version. This version number should be incremented each time you make changes to the chart.
version: 0.1.0

# This is the version number of the application being deployed. This version number should be incremented each time you make changes to the application.
appVersion: 1.0.0
maintainers: # (optional)
  - name: Your Name
    email: your_email@example.com
```

Теперь у вас есть пример Helm Chart, который можно использовать для развертывания простого приложения в Kubernetes. Вы можете настроить файлы `Chart.yaml`, `values.yaml` и шаблоны в каталоге `templates/` в соответствии с вашими требованиями. Этот Helm Chart можно использовать в вашем GitLab CI/CD пайплайне для автоматизации сборки, тестирования и отправки в GitLab Package Repository.

Закоммитьте и отправьте `helm chart` в репозиторий:
```bash
git add . 
git commit -m "Add Helm Chart"
git push origin main
```

# Настройка GitLab CI/CD
Создайте файл `.gitlab-ci.yml` в корне вашего репозитория и добавьте в него конфигурацию пайплайна. Например:
**Файл .gitlab-ci.yml:**
```yaml
stages:
  - build
  - test
  - deploy

variables:
  HELM_PACKAGE_NAME: "demo-chart"
  HELM_PACKAGE_VERSION: "0.1.0"   # должна совпадать с version: в demo-chart/Chart.yaml

# Образ alpine/helm запускает helm как entrypoint, поэтому для shell-скриптов entrypoint сбрасываем
image:
  name: alpine/helm:3.13.3
  entrypoint: [""]

default:
  tags: [lab-1, kubernetes]   # свои теги смотрите в Settings > CI/CD > Runners

build:
  stage: build
  script:
    - echo "Packaging Helm chart..."
    - helm package ./demo-chart --version $HELM_PACKAGE_VERSION
  artifacts:
    paths:
      - "*.tgz"

test:
  stage: test
  script:
    - echo "Linting Helm chart..."
    - helm lint ./demo-chart
  dependencies:
    - build

deploy:
  stage: deploy
  before_script:
    - apk add --no-cache curl
  script:
    - echo "Uploading Helm chart to GitLab Package Repository..."
    - 'curl --fail-with-body --request POST
     --form "chart=@$HELM_PACKAGE_NAME-$HELM_PACKAGE_VERSION.tgz"
     --user gitlab-ci-token:$CI_JOB_TOKEN
     "${CI_API_V4_URL}/projects/${CI_PROJECT_ID}/packages/helm/api/stable/charts"'

  dependencies:
    - build
  rules:
    - if: '$CI_COMMIT_BRANCH == "main"'

```

## Описание:
`stages`: Определяет этапы пайплайна: build, test, deploy.
`variables`: Определяет имя пакета Helm и версию. Версия должна совпадать с `version:` в `Chart.yaml`, иначе имя `.tgz` не совпадёт и загрузка упадёт. `--app-version` не передаётся: `appVersion` берётся из `Chart.yaml`.
`image`: `alpine/helm` с `entrypoint: [""]`, иначе скрипты `script:` не запустятся; `curl` для загрузки ставится в `deploy` через `apk`. `tags`: теги раннера стенда.
`build`:
    **stage**: Указывает, что это этап сборки.
    **script**: Выполняет команду helm package для упаковки Helm чарта.
    **artifacts**: Сохраняет артефакты сборки (упакованный Helm чарт) для последующих этапов.
`test`:
    **stage**: Указывает, что это этап тестирования.
    **script**: Выполняет команду helm lint для проверки Helm чарта.
`deploy`:
    **stage**: Указывает, что это этап развертывания.
    **script**: Выполняет команду curl для загрузки упакованного Helm чарта в GitLab Package Repository.
    **rules**: Указывает, что этот этап выполняется только на ветке main.

Закоммитьте и отправьте файл .gitlab-ci.yml в репозиторий:
```bash
git add .gitlab-ci.yml
git commit -m "Add GitLab CI/CD pipeline configuration"
git push origin main
```
## Проверка результатов
### Шаги для проверки выполнения вашего GitLab CI/CD пайплайна

После настройки вашего GitLab CI/CD пайплайна важно убедиться, что он выполняется корректно. Вот шаги для проверки выполнения вашего GitLab CI/CD пайплайна:

#### 1. Закоммитьте и отправьте ваши изменения

Убедитесь, что вы закоммитили и отправили файл `.gitlab-ci.yml` и любые другие необходимые файлы в ваш GitLab репозиторий:

```bash
git add .gitlab-ci.yml
git commit -m "Add GitLab CI/CD config"
git push origin main
```

#### 2. Перейдите в ваш проект на GitLab

1. Откройте ваш веб-браузер и перейдите на ваш GitLab.
2. Перейдите в ваш проект, выбрав его из списка проектов или используя строку поиска.

#### 3. Доступ к странице CI/CD пайплайнов

1. В левом боковом меню нажмите на **Build**, чтобы развернуть меню.
2. Нажмите на **Pipelines** (Пайплайны), чтобы просмотреть список пайплайнов для вашего проекта.

#### 4. Просмотр статуса пайплайна

На странице пайплайнов вы увидите список запущенных пайплайнов. Каждая запись пайплайна будет показывать следующую информацию:
- **Pipeline ID**: Уникальный идентификатор пайплайна.
- **Status**: Текущий статус пайплайна (например, выполняется, успешно, неудачно).
- **Stages**: Этапы, определенные в вашем файле `.gitlab-ci.yml` (например, build, test, deploy).
- **Duration**: Время, затраченное на выполнение пайплайна.
- **Trigger**: Событие, которое запустило пайплайн (например, push, merge request).

#### 5. Просмотр деталей пайплайна

1. Нажмите на ID пайплайна или на иконку статуса, чтобы просмотреть подробную информацию о выполнении пайплайна.
2. Вы увидите визуальное представление этапов и заданий пайплайна. Каждое задание будет иметь свой статус (например, выполняется, успешно, неудачно).

#### 6. Просмотр логов заданий

1. Нажмите на конкретное задание, чтобы просмотреть его детальные логи.
2. Логи задания покажут вывод команд, выполненных во время задания. Это полезно для отладки и понимания того, что произошло во время выполнения задания.

#### 7. Проверка артефактов 
1. Если ваши задания создают артефакты (например, артефакты сборки, отчеты о тестах), вы можете скачать их со страницы деталей задания.
2. Найдите раздел **Artifacts** (Артефакты) на странице деталей задания и нажмите на ссылки, чтобы скачать артефакты.

#### 8. Мониторинг выполнения пайплайна

1. Если ваш пайплайн все еще выполняется, вы можете мониторить его прогресс в реальном времени.
2. Статус каждого этапа и задания будет обновляться по мере их выполнения.

### 9. Проверка загрузки helm чарта в репозиторий проекта
1. В левом боковом меню нажмите на **Deploy**, чтобы развернуть меню.
2. Нажмите на **Package Registry**, чтобы просмотреть список артефактов в хранилище вашего проекта.

**Ожидаемый результат:** в списке есть пакет `demo-chart` версии `0.1.0`.

# Заключение
Теперь у вас есть новый репозиторий в GitLab с настроенным CI/CD пайплайном. Вы можете продолжать добавлять файлы и конфигурации в ваш проект, а также использовать GitLab CI/CD для автоматизации сборки, тестирования и развертывания вашего приложения.


## Чеклист самопроверки

- [ ] Проект создан и склонирован локально.
- [ ] Helm chart создан командой `helm create` и закоммичен.
- [ ] `.gitlab-ci.yml` добавлен с тремя стадиями: build, test, deploy.
- [ ] Pipeline запустился после `git push` и все три стадии зелёные.
- [ ] Helm chart появился в **Deploy > Package Registry** проекта.
- [ ] Deploy job запускается только на ветке `main`.

## Дополнительное задание — деплой в Kubernetes

> На стенде runner непривилегированный и не хранит учётных данных кластера, а kind-кластер находится на вашей VM. Для деплоя из pipeline рекомендуемые варианты: GitLab Agent for Kubernetes ([gitlab_agent_k8s_lab.md](gitlab_agent_k8s_lab.md)) или лаба полного цикла ([gitlab_ci_lab_full_cycle.md](gitlab_ci_lab_full_cycle.md)). Вариант ниже с токеном ServiceAccount в CI/CD-переменных упрощённый: любой участник проекта с доступом к переменным может прочитать токен, поэтому выдавайте права только на namespace `demo` и короткий срок жизни.

Расширьте pipeline: добавьте job `deploy-k8s`, который деплоит chart в кластер через `helm upgrade --install`.

**Переменные для runner'а:**

```
HELM_KUBEAPISERVER — адрес API-сервера кластера, например https://<cluster-api>:6443 (должен быть доступен из pod'а job'а)
HELM_KUBETOKEN     — токен ServiceAccount (Masked)
HELM_KUBECAFILE    — CA-сертификат кластера (тип переменной: File)
```

Helm читает эти переменные окружения сам, передавать их флагами не нужно. `HELM_KUBEASUSER` не задавайте: это имя пользователя для impersonation, и без прав `impersonate` у токена helm получит 403.

**Шаги:**

1. Создайте namespace `demo` (`kubectl create namespace demo`), в нём ServiceAccount с правами на этот namespace, и сгенерируйте токен: `kubectl -n demo create token <sa-name> --duration=24h`.
2. В GitLab добавьте переменные: **Settings > CI/CD > Variables**.
3. Добавьте job в `.gitlab-ci.yml`:

```yaml
deploy-k8s:
  stage: deploy
  script:
    - helm upgrade --install demo-chart ./demo-chart
        --namespace demo
        --set image.repository=nginx
        --set image.tag=1.27
  rules:
    - if: '$CI_COMMIT_BRANCH == "main"'
      when: manual
```

4. Закоммитьте и запушьте, затем нажмите кнопку запуска в UI:

```bash
git add .gitlab-ci.yml
git commit -m "feat: add k8s deploy job"
git push origin main
```

**Expect:** job `deploy-k8s` зелёный, а `kubectl -n demo get pods` показывает pod чарта в статусе `Running` (образ `nginx:1.27` берётся из публичного Docker Hub). Если job падает с таймаутом подключения, API-сервер кластера недоступен из pod'а runner'а: используйте GitLab Agent.
