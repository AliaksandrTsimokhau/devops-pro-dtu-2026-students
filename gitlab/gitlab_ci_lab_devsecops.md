# Lab: DevSecOps в GitLab CI — secrets, SAST, dependencies, IaC

**Duration:** 45–60 min  
**Difficulty:** ★★★☆☆

Собираем shift-left пайплайн, который ловит четыре класса проблем ещё до деплоя: секреты в коде, небезопасный код (SAST), уязвимые зависимости и ошибки в Dockerfile. Всё работает на **GitLab Free** и на self-hosted CE в kind — без платных security-шаблонов.

**What you learn:**
- `gitleaks` — поиск секретов в коде и в git-истории
- `semgrep` — SAST обычным job-ом (не Ultimate-шаблон)
- `trivy fs` — уязвимости в зависимостях, `trivy config` — misconfig в Dockerfile/IaC
- JSON-артефакты и quality gate: report mode на ветках, enforce mode на `main`
- Что даёт платный Security-функционал GitLab и чем его заменить

**Requires:** Runner (Docker или Kubernetes executor), **без DinD и privileged**. Доступ раннера в интернет (скачивание образов, правил semgrep, баз Trivy).  
**Lab order:** trivy lab first ([`gitlab_ci_lab_basic_trivy_scan.md`](gitlab_ci_lab_basic_trivy_scan.md)), then this lab. Предыдущая лаба — Trivy по образу через DinD; её job `trivy-repo-scan` сканирует репозиторий образом `aquasec/trivy` без DinD, как и здесь.

> В отличие от Trivy-лабы здесь нет `docker build`, поэтому DinD не нужен. Все сканеры работают по файлам репозитория — это дешевле и быстрее.

---

## Pipeline at a glance

```
test:   gitleaks ─┐
        semgrep  ─┼──► security-gate (gate)
        trivy-fs ─┤
        trivy-cfg ┘
```

Четыре независимых job-а в одном stage + `security-gate`, который читает артефакты и решает, пускать ли дальше. Gate читает отчёты gitleaks и Trivy; semgrep в gate **намеренно не входит**: он блокирует сам через `--error` на `main`.

---

## Step 1: Vulnerable sample repo

Создайте проект `devsecops-lab` и добавьте файлы. Все проблемы **намеренные**.

```bash
mkdir -p app && cat > app/main.py << 'EOF'
import subprocess
import yaml
from flask import Flask, request

app = Flask(__name__)

# Fake secret для лабы, не настоящий токен
GITLAB_TOKEN = "glpat-x7Kq9Zr2Lm4Tv8Bn3Wc5"

@app.route("/ping")
def ping():
    host = request.args.get("host", "localhost")
    return subprocess.check_output("ping -c1 " + host, shell=True)

@app.route("/load", methods=["POST"])
def load():
    return str(yaml.load(request.data, Loader=yaml.Loader))

if __name__ == "__main__":
    app.run(host="0.0.0.0", debug=True)
EOF

cat > requirements.txt << 'EOF'
flask==0.12.2
PyYAML==5.3
EOF

cat > Dockerfile << 'EOF'
FROM python:latest
ADD . /app
WORKDIR /app
RUN pip install -r requirements.txt
CMD ["python", "app/main.py"]
EOF
```

Что здесь не так (проверьте себя после сканов):

| Файл | Проблема | Какой сканер найдёт |
|---|---|---|
| `app/main.py` | токен `glpat-…` в коде | gitleaks |
| `app/main.py` | `shell=True` + пользовательский ввод, `yaml.load`, `debug=True` | semgrep (проверьте rule IDs в выводе: какие именно правила сработали, зависит от версии набора правил) |
| `requirements.txt` | старые `flask` и `PyYAML` с известными CVE | trivy fs |
| `Dockerfile` | тег `latest`, `ADD`, нет `USER` и `HEALTHCHECK` | trivy config |

```bash
git add . && git commit -m "add vulnerable sample app" && git push origin main
```

---

## Step 2: Scanner jobs

Версии образов **не закрепляем по памяти**, поэтому три образа заданы переменными в одном блоке `variables:` в начале файла. Пока вы не замените значение после двоеточия на реальный тег, job упадёт с `invalid reference format` — это ожидаемо.

**Как выбрать тег:**

1. Откройте страницу тегов образа на Docker Hub: `zricethezav/gitleaks`, `semgrep/semgrep`, `aquasec/trivy`.
2. Возьмите последний стабильный тег (не `latest`, не `rc`/`dev`).
3. Проверьте локально, что образ запускается: `docker run --rm <образ>:<тег> --version` (для gitleaks — `version`, для trivy — `version`).
4. Подставьте тег в `variables:` и закоммитьте. Выбранные теги запишите в README проекта.

Для trivy тоже берём тег вместо `latest`: в предыдущей лабе образ для `trivy-repo-scan` тоже задан переменной `TRIVY_IMAGE`, и тег там нужно `проверить` так же.

Создайте `.gitlab-ci.yml`:

```yaml
stages: [test, gate]

variables:
  GITLEAKS_IMAGE: zricethezav/gitleaks:ЗАМЕНИТЕ_НА_ТЕГ
  SEMGREP_IMAGE: semgrep/semgrep:ЗАМЕНИТЕ_НА_ТЕГ
  TRIVY_IMAGE: aquasec/trivy:ЗАМЕНИТЕ_НА_ТЕГ
  TRIVY_NO_PROGRESS: "true"
  TRIVY_CACHE_DIR: ".trivycache/"

.scan:
  stage: test
  # Report mode на ветках и MR, enforce mode на default-ветке
  allow_failure: true
  rules:
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH
      allow_failure: false
    - when: on_success
  artifacts:
    when: always
    expire_in: 1 week

gitleaks:
  extends: .scan
  image:
    name: $GITLEAKS_IMAGE
    entrypoint: [""]
  variables:
    GIT_DEPTH: 0                      # нужна вся история, иначе старые коммиты не проверятся
  script:
    - gitleaks detect --source . --redact --report-format json --report-path gitleaks-report.json -v
  artifacts:
    paths: [gitleaks-report.json]

semgrep:
  extends: .scan
  image:
    name: $SEMGREP_IMAGE
    entrypoint: [""]
  script:
    - semgrep scan --config p/python --config p/secrets --error --json-output=semgrep-report.json .
  artifacts:
    paths: [semgrep-report.json]

trivy-fs:
  extends: .scan
  image:
    name: $TRIVY_IMAGE
    entrypoint: [""]
  script:
    - trivy fs --scanners vuln --severity HIGH,CRITICAL --format json --output trivy-fs.json --exit-code 0 .
    - trivy fs --scanners vuln --severity HIGH,CRITICAL --format table --exit-code 1 .
  cache:
    paths: [.trivycache/]
  artifacts:
    paths: [trivy-fs.json]

trivy-config:
  extends: .scan
  image:
    name: $TRIVY_IMAGE
    entrypoint: [""]
  script:
    - trivy config --severity MEDIUM,HIGH,CRITICAL --format json --output trivy-config.json --exit-code 0 .
    - trivy config --severity HIGH,CRITICAL --format table --exit-code 1 .
  artifacts:
    paths: [trivy-config.json]
```

Пояснения:

- **`entrypoint: [""]`** — образы сканеров имеют свой entrypoint, а GitLab Runner запускает `script` через shell.
- **Двойной запуск Trivy** — первый пишет полный JSON и всегда возвращает 0, второй печатает таблицу и падает по порогу. Отчёт сохраняется даже при красном job-е (`artifacts: when: always`).
- **`GIT_DEPTH: 0`** — по умолчанию GitLab делает shallow clone; секрет, удалённый в прошлом коммите, останется незамеченным.
- **`rules: allow_failure:`** — один и тот же job жёлтый на feature-ветке и красный на `main`.
- **`semgrep --error`** — без флага semgrep возвращает 0 даже с findings.
- **Правила semgrep** — `p/python` и `p/secrets` загружаются с registry. Какие именно rule IDs сработают на `main.py`, зависит от версии набора правил: сверьтесь с выводом job-а и запишите ID. Если registry требует логин, используйте локальные правила (`--config ./rules`).

```bash
git add .gitlab-ci.yml && git commit -m "ci: add security scanners" && git push origin main
```

Pipeline на `main` должен **упасть** по всем четырём job-ам. Откройте логи и сопоставьте findings с таблицей из Step 1.

> Если gitleaks ничего не нашёл — токен недостаточно «случайный» для правила (энтропия). Замените символы после `glpat-` на другие 20 случайных. Токен должен остаться фейковым.

---

## Step 3: Quality gate

Gate читает JSON из артефактов и применяет единую политику: сколько CRITICAL допустимо. На `main` он **блокирует**, на остальных ветках только печатает предупреждение (report mode).

Добавьте job **в конец** `.gitlab-ci.yml` (после `trivy-config`):

```yaml
security-gate:
  stage: gate
  image: alpine:3.20   # проверь поддерживаемую версию на https://alpinelinux.org/releases/ и обновите тег, если ветка EOL
  needs:
    - job: gitleaks
      artifacts: true
      optional: true
    - job: trivy-fs
      artifacts: true
      optional: true
    - job: trivy-config
      artifacts: true
      optional: true
  variables:
    MAX_CRITICAL: "0"
  before_script:
    - apk add --no-cache jq
  script:
    - |
      LEAKS=$(jq 'length' gitleaks-report.json 2>/dev/null || echo 0)
      VULN=$(jq '[.Results[]?.Vulnerabilities[]? | select(.Severity=="CRITICAL")] | length' trivy-fs.json 2>/dev/null || echo 0)
      MISCONF=$(jq '[.Results[]?.Misconfigurations[]? | select(.Severity=="CRITICAL")] | length' trivy-config.json 2>/dev/null || echo 0)
      echo "secrets=$LEAKS critical_vulns=$VULN critical_misconfigs=$MISCONF (max critical: $MAX_CRITICAL)"
      if [ "$CI_COMMIT_BRANCH" = "$CI_DEFAULT_BRANCH" ]; then ENFORCE=1; else ENFORCE=0; fi
      STATUS=0
      [ "$LEAKS" -eq 0 ] || { echo "FOUND: secrets"; STATUS=1; }
      [ $((VULN + MISCONF)) -le "$MAX_CRITICAL" ] || { echo "FOUND: too many CRITICAL findings"; STATUS=1; }
      if [ "$STATUS" -ne 0 ] && [ "$ENFORCE" -eq 0 ]; then echo "WARN: report mode on branch $CI_COMMIT_BRANCH, gate does not block"; exit 0; fi
      exit "$STATUS"
  rules:
    - when: always
```

Идея: сканеры дают подробности, gate даёт **одно место для политики**. Порог меняется переменной `MAX_CRITICAL` без правки сканеров. Секреты не терпим вообще. `when: always` нужен, чтобы gate запускался и после красных сканеров; решение «блокировать или предупредить» принимает сам скрипт по ветке.

```bash
git add .gitlab-ci.yml && git commit -m "ci: add security gate" && git push origin main
```

Pipeline на `main` падает, а `security-gate` печатает счётчики и `FOUND: secrets`. Теперь посмотрите report mode: создайте ветку **без исправлений** и запушьте.

```bash
git switch -c demo/report-mode
git commit --allow-empty -m "demo: report mode"
git push origin demo/report-mode
```

На этой ветке сканеры жёлтые (`allow_failure`), а gate зелёный и печатает `WARN: report mode`. Вернитесь на `main`: `git switch main`.

---

## Step 4: Fix everything

Исправьте в отдельной ветке и откройте MR:

```bash
git switch -c fix/security-findings
```

1. **Секрет.** Удалите токен из кода и читайте из окружения. Токен, попавший в историю, считается скомпрометированным: его нужно **отозвать**, а не просто удалить.
   ```python
   import os
   GITLAB_TOKEN = os.environ["GITLAB_TOKEN"]
   ```
2. **SAST.** Уберите `shell=True`, используйте безопасный loader и выключите debug:
   ```python
   subprocess.check_output(["ping", "-c1", host])
   yaml.safe_load(request.data)
   app.run(host="0.0.0.0", debug=False)
   ```
3. **Зависимости.** Обновите версии в `requirements.txt` до актуальных (`pip index versions flask`) и перезапустите `trivy fs`. Job `trivy-fs` блокирует по порогу **HIGH и CRITICAL**, а gate считает только CRITICAL, поэтому проверяйте, что зелёный именно `trivy-fs`.
4. **Dockerfile.**
   ```dockerfile
   FROM python:3.12-slim
   WORKDIR /app
   COPY requirements.txt .
   RUN pip install --no-cache-dir -r requirements.txt
   COPY app/ app/
   RUN useradd -r -u 10001 appuser
   USER 10001
   HEALTHCHECK CMD python -c "import socket; socket.create_connection(('127.0.0.1', 5000), 2)"
   CMD ["python", "app/main.py"]
   ```

Gitleaks всё равно будет красным на `main`, пока токен есть в истории. Для лабы допустимо добавить `.gitleaksignore` с fingerprint из отчёта, но в реальной жизни — отзовите токен и (если нужно) перепишите историю.

---

## What GitLab offers built-in (and what it costs)

Лаба использует обычные job-ы, потому что встроенные шаблоны зависят от тарифа.

| Возможность | Тариф |
|---|---|
| Свои job-ы со сканерами (эта лаба) | Free |
| SAST и Secret Detection шаблоны (`Security/SAST.gitlab-ci.yml`, `Security/Secret-Detection.gitlab-ci.yml`) | Free запускает их, но результат только в JSON-артефактах |
| Dependency Scanning, DAST, Security tab пайплайна, security widget в MR, Vulnerability Report | Ultimate |

> `проверь по документации вашей версии` GitLab: состав тарифов и то, что попадает в UI, меняются между релизами.

Практический вывод: на Free встроенного UI для уязвимостей нет. Результаты живут в логах и JSON-артефактах, а gate — это ваш job.

---

## Common pitfalls

- **Shallow clone в gitleaks** — без `GIT_DEPTH: 0` проверяется только последний коммит.
- **Пустой `trivy fs` по Python** — Trivy читает `requirements.txt` статически; `pip install` не нужен, но файл должен лежать в репозитории.
- **semgrep без интернета** — `p/python` скачивается с registry. В закрытой сети используйте локальный каталог правил через `--config ./rules`.
- **Вечная зелёность из-за `allow_failure: true`** — если забыли `rules: allow_failure: false` для `main`, сканеры на `main` остаются жёлтыми. Gate (`when: always`) по-прежнему падает на секретах и CRITICAL, но находки HIGH, ошибки `semgrep` и сбой самого сканера пройдут незамеченными.
- **Ложные срабатывания** — не отключайте сканер целиком. Используйте `.trivyignore`, `# nosemgrep`, `.gitleaksignore` с комментарием, почему игнорируем.
- **Тег `latest` для самих сканеров** — завтра правила изменятся, и вчерашний зелёный пайплайн станет красным без изменений в коде.

---

## Self-Check

- [ ] Sample-репозиторий запушен, пайплайн на `main` красный.
- [ ] Каждая строка таблицы из Step 1 сопоставлена с finding в логе или JSON.
- [ ] На feature-ветке сканеры жёлтые (report mode), на `main` красные (enforce mode).
- [ ] `security-gate` печатает счётчики; на `main` падает на секрете, на `demo/report-mode` зелёный с `WARN`.
- [ ] После fix-MR `trivy-fs`, `trivy-config` и `semgrep` зелёные (`gitleaks` красный, пока токен в истории).
- [ ] Можете объяснить, почему удаления секрета из кода недостаточно.

## What You Learned

- Четыре класса проблем (secrets, SAST, SCA, IaC) закрываются четырьмя дешёвыми job-ами без платных функций.
- Report mode + enforce mode позволяют внедрять security постепенно, не блокируя команду.
- Артефакты и gate отделяют «найти» от «решить», политику можно менять в одном месте.
- Платные возможности GitLab (Security tab, Dependency Scanning, DAST) удобны, но не обязательны для базовой гигиены.
