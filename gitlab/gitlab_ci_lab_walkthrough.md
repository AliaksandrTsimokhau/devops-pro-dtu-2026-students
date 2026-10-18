# Lab: собираем GitLab CI pipeline по шагам

В этой лабораторной мы последовательно строим pipeline — от одного job'а до полноценного DAG с параллельностью и out-of-order выполнением. Каждый шаг — один коммит, один запуск pipeline. В конце вы увидите, как модель выполнения GitLab CI эволюционирует от простого к сложному.

**Главный вывод:** stages создают барьеры синхронизации — `needs` их убирает.

Опираемся на: [Basics of GitLab CI — updated](https://about.gitlab.com/blog/basics-of-gitlab-ci-updated/).

## Что мы построим

| Шаг | Что добавляем | Зачем |
|---|---|---|
| 1 | Один `test` job | Самый минимальный CI |
| 2 | `artifacts` | Передаём файл между jobs |
| 3 | `stages` | Sequential: test → package |
| 4 | `compile` stage | Compile once, use many |
| 5 | Параллельные jobs | pack-gz + pack-iso одновременно |
| 6 | `needs` (DAG) | Снимаем stage-барьер |

## Prerequisites

- Аккаунт в GitLab (gitlab.com или self-hosted).
- Установленный `git` локально.
- Включённый runner в проекте (Shared runners на gitlab.com — из коробки).
- 30–45 минут.

---

## Шаг 0. Создаём проект

1. **New project → Create blank project**.
2. Имя: `gitlab-ci-walkthrough`.
3. Visibility: Private.
4. Отметьте **Initialize repository with a README**.
5. Нажмите **Create project**.

Клонируйте локально:

```bash
git clone https://gitlab.example.com/<your-namespace>/gitlab-ci-walkthrough.git
cd gitlab-ci-walkthrough
```

---

## Шаг 1 — первый test job

Создайте `.gitlab-ci.yml`:

```yaml
test:
  before_script:
    - echo "Hello " | tr -d "\n" > file1.txt
    - echo "world" > file2.txt
  script: cat file1.txt file2.txt | grep -q 'Hello world'
```

```bash
git add .gitlab-ci.yml
git commit -m "step 1: minimal test job"
git push origin main
```

**Что происходит:**
- GitLab Runner поднимает контейнер (по умолчанию `ruby:3.1`).
- `before_script` создаёт два файла; `script` проверяет их содержимое через `grep`.
- exit code 0 → ✅ ; non-zero → ❌

**Проверьте в UI:** Build → Pipelines → нажмите на job `test` → просмотрите лог. Вы увидите каждую команду и `Job succeeded`.

> **Эксперимент:** измените `world` на `World` в `.gitlab-ci.yml` — grep завершится с кодом 1, job упадёт. Верните обратно.

---

## Шаг 2 — артефакты сборки

Добавьте второй job `package` с `artifacts`:

```yaml
test:
  before_script:
    - echo "Hello " | tr -d "\n" > file1.txt
    - echo "world" > file2.txt
  script: cat file1.txt file2.txt | grep -q 'Hello world'

package:
  before_script:
    - echo "Hello " | tr -d "\n" > file1.txt
    - echo "world" > file2.txt
  script: cat file1.txt file2.txt | gzip > packaged.gz
  artifacts:
    paths:
      - packaged.gz
```

```bash
git add .gitlab-ci.yml
git commit -m "step 2: add package job with artifacts"
git push origin main
```

**Что происходит:**
- Оба job'а выполняются **параллельно** (нет stages → нет барьера).
- `package` сохраняет `packaged.gz` в GitLab storage.

**Проверьте:** на странице job'а `package` появится кнопка **Download artifacts**. Нажмите — скачается `packaged.gz`.

> **Проблема:** `test` и `package` используют одни файлы, но `file1.txt` / `file2.txt` создаются только в `test`, а в `package` их нет. Поэтому здесь `before_script` приходится дублировать в обоих jobs (`package` без него упадёт). Это мотивация для шага 4.

---

## Шаг 3 — sequential execution через `stages`

Добавьте `stages` чтобы `package` всегда ждал `test`:

```yaml
stages:
  - test
  - package

test:
  stage: test
  before_script:
    - echo "Hello " | tr -d "\n" > file1.txt
    - echo "world" > file2.txt
  script: cat file1.txt file2.txt | grep -q 'Hello world'

package:
  stage: package
  before_script:
    - echo "Hello " | tr -d "\n" > file1.txt
    - echo "world" > file2.txt
  script: cat file1.txt file2.txt | gzip > packaged.gz
  artifacts:
    paths:
      - packaged.gz
```

```bash
git add .gitlab-ci.yml
git commit -m "step 3: add stages for sequential execution"
git push origin main
```

**Что изменилось:** pipeline UI теперь показывает два этапа. `package` не стартует, пока `test` не завершится.

**Проблема этого подхода:** мы дублируем `before_script` в обоих jobs. Шаг 4 это исправит.

---

## Шаг 4 — compile once, use many

Выносим создание файлов в отдельный `compile` stage. `test` и `package` наследуют artifact:

```yaml
image: alpine

stages:
  - compile
  - test
  - package

compile:
  stage: compile
  script: |
    echo "Hello " | tr -d "\n" > file1.txt
    echo "world" > file2.txt
    cat file1.txt file2.txt > compiled.txt
  artifacts:
    paths:
      - compiled.txt
    expire_in: 20 minutes

test:
  stage: test
  script: cat compiled.txt | grep -q 'Hello world'

package:
  stage: package
  script: cat compiled.txt | gzip > packaged.gz
  artifacts:
    paths:
      - packaged.gz
```

```bash
git add .gitlab-ci.yml
git commit -m "step 4: compile stage, compile once use many"
git push origin main
```

**Что изменилось:**
- `compile` создаёт `compiled.txt` и сохраняет как artifact.
- `test` и `package` автоматически получают `compiled.txt` из предыдущего stage — не нужно ничего настраивать.
- `image: alpine` — лёгкий образ вместо `ruby:3.1` (~900 МБ).

**Проверьте:** на странице job'а `test` в секции **Job artifacts** видно, что был скачан артефакт из `compile`.

---

## Шаг 5 — parallel jobs в одном stage

Добавьте второй формат упаковки. Оба job'а запускаются **одновременно**:

```yaml
image: alpine

stages:
  - compile
  - test
  - package

compile:
  stage: compile
  script: |
    echo "Hello " | tr -d "\n" > file1.txt
    echo "world" > file2.txt
    cat file1.txt file2.txt > compiled.txt
  artifacts:
    paths:
      - compiled.txt
    expire_in: 20 minutes

test:
  stage: test
  script: cat compiled.txt | grep -q 'Hello world'

pack-gz:
  stage: package
  script: cat compiled.txt | gzip > packaged.gz
  artifacts:
    paths:
      - packaged.gz

pack-iso:
  stage: package
  before_script:
    - apk update && apk add xorriso
  script: mkisofs -o ./packaged.iso ./compiled.txt
  artifacts:
    paths:
      - packaged.iso
```

```bash
git add .gitlab-ci.yml
git commit -m "step 5: parallel pack-gz and pack-iso jobs"
git push origin main
```

**Что изменилось:** в stage `package` теперь два job'а. В UI они запускаются одновременно. Общее время stage = max(pack-gz, pack-iso), а не сумма.

**Проверьте:** в UI pipeline — stage `package` содержит оба job'а рядом. Посмотрите их длительность.

---

## Шаг 6 — DAG через `needs`

Добавьте `needs: [compile, test]` к обоим pack-* jobs. Оба нужны: `compile` даёт `compiled.txt` (artifacts берутся только из перечисленных в `needs` jobs, без `needs: ["test"]` артефакты `compile` не придут), а `test` задаёт порядок. Теперь они стартуют сразу после своих зависимостей, не ожидая завершения всего stage `test`:

```yaml
image: alpine

stages:
  - compile
  - test
  - package

compile:
  stage: compile
  script: |
    echo "Hello " | tr -d "\n" > file1.txt
    echo "world" > file2.txt
    cat file1.txt file2.txt > compiled.txt
  artifacts:
    paths:
      - compiled.txt
    expire_in: 20 minutes

test:
  stage: test
  script: cat compiled.txt | grep -q 'Hello world'

pack-gz:
  stage: package
  script: cat compiled.txt | gzip > packaged.gz
  needs: [compile, test]
  artifacts:
    paths:
      - packaged.gz

pack-iso:
  stage: package
  before_script:
    - apk update && apk add xorriso
  script: mkisofs -o ./packaged.iso ./compiled.txt
  needs: [compile, test]
  artifacts:
    paths:
      - packaged.iso
```

```bash
git add .gitlab-ci.yml
git commit -m "step 6: DAG with needs, out-of-order execution"
git push origin main
```

**Что изменилось:** `needs` создаёт явную зависимость. Если в `test` stage было несколько jobs (например, медленный e2e), `pack-gz` и `pack-iso` больше не ждут всех — стартуют как только нужный им `test` завершился.

**Проверьте:** в UI pipeline нажмите кнопку **Show dependencies** (иконка графа) — увидите DAG с рёбрами между jobs.

---

## Шаг 7 — эксперимент: сломать pipeline намеренно

Убедитесь, что stage-барьер работает — если `test` упадёт, `pack-*` не запустятся:

```bash
# Сломайте test job намеренно
sed -i.bak 's/Hello world/Hello World/' .gitlab-ci.yml
git add .gitlab-ci.yml
git commit -m "experiment: break test job"
git push origin main
```

**Что должно произойти:** `test` упадёт с exit code 1. `pack-gz` и `pack-iso` будут в статусе `skipped`.

Верните обратно:

```bash
sed -i.bak 's/Hello World/Hello world/' .gitlab-ci.yml
git add .gitlab-ci.yml
git commit -m "fix: restore test assertion"
git push origin main
```

---

## Self-check

- [ ] Проект создан и склонирован локально.
- [ ] Шаг 1 — pipeline с одним job'ом зелёный.
- [ ] Шаг 2 — job `package` создаёт артефакт, доступный для скачивания в UI (оба job'а создают файлы в `before_script`).
- [ ] Шаг 3 — pipeline UI показывает два stage; `package` стартует только после `test`.
- [ ] Шаг 4 — `compile` job создаёт `compiled.txt`; `test` и `package` используют его без дублирования команд.
- [ ] Шаг 5 — `pack-gz` и `pack-iso` выполняются параллельно в UI.
- [ ] Шаг 6 — pipeline показывает DAG-граф через **Show dependencies**.
- [ ] Шаг 7 — намеренно сломанный `test` блокирует `pack-*` jobs.
- [ ] Понимаете разницу: stages = барьер синхронизации; needs = явная DAG-зависимость.

---

## Что дальше

- **Добавьте `deploy` stage** с `rules: - if: '$CI_COMMIT_BRANCH == "main"'` — только main-ветка деплоит.
- **Добавьте `cache:`** для `apk` в `pack-iso` — ускорьте повторные запуски.
- **Добавьте `parallel:matrix`** в `pack-gz` — соберите несколько форматов одним job-определением.
- **`needs: []`** — задайте для lint-job'а, который должен стартовать немедленно без ожидания compile.

### Полезные ссылки

- [Basics of GitLab CI — updated](https://about.gitlab.com/blog/basics-of-gitlab-ci-updated/)
- [`needs:` keyword reference](https://docs.gitlab.com/ee/ci/yaml/index.html#needs)
- [DAG pipelines](https://docs.gitlab.com/ee/ci/directed_acyclic_graph/)
- [`artifacts:` reference](https://docs.gitlab.com/ee/ci/yaml/index.html#artifacts)
