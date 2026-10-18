# Lab: управление запуском jobs через `rules`

В этой лабораторной мы практикуем все четыре механизма `rules` в GitLab CI: `if`, `changes`, `exists`, и `$CI_PIPELINE_SOURCE`. Для каждого создаём условие, делаем push и смотрим, какие jobs появились в pipeline, а какие нет.

**Главный вывод:** `rules` оцениваются сверху вниз — первое совпавшее условие определяет поведение job'а.

## Что мы построим

| Шаг | Что изучаем | Условие |
|---|---|---|
| 1 | Базовые branch rules | `if: $CI_COMMIT_BRANCH == "main"` |
| 2 | rules:changes | Запуск только при изменениях в `src/` |
| 3 | Pipeline source | Поведение при push vs MR vs schedule |
| 4 | rules:exists | Job активируется по наличию файла |
| 5 | `when:` + цепочка правил | manual, never, on_success |
| 6 | workflow:rules | Контроль на уровне всего pipeline |

## Prerequisites

- Аккаунт в GitLab (gitlab.com или self-hosted; минимальная версия с поддержкой `compare_to` в `rules:changes` — **проверь** в документации вашей версии).
- Установленный `git` локально.
- Включённый runner в проекте.
- 30–40 минут.

> **Примечание:** без `compare_to` `rules:changes` для push-pipeline сравнивает текущий коммит с предыдущим, а для **первого push новой ветки** всегда возвращает `true` — все jobs с `changes` запустятся. Поэтому в шаге 2 мы сравниваем с `main` через `compare_to: refs/heads/main`. Тогда на самой ветке `main` сравнение пустое, и `build`/`test` там не запускаются — это ожидаемо.

---

## Шаг 0. Создаём проект со структурой

```bash
# Создайте проект в GitLab: New project → Create blank project
# Имя: rules-lab, Initialize with README
git clone https://gitlab.example.com/<your-namespace>/rules-lab.git
cd rules-lab
```

Создайте структуру файлов:

```bash
mkdir -p src tests
echo 'print("app")' > src/app.py
echo 'print("test")' > tests/test_app.py
echo 'FROM alpine:3.20' > Dockerfile
```

Создайте базовый `.gitlab-ci.yml`:

```yaml
image: alpine:latest

stages:
  - build
  - test
  - deploy

build:
  stage: build
  script:
    - echo "Building..."
    - ls src/

test:
  stage: test
  script:
    - echo "Testing..."
    - ls tests/

deploy:
  stage: deploy
  script:
    - echo "Deploying..."
```

```bash
git add .
git commit -m "scaffold: project structure + basic pipeline"
git push origin main
```

**Что увидите:** все три job'а запустились — нет никаких `rules`, значит job создаётся всегда.

---

## Шаг 1 — rules:if — branch conditions

Добавьте `rules` к job'у `deploy`, чтобы он запускался **только на `main`**:

```yaml
deploy:
  stage: deploy
  script:
    - echo "Deploying..."
  rules:
    - if: '$CI_COMMIT_BRANCH == "main"'
```

```bash
git add .gitlab-ci.yml
git commit -m "rules step 1: deploy only on main"
git push origin main
```

**Проверьте:** job `deploy` появился — мы на ветке `main`.

Теперь создайте feature-ветку и запушьте туда:

```bash
git checkout -b feature/rules-test
echo "# change" >> src/app.py
git commit -am "test: push to feature branch"
git push origin feature/rules-test
```

**Что должно произойти:** `build` и `test` запустились, `deploy` **отсутствует** в pipeline (не создан).

---

## Шаг 2 — rules:changes — file-based triggers

Обновите `.gitlab-ci.yml` — `build` запускается только при изменениях в `src/`, `test` — только в `tests/`:

```yaml
image: alpine:latest

stages:
  - build
  - test
  - deploy

build:
  stage: build
  script:
    - echo "Building..."
  rules:
    - if: '$CI_COMMIT_BRANCH'
      changes:
        paths:
          - src/**/*
        compare_to: refs/heads/main

test:
  stage: test
  script:
    - echo "Testing..."
  rules:
    - if: '$CI_COMMIT_BRANCH'
      changes:
        paths:
          - tests/**/*
        compare_to: refs/heads/main

deploy:
  stage: deploy
  script:
    - echo "Deploying..."
  rules:
    - if: '$CI_COMMIT_BRANCH == "main"'
```

```bash
git checkout main
git add .gitlab-ci.yml
git commit -m "rules step 2: changes-based triggers"
git push origin main
```

> На `main` после этого коммита `build` и `test` не появятся: сравнение `main` с `main` пустое. Это ожидаемо.

**Тест A — изменение только в `src/`:**

```bash
git checkout -b feature/src-change
echo "# src change" >> src/app.py
git commit -am "change: modify src only"
git push origin feature/src-change
```

Ожидаемый результат: только `build` запустился. `test` и `deploy` отсутствуют.

**Тест B — изменение только в `tests/`:**

```bash
git checkout main && git pull
git checkout -b feature/test-change
echo "# test change" >> tests/test_app.py
git commit -am "change: modify tests only"
git push origin feature/test-change
```

Ожидаемый результат: только `test` запустился. `build` и `deploy` отсутствуют.

**Тест C — изменение в обоих:**

```bash
git checkout main && git pull
git checkout -b feature/both-change
echo "# both" >> src/app.py
echo "# both" >> tests/test_app.py
git commit -am "change: modify src and tests"
git push origin feature/both-change
```

Ожидаемый результат: `build` и `test` запустились. `deploy` отсутствует (не main).

> **Гоча `src/**/*` vs `src/*`:**
> - `src/*` — только файлы прямо в `src/`, поддиректории игнорируются.
> - `src/**/*` — рекурсивно во всех поддиректориях.
> Используйте `**/*` для реальных проектов.

---

## Шаг 3 — $CI_PIPELINE_SOURCE — источник запуска

Добавьте job `integration-test`, который запускается **только из MR**:

```yaml
integration-test:
  stage: test
  script:
    - echo "Running integration tests for MR..."
  rules:
    - if: '$CI_PIPELINE_SOURCE == "merge_request_event"'
```

И job `nightly-scan`, который запускается **только по расписанию**:

```yaml
nightly-scan:
  stage: test
  script:
    - echo "Running nightly security scan..."
  rules:
    - if: '$CI_PIPELINE_SOURCE == "schedule"'
```

Добавьте оба в `.gitlab-ci.yml` и запушьте:

```bash
git checkout main
git add .gitlab-ci.yml
git commit -m "rules step 3: pipeline source conditions"
git push origin main
```

**Тест A — обычный push:**

`integration-test` и `nightly-scan` отсутствуют (source = `push`).

**Тест B — создайте MR:**

> MR-pipeline использует `.gitlab-ci.yml` **ветки-источника**. Ветки из шага 2 созданы до коммита шага 3 и не содержат job `integration-test`, поэтому MR из них его не покажет. Создайте новую ветку от актуального `main` (уже с коммитом шага 3):
>
> ```bash
> git checkout main && git pull
> git checkout -b feature/mr-test
> echo "# mr test" >> src/app.py
> git commit -am "change: mr test"
> git push origin feature/mr-test
> ```

1. Откройте **Merge requests → New merge request**.
2. Source branch: `feature/mr-test`. Target: `main`.
3. После создания MR — откройте **Pipelines** в MR.

`integration-test` должен появиться (source = `merge_request_event`).

**Тест C — schedule (опционально):**

1. **CI/CD → Schedules → New schedule**.
2. Description: `nightly`, Cron: `0 2 * * *`.
3. Нажмите **Run pipeline manually** (кнопка рядом со schedule).

`nightly-scan` запустится, остальные — нет.

**Все возможные значения `$CI_PIPELINE_SOURCE`:**

| Значение | Когда |
|---|---|
| `push` | Обычный `git push` |
| `merge_request_event` | Pipeline на MR |
| `schedule` | По расписанию |
| `trigger` | API trigger / `curl` |
| `web` | Run pipeline в UI |
| `parent_pipeline` | Из parent через `trigger:` |
| `pipeline` | Multi-project trigger |

---

## Шаг 4 — rules:exists — наличие файла

Добавьте job `docker-build`, который запускается **только если в репозитории есть `Dockerfile`**:

```yaml
docker-build:
  stage: build
  script:
    - echo "Building Docker image..."
  rules:
    - exists:
        - Dockerfile
```

```bash
git add .gitlab-ci.yml
git commit -m "rules step 4: exists condition"
git push origin main
```

**Проверьте:** `docker-build` запустился — `Dockerfile` существует.

**Тест — удалите Dockerfile:**

```bash
git rm Dockerfile
git commit -m "remove Dockerfile"
git push origin main
```

`docker-build` исчезнет из pipeline.

Верните обратно:

```bash
echo 'FROM alpine:3.20' > Dockerfile
git add Dockerfile
git commit -m "restore Dockerfile"
git push origin main
```

> **Применение:** `exists` удобен в shared templates через `include:` — job активируется только в проектах, где нужный файл есть. Подключил один template во все проекты → каждый проект сам определяет, какие jobs нужны.

---

## Шаг 5 — `when:` — уточняем поведение

Обновите `deploy` с полной цепочкой правил:

```yaml
deploy:
  stage: deploy
  script:
    - echo "Deploying to production..."
  rules:
    - if: '$CI_COMMIT_TAG =~ /^v\d+\.\d+\.\d+$/'
      when: on_success
    - if: '$CI_COMMIT_BRANCH == "main"'
      when: manual
    - when: never
```

```bash
git add .gitlab-ci.yml
git commit -m "rules step 5: when chain (auto/manual/never)"
git push origin main
```

**Тест A — push в main:** `deploy` появился с иконкой ▶ (manual) — нужно нажать.

**Тест B — push в feature-ветку:** `deploy` отсутствует (`when: never`).

**Тест C — semantic version tag:**

```bash
git tag v1.0.0
git push origin v1.0.0
```

`deploy` появился и **запустился автоматически** (`on_success`).

**Логика `when:`:**

| Значение | Поведение |
|---|---|
| `on_success` (default) | Job запускается если предыдущие jobs успешны |
| `manual` | Job появляется, но требует нажатия кнопки |
| `never` | Job не создаётся |
| `always` | Запускается независимо от статуса предыдущих |
| `on_failure` | Запускается только если предыдущий job упал |

---

## Шаг 6 — workflow:rules — контроль на уровне pipeline

`rules` внутри job управляют отдельным job'ом. `workflow:rules` управляет тем, **создаётся ли pipeline вообще**.

Добавьте в начало `.gitlab-ci.yml` перед `stages:`:

```yaml
workflow:
  rules:
    - if: '$CI_PIPELINE_SOURCE == "merge_request_event"'
    - if: '$CI_COMMIT_BRANCH == "main"'
    - if: '$CI_COMMIT_TAG'
    - when: never
```

```bash
git add .gitlab-ci.yml
git commit -m "rules step 6: workflow rules"
git push origin main
```

**Результат:** pipeline теперь запускается **только** для MR, push в `main`, или тега. Push в любую другую ветку — pipeline не создаётся совсем (не `skipped`, а вообще отсутствует).

Это стандартный **MR-first** паттерн: разработчик не засоряет CI случайными push'ами в feature-ветки без открытого MR.

**Тест:** создайте новую ветку без MR и запушьте:

```bash
git checkout -b feature/no-mr
echo "test" >> src/app.py
git commit -am "test: push without MR"
git push origin feature/no-mr
```

Перейдите **CI/CD → Pipelines** — pipeline для этой ветки не создан.

---

## Итоговый `.gitlab-ci.yml`

```yaml
workflow:
  rules:
    - if: '$CI_PIPELINE_SOURCE == "merge_request_event"'
    - if: '$CI_COMMIT_BRANCH == "main"'
    - if: '$CI_COMMIT_TAG'
    - when: never

image: alpine:latest

stages:
  - build
  - test
  - deploy

build:
  stage: build
  script:
    - echo "Building..."
  rules:
    - if: '$CI_COMMIT_BRANCH'
      changes:
        paths:
          - src/**/*
        compare_to: refs/heads/main

docker-build:
  stage: build
  script:
    - echo "Building Docker image..."
  rules:
    - exists:
        - Dockerfile

test:
  stage: test
  script:
    - echo "Testing..."
  rules:
    - if: '$CI_COMMIT_BRANCH'
      changes:
        paths:
          - tests/**/*
        compare_to: refs/heads/main

integration-test:
  stage: test
  script:
    - echo "Running integration tests for MR..."
  rules:
    - if: '$CI_PIPELINE_SOURCE == "merge_request_event"'

nightly-scan:
  stage: test
  script:
    - echo "Running nightly security scan..."
  rules:
    - if: '$CI_PIPELINE_SOURCE == "schedule"'

deploy:
  stage: deploy
  script:
    - echo "Deploying to production..."
  rules:
    - if: '$CI_COMMIT_TAG =~ /^v\d+\.\d+\.\d+$/'
      when: on_success
    - if: '$CI_COMMIT_BRANCH == "main"'
      when: manual
    - when: never
```

```bash
git add .gitlab-ci.yml
git commit -m "rules: final combined pipeline"
git push origin main
```

---

## Self-check

- [ ] Проект создан, базовая структура `src/`, `tests/`, `Dockerfile` закоммичена.
- [ ] Шаг 1 — `deploy` отсутствует на feature-ветке, появляется на `main`.
- [ ] Шаг 2 — изменение в `src/` триггерит только `build`; в `tests/` — только `test`.
- [ ] Шаг 3 — `integration-test` появляется только в MR-pipeline.
- [ ] Шаг 4 — `docker-build` исчез после удаления `Dockerfile`, вернулся после восстановления.
- [ ] Шаг 5 — `deploy` появляется как manual на `main`; запускается автоматически при теге `v*`.
- [ ] Шаг 6 — `workflow:rules` блокирует создание pipeline для ветки без MR.
- [ ] Понимаете порядок оценки: условия проверяются сверху вниз, первое совпавшее срабатывает.
- [ ] Понимаете разницу между `rules` в job и `workflow:rules`.

---

## Что дальше

- **`rules:changes` + `rules:if` вместе** — job запускается при изменениях В `src/` И только на `main` (логическое AND внутри одного rule-item).
- **`rules:variables`** — задаёт переменную когда rule совпал (`variables: DEPLOY_ENV: staging`).
- **`inherit:variables`** — управление тем, какие переменные наследует job.
- **`!reference`** — DRY: выносим повторяющиеся правила в hidden job и ссылаемся через `!reference [.template, rules]`.

### Полезные ссылки

- [`rules:` keyword reference](https://docs.gitlab.com/ee/ci/yaml/index.html#rules)
- [`workflow:rules`](https://docs.gitlab.com/ee/ci/yaml/index.html#workflowrules)
- [Predefined CI/CD variables](https://docs.gitlab.com/ee/ci/variables/predefined_variables.html)
- [Common `if` clauses](https://docs.gitlab.com/ee/ci/jobs/job_rules.html#common-if-clauses-for-rules)
