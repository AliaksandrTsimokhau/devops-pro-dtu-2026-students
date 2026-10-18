# Lab: создание первого GitLab CI/CD Component

В этой лабораторной мы создадим свой первый **CI/CD Component** — переиспользуемый блок pipeline-конфигурации, который любая команда в организации может подключить одной строкой через `include: - component:`.

Опираемся на официальный туториал GitLab: [https://about.gitlab.com/blog/tutorial-how-to-set-up-your-first-gitlab-ci-cd-component/](https://about.gitlab.com/blog/tutorial-how-to-set-up-your-first-gitlab-ci-cd-component/).

## Что мы построим

| Шаг | Что делаем |
|---|---|
| 1 | Создаём проект-библиотеку компонентов в GitLab |
| 2 | Пишем Python-скрипт, который компонент будет запускать |
| 3 | Описываем сам компонент в `templates/template.yml` |
| 4 | Тегируем версию компонента (`v1.0.0`) |
| 5 | Создаём второй проект — application — и подключаем компонент в его `.gitlab-ci.yml` |
| 6 | Проверяем результат: pipeline в application-проекте использует компонент |
| 7 | (опционально) Расширяем компонент — обращение к GitLab API через personal access token |

## Prerequisites

- Аккаунт в GitLab (gitlab.com или self-hosted).
- Базовое знание Python (по сути нужен только `argparse`).
- Работающее представление о `.gitlab-ci.yml`, stages, jobs, include.
- Установленный `git` локально.
- 30–45 минут.

> Перед началом проверьте, что GitLab Runner доступен для вашего проекта: Settings → CI/CD → Runners → должен быть хотя бы один enabled runner. На gitlab.com — Shared runners включены по умолчанию.

---

## Шаг 1. Создаём проект-библиотеку компонентов

1. В GitLab нажмите **New project → Create blank project**.
2. Имя: `gitlab-python-cicd-component`.
3. Visibility: `Private` (компонент будет доступен консьюмерам из той же группы/инстанса; для Public и публикации в Catalog нужна отдельная настройка, см. Шаг 6).
4. Отметьте **Initialize repository with a README**.
5. Нажмите **Create project**.

Клонируйте репозиторий локально:

```bash
git clone https://gitlab.example.com/<your-namespace>/gitlab-python-cicd-component.git
cd gitlab-python-cicd-component
```

> Имя репозитория = имя CI/CD Component'а. Старайтесь делать его описательным: `python-lint-component`, `helm-deploy-component`, `trivy-scan-component`.

---

## Шаг 2. Структура проекта

Создайте директории:

```bash
mkdir -p src templates
touch src/script.py src/requirements.txt templates/template.yml .gitlab-ci.yml
```

Финальная структура:

```
gitlab-python-cicd-component/
├── .gitlab-ci.yml          # self-test pipeline компонента
├── README.md
├── src/
│   ├── requirements.txt
│   └── script.py
└── templates/
    └── template.yml        # сам компонент
```

> Конвенция GitLab: файлы в директории `templates/` автоматически распознаются как CI/CD Components. Имя файла без `.yml` становится именем компонента: `templates/template.yml` → component name = `template`.

---

## Шаг 3. Пишем Python-скрипт

Откройте `src/script.py` и вставьте:

```python
import argparse

parser = argparse.ArgumentParser(description='Python CICD Component Boilerplate')
parser.add_argument('python_container_image', type=str, help='python:3.10-slim')
parser.add_argument('stage', type=str, help='Build')
parser.add_argument('persons_name', type=str, help='Noah')
args = parser.parse_args()

python_container_image = args.python_container_image
stage = args.stage
persons_name = args.persons_name

print("You have chosen " + python_container_image + " as the container image")
print("You have chosen " + stage + " as the stage to run this job")
print("Thank you " + persons_name + "! you are SUCCESSFULLY using GitLab CI with a Python script.")
```

Скрипт принимает три аргумента и просто выводит их. В реальном компоненте здесь была бы полезная нагрузка: линтер, сканер, генератор отчёта.

Заполните `src/requirements.txt`:

```text
python-gitlab==4.4.0
```

(Пакет нужен только для локальных экспериментов с GitLab API. В CI-компоненте мы обходимся стандартной библиотекой — см. шаг 9. `argparse` тоже входит в stdlib, устанавливать его не нужно.)

Проверьте локально:

```bash
python3 src/script.py python:3.11-slim build Aliaksandr
```

Ожидаемый вывод:

```
You have chosen python:3.11-slim as the container image
You have chosen build as the stage to run this job
Thank you Aliaksandr! you are SUCCESSFULLY using GitLab CI with a Python script.
```

> Локальная проверка удобна для отладки логики скрипта. В CI-компоненте сам скрипт будет встроен inline в шаблон (см. шаг 4) — это самый простой способ сделать его доступным в checkout потребителя.

---

## Шаг 4. Описываем компонент в `templates/template.yml`

Это сердце CI/CD Component'а. Открываем `templates/template.yml`:

```yaml
spec:
  inputs:
    python_container_image:
      default: python:3.11-slim
      description: "Python container image to use"
    stage:
      default: build
      description: "Stage this job will run in"
    persons_name:
      default: Noah
      description: "Your name"
---
component:
  image: $[[ inputs.python_container_image ]]
  stage: $[[ inputs.stage ]]
  variables:
    PERSONS_NAME: $[[ inputs.persons_name ]]
    PYTHON_IMAGE: $[[ inputs.python_container_image ]]
    STAGE_NAME: $[[ inputs.stage ]]
  script:
    - |
      python3 - << 'PYEOF'
      import os
      python_container_image = os.environ["PYTHON_IMAGE"]
      stage = os.environ["STAGE_NAME"]
      persons_name = os.environ["PERSONS_NAME"]
      print("You have chosen " + python_container_image + " as the container image")
      print("You have chosen " + stage + " as the stage to run this job")
      print("Thank you " + persons_name + "! you are SUCCESSFULLY using GitLab CI with a Python script.")
      PYEOF
```

> **Ключевой архитектурный момент.** Когда потребитель подключает компонент через `include: - component:`, jobs из шаблона выполняются в **git checkout потребителя**, а не компонента. Файл `src/script.py` из репозитория компонента при этом недоступен. Поэтому скрипт встроен в шаблон inline — через heredoc Python. Альтернативы для больших скриптов: собрать Docker-образ со скриптом и указывать его в `image:`, или скачивать скрипт из репозитория компонента по API.

Разбор:

- **`spec:inputs:`** — объявление входных параметров. У каждого может быть `default`, `description`, `type`, `regex`.
- **`---`** — разделитель между spec и телом компонента. Обязателен.
- **`$[[ inputs.X ]]`** — синтаксис подстановки входных параметров на этапе **парсинга** pipeline, до запуска runner'а. Он работает в любом месте шаблона, включая `script:`: GitLab просто подставляет текст. Мы всё же передаём inputs через `variables:` — так значение попадает в shell как env-переменная, и кавычки или спецсимволы во входе не ломают Python-код в heredoc.
- **`$VAR`** — runtime-переменная, разрешается уже в shell runner'а. Это разные миры.
- **`component:`** — обычный GitLab CI job. Имя `component` — стандарт; можно задать любое.

---

## Шаг 5. Self-test pipeline для компонента

Чтобы CI компонента сам себя проверял, добавим базовый `.gitlab-ci.yml` в корень репозитория:

```yaml
stages:
  - test

include:
  - component: $CI_SERVER_FQDN/$CI_PROJECT_PATH/template@$CI_COMMIT_SHA
    inputs:
      python_container_image: python:3.11-slim
      stage: test
      persons_name: Tanuki
```

Здесь компонент включает сам себя из текущего коммита через предопределённые переменные:

- **`$CI_SERVER_FQDN`** — хост GitLab (например, `gitlab.com`).
- **`$CI_PROJECT_PATH`** — `namespace/project-name`.
- **`@$CI_COMMIT_SHA`** — точная версия (текущий commit), не зависит от tag/branch.

Коммитим и пушим:

```bash
git add .
git commit -m "feat: add CI/CD component boilerplate"
git push origin main
```

Откройте **Build → Pipelines** — должен пройти один job `component` в стадии `test` с приветствием для Tanuki.

---

## Шаг 6. Тегируем версию

CI/CD Components версионируются через git tags. Сделаем релиз 1.0.0:

```bash
git tag v1.0.0
git push origin v1.0.0
```

Откройте **Deploy → Releases** или **Code → Tags** — должен появиться `v1.0.0`.

> Используйте semver:
> - `v1.0.0` — стабильный релиз,
> - `v1.0.1` — bugfix,
> - `v1.1.0` — новые входные параметры (обратно совместимо),
> - `v2.0.0` — breaking changes.

(Дополнительно) Опубликуйте в **CI/CD Catalog**: Settings → General → Project visibility → нужны Public visibility, отмеченный чекбокс CI/CD Catalog Resource и подходящий README.

---

## Шаг 7. Application repo — подключаем компонент

Создаём ВТОРОЙ проект — это будет потребитель компонента:

1. **New project → Create blank project**.
2. Имя: `gitlab-python-cicd-component-consumer`.
3. Initialize with README.

Клонируем:

```bash
git clone https://gitlab.example.com/<your-namespace>/gitlab-python-cicd-component-consumer.git
cd gitlab-python-cicd-component-consumer
```

Создаём `.gitlab-ci.yml`:

```yaml
stages:
  - build
  - test
  - deploy

include:
  - component: $CI_SERVER_FQDN/<your-namespace>/gitlab-python-cicd-component/template@v1.0.0
    inputs:
      python_container_image: python:3.11-slim
      stage: test
      persons_name: Aliaksandr
```

Замените `<your-namespace>` на свой namespace.

Коммитим и пушим:

```bash
git add .gitlab-ci.yml
git commit -m "ci: use python cicd component v1.0.0"
git push origin main
```

Откройте **Build → Pipelines** — pipeline должен содержать тот же job `component` (но уже в проекте-потребителе) с приветствием для Aliaksandr.

> Важно: для приватного проекта-компонента потребитель должен иметь доступ на чтение к проекту-источнику. Если его нет, include упадёт с ошибкой доступа. Возможное решение — **Settings → CI/CD → Token Access** в проекте-источнике (**проверь**: в вашей версии GitLab Token Access может работать иначе или быть ограничен для компонентов; проверьте раздел документации «CI/CD components» для вашей версии). Самый простой путь в лабе — добавить проект-потребитель в группу с доступом к компоненту.

---

## Шаг 8. Проверка изменений через bump версии

Сделаем "обновление" компонента и проверим, что версии работают.

В проекте `gitlab-python-cicd-component` изменим финальное сообщение в шаблоне:

```bash
# Меняем текст в template.yml (скрипт встроен в шаблон inline)
sed -i '' 's/SUCCESSFULLY/SUCCESSFULLY! 🚀/' templates/template.yml  # macOS
# sed -i 's/SUCCESSFULLY/SUCCESSFULLY! 🚀/' templates/template.yml    # Linux

git add templates/template.yml
git commit -m "feat: improve success message"
git tag v1.0.1
git push origin main v1.0.1
```

В проекте-потребителе обновите ссылку:

```yaml
include:
  - component: $CI_SERVER_FQDN/<your-namespace>/gitlab-python-cicd-component/template@v1.0.1
    ...
```

Запустите pipeline в проекте-потребителе — в логе появится обновлённое сообщение.

> Это и есть основная ценность Components: один источник правды, обновление через bump версии, прозрачная история изменений.

---

## Шаг 9 (опционально). Расширяем — GitLab API через PAT

Добавим в компонент возможность обращаться к GitLab API, например, чтобы получить список проектов в namespace.

Скрипт выполняется inline из шаблона, поэтому код добавляем в `templates/template.yml`. Используем только стандартную библиотеку (`urllib`) — без `pip install` в job'е. Добавьте второй элемент в `script:` после существующего heredoc:

```yaml
    - |
      python3 - << 'PYEOF'
      import json
      import os
      import urllib.request

      token = os.environ.get("GLPAT")
      if not token:
          print("GLPAT is not set: skipping GitLab API check")
      else:
          req = urllib.request.Request(
              os.environ["CI_API_V4_URL"] + "/projects?membership=true&simple=true",
              headers={"PRIVATE-TOKEN": token},
          )
          try:
              with urllib.request.urlopen(req) as resp:
                  for project in json.load(resp):
                      print(project["path_with_namespace"])
          except Exception as error:
              print("Error:", error)
      PYEOF
```

> Здесь `'PYEOF'` в кавычках: shell ничего не подставляет внутри heredoc, а переменные читаются в Python через `os.environ`. `CI_API_V4_URL` — предопределённая переменная, поэтому код работает и на gitlab.com, и на self-hosted.

Создайте **Personal Access Token**:

1. **User Settings → Access Tokens**.
2. Name: `gitlab-component-pat`.
3. Scopes: `read_api`.
4. Скопируйте сгенерированный токен.

Добавьте в проект-потребитель:

1. **Settings → CI/CD → Variables → Add variable**.
2. Key: `GLPAT`.
3. Value: вставьте токен.
4. Type: **Variable** (или **Masked** для скрытия в логах).
5. Protected: чекбокс снят, чтобы вариант был доступен на любой ветке.

Поднимите версию компонента (`v1.1.0`), обновите ссылку в потребителе на `@v1.1.0`. После запуска pipeline вы увидите список проектов в логе.

> **Security note:** в реальном компоненте никогда не выводите PAT в логи; используйте `Masked` + `Protected` для prod-токенов; рассматривайте OIDC federation вместо long-lived PAT (см. GitLab docs → CI/CD → Cloud OIDC).

---

## Чеклист самопроверки

- [ ] Создал два проекта: компонент и потребитель.
- [ ] Self-test pipeline компонента зелёный.
- [ ] Тег `v1.0.0` создан, виден в Tags.
- [ ] Компонент подключён в проекте-потребителе через `include: - component:`.
- [ ] Pipeline в проекте-потребителе использует компонент и выводит правильное сообщение.
- [ ] Bump версии (`v1.0.1`) — потребитель видит изменения после обновления `@version` в include.
- [ ] (опционально) Компонент обращается к GitLab API через `$GLPAT`.

---

## Что дальше

- **CI/CD Catalog** — опубликуйте компонент в каталоге, чтобы коллеги могли искать его в UI: [docs.gitlab.com/ee/ci/components/#cicd-catalog](https://docs.gitlab.com/ee/ci/components/#cicd-catalog).
- **Несколько компонентов в одном репо** — добавьте `templates/lint.yml`, `templates/build.yml`, `templates/deploy.yml`; каждый подключается отдельно.
- **Input typing** — добавьте `type: string|number|boolean|array` и `regex:` в spec, чтобы валидировать входы (`type` доступен с GitLab 16.5+).
- **Replace your existing templates** — пересмотрите свои существующие `include:project` шаблоны и мигрируйте на компоненты, чтобы получить версионирование и каталог.

## Полезные ссылки

- Туториал-источник: [How to set up your first GitLab CI/CD component](https://about.gitlab.com/blog/tutorial-how-to-set-up-your-first-gitlab-ci-cd-component/)
- CI/CD Components reference: [docs.gitlab.com/ee/ci/components/](https://docs.gitlab.com/ee/ci/components/)
- Demo repository: [gitlab.com/demos/templates/gitlab-python-cicd-component](https://gitlab.com/demos/templates/gitlab-python-cicd-component/)
- CI/CD Catalog: [gitlab.com/explore/catalog](https://gitlab.com/explore/catalog)
