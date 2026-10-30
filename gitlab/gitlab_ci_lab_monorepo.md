# Lab: GitLab CI/CD pipeline для монорепо

В этой лабораторной мы построим pipeline для монорепо — проекта, в котором живёт несколько независимых приложений. Цель — сделать так, чтобы **каждое приложение пересобиралось только при изменениях в своей директории**, без лишних запусков.

Опираемся на официальную статью GitLab: [Building a GitLab CI/CD pipeline for a monorepo the easy way](https://about.gitlab.com/blog/building-a-gitlab-ci-cd-pipeline-for-a-monorepo-the-easy-way/).

## Что мы построим

| Шаг | Что делаем |
|---|---|
| 1 | Создаём проект-монорепо в GitLab |
| 2 | Заводим директории `java/` и `python/` для двух приложений |
| 3 | (Legacy) Реализуем подход через `extends` со скрытыми job'ами — чтобы прочувствовать боль |
| 4 | (Modern) Переписываем на `include` + `rules:changes` — компактно и без дублирования |
| 5 | Проверяем работу: коммит в `java/` → запускается только Java pipeline |
| 6 | (Бонус) Добавляем третий сервис и боремся с тем, что при первом push в новую ветку всегда запускаются все pipeline'ы |
| 7 | Финальный self-check |

## Prerequisites

- Аккаунт на GitLab стенда (GitLab 19.x; `include` с `rules:changes` поддерживается).
- Установленный `git` локально.
- Runner в проекте. На стенде это ваш runner с тегами `lab-1` или `lab-2` и `kubernetes` (точные теги проверьте в **Settings > CI/CD > Runners**). Если job висит в `pending`, добавьте в `.gitlab-ci.yml` блок `default:` с `tags: [lab-1, kubernetes]` (подставьте свои теги) или включите **Run untagged jobs** у runner'а.
- 30–40 минут.

> **Kubernetes executor стенда:** runner работает без привилегий, Docker daemon в jobs нет, образ по умолчанию — `alpine:3.22`. В примерах ниже `image:` указан у каждого job явно.

---

## Шаг 1. Создаём проект-монорепо

1. В GitLab нажмите **New project → Create blank project**.
2. Имя: `monorepo-ci-lab`.
3. Visibility: `Private` или `Internal` — на ваш выбор.
4. Отметьте **Initialize repository with a README**.
5. Нажмите **Create project**.

Клонируйте проект локально:

```bash
git clone https://<gitlab-host>/<your-namespace>/monorepo-ci-lab.git
cd monorepo-ci-lab
```

> «Монорепо» в этой лабе — синтетический пример. В реальной жизни в одной репе могут жить десятки сервисов на разных языках. Принцип `rules:changes` масштабируется на любое число директорий.

---

## Шаг 2. Заводим директории приложений

Создаём базовую структуру:

```bash
mkdir -p java python
echo "// Java app placeholder" > java/App.java
echo "# Python app placeholder" > python/app.py

# Файлы pipeline-конфигов приложений (пока пустые); корневой .gitlab-ci.yml создадим в шаге 3
touch java/j.gitlab-ci.yml python/py.gitlab-ci.yml
```

Финальная структура:

```
monorepo-ci-lab/
├── .gitlab-ci.yml          # control-plane pipeline (создаём в шаге 3)
├── README.md
├── java/
│   ├── App.java
│   └── j.gitlab-ci.yml     # Java-specific pipeline
└── python/
    ├── app.py
    └── py.gitlab-ci.yml    # Python-specific pipeline
```

Закоммитим baseline:

```bash
git add .
git commit -m "scaffold monorepo structure"
git push origin main
```

---

## Шаг 3. Legacy подход — `extends` со скрытым job

> **Зачем мы это делаем:** сначала реализуем «старый» способ, чтобы увидеть, **почему** новый подход лучше. На этом шаге ваш pipeline будет работать, но окажется громоздким.

### `.gitlab-ci.yml` (control plane)

```yaml
# Если job висит в pending, раскомментируйте блок и сохраняйте его во всех следующих версиях файла:
# default: {tags: [lab-1, kubernetes]}

stages:
  - build
  - test
  - deploy

top-level-job:
  image: alpine:3.22
  stage: build
  script:
    - echo "Hello from monorepo root..."

include:
  - local: '/java/j.gitlab-ci.yml'
  - local: '/python/py.gitlab-ci.yml'
```

### `java/j.gitlab-ci.yml`

```yaml
stages:
  - build
  - test
  - deploy

.java-common:
  rules:
    - changes:
        - 'java/**/*'

java-build-job:
  extends: .java-common
  image: alpine:3.22
  stage: build
  script:
    - echo "Building Java"

java-test-job:
  extends: .java-common
  image: alpine:3.22
  stage: test
  script:
    - echo "Testing Java"
```

### `python/py.gitlab-ci.yml`

```yaml
stages:
  - build
  - test
  - deploy

.python-common:
  rules:
    - changes:
        - 'python/**/*'

python-build-job:
  extends: .python-common
  image: alpine:3.22
  stage: build
  script:
    - echo "Building Python"

python-test-job:
  extends: .python-common
  image: alpine:3.22
  stage: test
  script:
    - echo "Testing Python"
```

Зафиксируем и запушим:

```bash
git add .
git commit -m "legacy: extends-based monorepo pipeline"
git push origin main
```

### Что попробовать

1. В GitLab перейдите **Build → Pipelines** — увидите запущенный pipeline. На этом push **все** jobs зелёные: коммит затрагивает и `java/`, и `python/`, поэтому совпали оба условия `changes`.
2. Создайте ветку **без изменений** и запушьте её — это первый pipeline ветки (гоча #1, см. шаг 6: на новой ветке запускаются все include):

   ```bash
   git checkout -b update-java
   git push origin update-java
   ```

   Затем сделайте изменение только в Java и запушьте второй коммит:

   ```bash
   echo "// Java change" >> java/App.java
   git commit -am "java only"
   git push origin update-java
   ```

3. Откройте pipeline **второго** коммита. Должны выполниться только `top-level-job` и `java-*` jobs. `python-*` будут отсутствовать (не созданы).

### Боли legacy-подхода

Запишите для себя, что вы заметили:

- В каждом job'е нужно прописывать `extends: .<lang>-common` — копипаста на каждой строчке.
- Нельзя добавить **свои** `rules:` в job, потому что они **переопределят** `rules:` из `.java-common` (`extends` объединяет хеши, но массив `rules:` заменяется целиком).
- Hidden job (`.java-common`) живёт **внутри** application-pipeline'а, а не в control plane — control plane не знает, что и почему пропустилось.

Дальше мы это исправим.

---

## Шаг 4. Современный подход — `include` + `rules:changes`

Мы можем повесить `rules:changes` **прямо на `include:`** (на GitLab 19.x это поддерживается). Это убирает всю boilerplate с extends.

Сначала вернитесь на `main` (после шага 3 вы на ветке `update-java`) и только потом правьте файлы:

```bash
git checkout main
git pull
```

### Новый `.gitlab-ci.yml`

```yaml
stages:
  - build
  - test

top-level-job:
  image: alpine:3.22
  stage: build
  script:
    - echo "Hello from monorepo root..."

include:
  - local: '/java/j.gitlab-ci.yml'
    rules:
      - changes:
          - 'java/**/*'
  - local: '/python/py.gitlab-ci.yml'
    rules:
      - changes:
          - 'python/**/*'
```

### Новый `java/j.gitlab-ci.yml`

```yaml
stages:
  - build
  - test
  - deploy

java-build-job:
  image: alpine:3.22
  stage: build
  script:
    - echo "Building Java"

java-test-job:
  image: alpine:3.22
  stage: test
  script:
    - echo "Testing Java"
```

### Новый `python/py.gitlab-ci.yml`

```yaml
stages:
  - build
  - test
  - deploy

python-build-job:
  image: alpine:3.22
  stage: build
  script:
    - echo "Building Python"

python-test-job:
  image: alpine:3.22
  stage: test
  script:
    - echo "Testing Python"
```

Зафиксируем:

```bash
git add .
git commit -m "modern: include with rules:changes"
git push origin main
```

### Что выиграли

- В application-файлах **нет boilerplate** — `extends: .xxx-common` больше не нужен.
- Каждый job свободен в собственных `rules:` (например, `rules: - if: '$CI_COMMIT_BRANCH == "main"'`) — конфликта с include-уровневыми правилами нет.
- Логика «что включать» собрана в одном месте — в control plane `.gitlab-ci.yml`. Любой инженер видит карту монорепо в одном файле.

---

## Шаг 5. Проверяем поведение

### Изменение только Java

```bash
git checkout -b feature/java-only
git push origin feature/java-only          # первый pipeline ветки: всё запускается
echo "// Java incremental change" >> java/App.java
git commit -am "java only change"
git push origin feature/java-only          # второй pipeline: фильтрация работает
```

В Pipelines второго коммита должны выполниться:

- `top-level-job` ✅
- `java-build-job` ✅
- `java-test-job` ✅
- `python-*` — **отсутствуют в pipeline** (не «skipped» — а полностью не созданы)

> Первый push новой ветки (без изменений) запускает все include — это ожидаемо. Фильтрация видна только на втором коммите.

### Изменение только Python

```bash
git checkout main && git pull
git checkout -b feature/python-only
git push origin feature/python-only        # первый pipeline ветки: всё запускается
echo "# Python incremental change" >> python/app.py
git commit -am "python only change"
git push origin feature/python-only        # второй pipeline: фильтрация работает
```

Во втором pipeline запустится только `python-*` (и `top-level-job`).

### Изменение в обеих директориях

```bash
git checkout main && git pull
git checkout -b feature/both
git push origin feature/both               # первый pipeline ветки
echo "// both" >> java/App.java
echo "# both" >> python/app.py
git commit -am "both apps"
git push origin feature/both               # второй pipeline
```

Во втором pipeline запустятся **все** jobs обеих apps — `rules:changes` сработал по обоим условиям.

> Откройте side-by-side три pipeline UI и сравните — это самое наглядное demo conditional includes.

---

## Шаг 6. (Бонус) Гочи и третий сервис

### Гоча #1 — первый push в ветку всегда запускает всё

`rules:changes` для **новой ветки** всегда оценивается как `true` для **всех** включений. Это документированное поведение GitLab.

**Workaround:**

1. Создаёте feature branch **БЕЗ** изменений и сразу пушите её (первый pipeline запустит всё — это ожидаемо).
2. Все последующие коммиты пушите в уже существующую ветку → `rules:changes` сравнивает diff с предыдущим коммитом, и фильтрация работает.
3. Альтернатива — MR-pipeline: он сравнивает изменения с целевой веткой. Но конфигурация этой лабы не рассчитана на MR: у `top-level-job` и include-правил нет условия `merge_request_event`, поэтому создаётся ли MR-pipeline и какие jobs в нём будут, здесь не гарантируется. Для MR-first подхода нужны `workflow:rules` и явное условие `$CI_PIPELINE_SOURCE == "merge_request_event"` в правилах.

### Гоча #2 — `'java/*'` vs `'java/**/*'`

- `'java/*'` — только файлы **в корне** директории `java/`. Изменения в `java/subdir/file.java` НЕ сработают.
- `'java/**/*'` — рекурсивно во вложенных директориях.

Для реальных проектов всегда используйте `**/*` (этот паттерн уже применён в шаге 4 выше):

```yaml
include:
  - local: '/java/j.gitlab-ci.yml'
    rules:
      - changes:
          - 'java/**/*'
```

### Добавляем третий сервис — Go

Простое расширение паттерна. Нужны `go.mod` и функция `main()`, иначе `go build` и `go test` упадут. `go.mod` создаём вручную, локальный Go не нужен:

```bash
mkdir -p go
cat > go/main.go <<'EOF'
package main

import "fmt"

func main() {
	fmt.Println("hello from go")
}
EOF
printf 'module example/go\n\ngo 1.25\n' > go/go.mod
touch go/go.gitlab-ci.yml
```

> Директива `go 1.25` в `go.mod` должна совпадать (или быть ниже) с версией образа `golang:1.25-alpine` из job'ов ниже; при смене образа обновите обе. Проверьте, что тег `golang:1.25-alpine` существует; если нет, возьмите актуальную поддерживаемую версию Go.

`go/go.gitlab-ci.yml`:

```yaml
stages:
  - build
  - test

go-build-job:
  stage: build
  image: golang:1.25-alpine
  script:
    - cd go
    - go build .

go-test-job:
  stage: test
  image: golang:1.25-alpine
  script:
    - cd go
    - go test ./...
```

Обновляем control plane `.gitlab-ci.yml`:

```yaml
include:
  - local: '/java/j.gitlab-ci.yml'
    rules:
      - changes: ['java/**/*']
  - local: '/python/py.gitlab-ci.yml'
    rules:
      - changes: ['python/**/*']
  - local: '/go/go.gitlab-ci.yml'
    rules:
      - changes: ['go/**/*']
```

(Остальное в `.gitlab-ci.yml` — `stages` и `top-level-job` — без изменений.)

Push и проверьте (как в шаге 5: сначала первый push новой ветки, затем коммит с изменением): при изменении только в `go/` стартуют `top-level-job` и `go-*`, Java и Python jobs отсутствуют.

### Гоча #3 — общий код

Что делать, если в монорепо есть **общая** библиотека (например, `shared/`), изменения в которой должны триггерить **все** приложения?

```yaml
include:
  - local: '/java/j.gitlab-ci.yml'
    rules:
      - changes:
          - 'java/**/*'
          - 'shared/**/*'
  - local: '/python/py.gitlab-ci.yml'
    rules:
      - changes:
          - 'python/**/*'
          - 'shared/**/*'
```

Каждый язык-сервис «подписывается» на изменения в общем коде. Изменение `shared/` → стартуют все pipelines.

---

## Шаг 7. Self-check

Поставьте галочки, прежде чем закрыть лабу:

- [ ] Проект-монорепо создан и склонирован локально.
- [ ] Внутри есть директории `java/` и `python/` с собственными `j.gitlab-ci.yml` / `py.gitlab-ci.yml`.
- [ ] Реализовали legacy-подход с `.java-common` и `.python-common` — увидели boilerplate.
- [ ] Переписали на `include` с `rules:changes` — control plane упростился.
- [ ] Push в Java-only ветку запускает только Java jobs.
- [ ] Push в Python-only ветку запускает только Python jobs.
- [ ] Понимаете гочу с первым push в новую ветку (все pipeline'ы запустятся).
- [ ] Понимаете разницу между `java/*` и `java/**/*`.
- [ ] (бонус) Добавили третий сервис и проверили его independence.
- [ ] (бонус) Понимаете, как обрабатывать общий код через дублирование путей в `changes:`.

---

## Что дальше

Эта лаба покрывает базовый случай. В реальном монорепо есть ещё несколько направлений для развития:

- **`when: always` для общих guard-jobs** — например, lint всего YAML или security scan на каждом push.
- **Cross-service tests** — отдельный pipeline-файл `e2e.gitlab-ci.yml`, который включается при изменениях в **любой** из директорий.
- **Child pipelines вместо include** — для очень больших монорепо лучше использовать `trigger: include:` с независимым lifecycle.
- **CI/CD Components вместо local include** — переиспользуем монорепо-логику в нескольких проектах через [CI/CD Catalog](https://docs.gitlab.com/ee/ci/components/).
- **Pipeline scheduling per service** — отдельные schedule'ы для nightly Java build / weekly Python build.

### Полезные ссылки

- [Conditionally include CI/CD configuration](https://docs.gitlab.com/ee/ci/yaml/includes.html#use-rules-with-include) — официальные docs.
- [`rules:changes` reference](https://docs.gitlab.com/ee/ci/yaml/index.html#ruleschanges) — нюансы и edge-cases.
- [5 tips for managing monorepos in GitLab](https://about.gitlab.com/blog/tips-for-managing-monorepos-in-gitlab/) — расширенные советы.
- [Original article](https://about.gitlab.com/blog/building-a-gitlab-ci-cd-pipeline-for-a-monorepo-the-easy-way/) — источник этой лабы.
