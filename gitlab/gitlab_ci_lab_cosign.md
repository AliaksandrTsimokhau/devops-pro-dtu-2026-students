# Lab: подпись и провенанс Docker-образов через Cosign в GitLab CI/CD

В этой лабе вы научите свой GitLab pipeline **подписывать собранные Docker-образы** и **аннотировать их метаданными билда** через [Cosign](https://github.com/sigstore/cosign) — без управления приватными ключами. Подпись происходит **keyless**: GitLab выпускает короткоживущий OIDC-токен, Sigstore Fulcio превращает его в сертификат, Rekor публикует факт подписи в неподделываемом transparency log.

Опираемся на статью: [Annotate container images with build provenance using Cosign in GitLab CI/CD](https://about.gitlab.com/blog/annotate-container-images-with-build-provenance-using-cosign-in-gitlab-ci-cd/).

## Что мы получим

После лабы у каждого собранного образа в вашем GitLab Container Registry будет:

- **криптографическая подпись** Cosign, привязанная к identity вашего GitLab проекта,
- **аннотации** с метаданными pipeline'а (commit SHA, pipeline ID, кто запустил, ref name и т.д.),
- **запись в Rekor** — публичный append-only log, через который любой может проверить, что образ действительно собран этим pipeline'ом, а не подменён.

## Почему это важно

| Сценарий | Без подписи | С Cosign + GitLab |
|---|---|---|
| Кто-то подменил образ в registry | Незаметно | `cosign verify` падает |
| Прод-инцидент, нужно найти билд | Гадаем по тегам | Аннотация → ссылка на pipeline и commit |
| Аудит CI/CD на compliance | Долгое расследование | Запись в Rekor + цепочка identity |
| Подтянули образ из publicly-mirrored registry | Доверяем тегу | Проверяем подпись и issuer |

## Prerequisites

- Аккаунт на **gitlab.com** (для `id_tokens` с OIDC; см. note ниже про self-hosted).
- Включённый Container Registry в проекте (Settings → General → Visibility → Container registry).
- Доступный shared runner (Docker-in-Docker поддерживается).
- Локально установленные:
  - `git`
  - `docker` (для опционального verify-step),
  - `cosign` (для локальной проверки — `brew install cosign` / [docs](https://docs.sigstore.dev/cosign/installation/)).
- 30–40 минут.

> Лаба работает **только на gitlab.com**. Публичный Sigstore Fulcio доверяет OIDC issuer только `https://gitlab.com`, поэтому keyless-подпись с self-hosted GitLab через публичный Sigstore не пройдёт. Для self-hosted нужны собственные Fulcio и Rekor (см. «Что дальше»).

---

## Шаг 1. Концептуальный обзор — что под капотом

Прежде чем кодить, разберём механику keyless signing — иначе магия `id_tokens` останется чёрным ящиком.

```
1. GitLab CI job стартует.
   GitLab генерирует JWT с claims:
     - issuer: https://gitlab.com
     - subject: project_path:my-group/my-project:ref_type:branch:ref:main
     - audience: sigstore

2. Cosign отправляет этот JWT в Fulcio (Sigstore CA).

3. Fulcio:
     - проверяет issuer (GitLab — он в allowlist),
     - проверяет подпись JWT (OIDC discovery),
     - выпускает X.509 сертификат с identity = subject из JWT.
     Срок жизни сертификата: 10 минут.

4. Cosign подписывает digest образа этим сертификатом.

5. Подпись + сертификат публикуются в:
     a) OCI registry рядом с образом (`<image>:<digest>.sig`),
     b) Rekor (transparency log) — append-only, immutable.

6. Verify-этап: cosign читает подпись из registry, сертификат из
   Rekor, проверяет цепочку доверия и матчит identity на ваш
   project URL.
```

**Ключевые компоненты:**

- **Fulcio** — Sigstore CA. Выпускает короткоживущие сертификаты на основе OIDC.
- **Rekor** — публичный append-only log всех подписей. URL: `https://rekor.sigstore.dev`.
- **`id_tokens` в GitLab** — встроенный механизм генерации OIDC токенов с настраиваемой `audience`.
- **Keyless** — у нас **нет** долгоживущих private keys. Identity подтверждается через GitLab OIDC.

---

## Шаг 2. Создаём проект и приложение

1. New project → `cosign-signed-images-lab` → Initialize with README.
2. Settings → General → Visibility → убедитесь, что **Container registry** включён.

Клонируем и создаём простое приложение:

```bash
git clone https://gitlab.example.com/<your-namespace>/cosign-signed-images-lab.git
cd cosign-signed-images-lab
```

**`Dockerfile`:**

```dockerfile
FROM alpine:3.20

LABEL maintainer="devops-lab"
LABEL description="Demo image for cosign provenance lab"

RUN apk add --no-cache curl

COPY hello.sh /usr/local/bin/hello
RUN chmod +x /usr/local/bin/hello

ENTRYPOINT ["/usr/local/bin/hello"]
```

> `alpine:3.20`: `проверь поддерживаемую версию` на https://alpinelinux.org/releases/. Если ветка уже EOL, возьмите актуальную stable-ветку и поправьте тег здесь.

**`hello.sh`:**

```bash
#!/bin/sh
echo "Hello from a signed image!"
echo "Built at: $(date)"
```

> Это намеренно тривиальное приложение — фокус лабы на цепочке подписи, а не на бизнес-логике.

---

## Шаг 3. Pipeline для сборки, подписи и проверки

Создайте `.gitlab-ci.yml` в корне:

```yaml
stages:
  - build

build_and_sign:
  stage: build
  image: docker:27
  services:
    - name: docker:27-dind
  variables:
    IMAGE_TAG: $CI_COMMIT_SHORT_SHA
    IMAGE_URI: $CI_REGISTRY_IMAGE:$IMAGE_TAG
    COSIGN_YES: "true"
    FF_SCRIPT_SECTIONS: "true"
  id_tokens:
    SIGSTORE_ID_TOKEN:
      aud: sigstore
  before_script:
    - apk add --no-cache cosign jq
    - docker login -u "gitlab-ci-token" -p "$CI_JOB_TOKEN" "$CI_REGISTRY"
  script:
    - docker build --pull -t "$IMAGE_URI" .
    - docker push "$IMAGE_URI"
    - IMAGE_DIGEST=$(docker inspect --format='{{index .RepoDigests 0}}' "$IMAGE_URI")
    - |
      cosign sign "$IMAGE_DIGEST" \
        --annotations "com.gitlab.ci.user.name=$GITLAB_USER_NAME" \
        --annotations "com.gitlab.ci.pipeline.id=$CI_PIPELINE_ID" \
        --annotations "com.gitlab.ci.pipeline.url=$CI_PIPELINE_URL" \
        --annotations "com.gitlab.ci.job.id=$CI_JOB_ID" \
        --annotations "com.gitlab.ci.job.url=$CI_JOB_URL" \
        --annotations "com.gitlab.ci.commit.sha=$CI_COMMIT_SHA" \
        --annotations "com.gitlab.ci.commit.ref.name=$CI_COMMIT_REF_NAME" \
        --annotations "com.gitlab.ci.project.path=$CI_PROJECT_PATH" \
        --annotations "org.opencontainers.image.source=$CI_PROJECT_URL" \
        --annotations "org.opencontainers.image.revision=$CI_COMMIT_SHA" \
        --annotations "tag=$IMAGE_TAG"
    - |
      cosign verify \
        --annotations "tag=$IMAGE_TAG" \
        --certificate-identity "$CI_PROJECT_URL//.gitlab-ci.yml@refs/heads/$CI_COMMIT_REF_NAME" \
        --certificate-oidc-issuer "$CI_SERVER_URL" \
        "$IMAGE_URI" | jq .
```

### Разбор по строкам

**`image: docker:27` + `services: docker:27-dind`**
Классическая связка Docker-in-Docker. Без неё мы не сможем выполнять `docker build/push` внутри job. Тег закреплён (как в лабе trivy), а не `latest`, чтобы pipeline не менялся без изменений в коде.

**`COSIGN_YES: "true"`**
Cosign по умолчанию интерактивно подтверждает publish в Rekor. В CI это блокирует pipeline. `COSIGN_YES=true` — non-interactive.

**`FF_SCRIPT_SECTIONS: "true"`**
GitLab feature flag — каждая команда `script:` сворачивается в свою collapsible секцию в UI. Не обязательно, но удобно для дебага.

**`id_tokens: SIGSTORE_ID_TOKEN: aud: sigstore`**
Это и есть OIDC-магия. GitLab генерирует JWT с audience `sigstore` и кладёт его в переменную `SIGSTORE_ID_TOKEN`. Cosign автоматически читает её именно из этой переменной.

**`apk add --no-cache cosign jq`**
`cosign` ставится из Alpine-репозитория образа. `проверь: apk info cosign` (или `apk search cosign`) в `docker:27`, какая версия пакета доступна. `jq` нужен для красивого вывода verify-результата.

> `проверь` версию cosign. В cosign v3 формат bundle и раскладка подписей в registry могут отличаться от описанных здесь (например, `sha256-…sig`-теги). Зафиксируйте версию пакета (`apk add cosign=<версия>`, `проверь` точное имя пакета и версии) и сверьте флаги с `cosign sign --help` и `cosign verify --help`.

**`docker login -u "gitlab-ci-token" -p "$CI_JOB_TOKEN" "$CI_REGISTRY"`**
Авторизация в GitLab Container Registry через автогенерируемый `CI_JOB_TOKEN`. Никаких manual credentials.

**`IMAGE_DIGEST=$(docker inspect --format='{{index .RepoDigests 0}}' "$IMAGE_URI")`**
Cosign подписывает **digest**, а не **tag**. Tag — изменяемая ссылка, digest — content-addressed (`sha256:...`). Подпись по digest гарантирует, что подменить контент незаметно невозможно.

**`cosign sign ... --annotations ...`**
Подписывает digest и **добавляет аннотации** в payload подписи. Аннотации — это **подписанные метаданные**, которые нельзя подделать после подписания.

**`cosign verify --certificate-identity ... --certificate-oidc-issuer ...`**
Проверка: подпись валидна **И** identity сертификата матчит ваш `.gitlab-ci.yml` на указанной ветке у указанного issuer. То есть подписать мог **только** этот pipeline в этом проекте.

---

## Шаг 4. Запускаем и смотрим, что произошло

```bash
git add Dockerfile hello.sh .gitlab-ci.yml
git commit -m "feat: cosign-signed image pipeline"
git push origin main
```

Откройте Build → Pipelines → `build_and_sign` job. По логу пройдитесь по секциям:

1. **`docker build`** — стандартная сборка alpine-образа.
2. **`docker push`** — push в `registry.gitlab.com/<your-namespace>/cosign-signed-images-lab:<sha>`.
3. **`docker inspect --format='{{index .RepoDigests 0}}'`** — должно вывести `<image>@sha256:...`.
4. **`cosign sign`** — должен напечатать `tlog entry created with index: <N>` (это запись в Rekor!).
5. **`cosign verify`** — должен напечатать JSON с массивом claim'ов, включая все ваши аннотации.

> Если `cosign sign` падает с `fetching token` — проверьте, что в job есть блок `id_tokens:`, и `aud: sigstore` написан буквально.

### Что нужно увидеть в Rekor

Из логов скопируйте `tlog entry index`. Откройте https://search.sigstore.dev/?logIndex=<N> — увидите вашу запись с полным content-addressed payload.

---

## Шаг 5. Локальная верификация

На своей машине проверим подпись:

```bash
cosign verify \
  --certificate-identity "https://gitlab.com/<your-namespace>/cosign-signed-images-lab//.gitlab-ci.yml@refs/heads/main" \
  --certificate-oidc-issuer "https://gitlab.com" \
  registry.gitlab.com/<your-namespace>/cosign-signed-images-lab:<sha> | jq .
```

> Замените `<your-namespace>` и `<sha>` на свои. Лаба рассчитана на gitlab.com (см. note в начале).

Получите JSON примерно вида:

```json
[
  {
    "critical": {
      "identity": { "docker-reference": "registry.gitlab.com/..." },
      "image": { "docker-manifest-digest": "sha256:..." },
      "type": "cosign container image signature"
    },
    "optional": {
      "Subject": "https://gitlab.com/.../.gitlab-ci.yml@refs/heads/main",
      "Issuer": "https://gitlab.com",
      "com.gitlab.ci.user.name": "Your Name",
      "com.gitlab.ci.pipeline.id": "123456789",
      "com.gitlab.ci.commit.sha": "abc123...",
      "tag": "abc123"
    }
  }
]
```

→ **Это и есть provenance.** Любой downstream (например, admission controller в Kubernetes) может ту же команду запустить и принять/отклонить образ.

---

## Шаг 6. Тест на подмену

Самое интересное: проверим, что подпись действительно защищает.

### Случай 1 — verify с неправильным issuer

```bash
cosign verify \
  --certificate-identity "https://gitlab.com/.../.gitlab-ci.yml@refs/heads/main" \
  --certificate-oidc-issuer "https://github.com" \
  registry.gitlab.com/.../cosign-signed-images-lab:<sha>
```

→ Ошибка: `no matching signatures: certificate issuer does not match`.

### Случай 2 — verify с неправильной веткой

```bash
cosign verify \
  --certificate-identity ".../.gitlab-ci.yml@refs/heads/feature-x" \
  --certificate-oidc-issuer "https://gitlab.com" \
  registry.gitlab.com/.../cosign-signed-images-lab:<sha>
```

→ Ошибка: identity mismatch.

### Случай 3 — verify несуществующего образа

```bash
cosign verify \
  --certificate-identity "..." \
  --certificate-oidc-issuer "https://gitlab.com" \
  registry.gitlab.com/.../cosign-signed-images-lab:fake-tag
```

→ Ошибка: no signatures found.

> Эти три кейса — основа любой downstream policy. Запомните их.

---

## Шаг 7. Расширение — подпись с tag-mutation guard

Сейчас в pipeline есть проблема: если кто-то перезапустит pipeline для того же `$CI_COMMIT_SHA` — подпись перезапишется. Это не катастрофа, но для production лучше явный guard.

Добавьте в `script:` перед `cosign sign`:

```yaml
    - |
      if cosign verify \
        --certificate-identity "$CI_PROJECT_URL//.gitlab-ci.yml@refs/heads/$CI_COMMIT_REF_NAME" \
        --certificate-oidc-issuer "$CI_SERVER_URL" \
        "$IMAGE_DIGEST" >/dev/null 2>&1; then
        echo "Image already signed by this pipeline identity — skipping re-sign"
        exit 0
      fi
```

> Проверка идёт через `cosign verify` с той же identity, что и в шаге verify: если валидная подпись уже есть, повторно подписывать не нужно. `проверь` на вашей версии cosign: вывод `cosign tree` (показывает signatures, attestations, SBOMs) может не содержать строки `Signatures:`, поэтому grep по нему не использовать. Также `проверь`, что `exit 0` здесь завершает job без ошибки, как вы и хотите.

---

## Шаг 8. Бонус — добавляем SBOM как attestation

Поверх подписи можно прикрепить **подписанную SBOM** (Software Bill of Materials) — список всех пакетов в образе.

Добавьте в pipeline новый job:

```yaml
attach_sbom:
  stage: build
  image: docker:27
  services:
    - name: docker:27-dind
  needs: ["build_and_sign"]
  variables:
    IMAGE_TAG: $CI_COMMIT_SHORT_SHA
    IMAGE_URI: $CI_REGISTRY_IMAGE:$IMAGE_TAG
    COSIGN_YES: "true"
  id_tokens:
    SIGSTORE_ID_TOKEN:
      aud: sigstore
  before_script:
    - apk add --no-cache cosign syft jq
    - docker login -u "gitlab-ci-token" -p "$CI_JOB_TOKEN" "$CI_REGISTRY"
  script:
    - IMAGE_DIGEST=$(docker pull "$IMAGE_URI" >/dev/null && docker inspect --format='{{index .RepoDigests 0}}' "$IMAGE_URI")
    - syft "$IMAGE_DIGEST" -o cyclonedx-json > sbom.json
    - cosign attest --predicate sbom.json --type cyclonedx "$IMAGE_DIGEST"
    - |
      cosign verify-attestation --type cyclonedx \
        --certificate-identity "$CI_PROJECT_URL//.gitlab-ci.yml@refs/heads/$CI_COMMIT_REF_NAME" \
        --certificate-oidc-issuer "$CI_SERVER_URL" \
        "$IMAGE_URI" | jq .
  artifacts:
    paths:
      - sbom.json
    expire_in: 30 days
```

> `syft` — генератор SBOM от Anchore. `проверь`, что пакет `syft` есть в Alpine-репозитории образа `docker:27` (`apk info syft`), иначе используйте [official binary](https://github.com/anchore/syft). Флаги `cosign attest --help` и `cosign verify-attestation --help` тоже `проверь` для вашей версии cosign (в v3 `--type` и формат predicate могут отличаться).

---

## Шаг 9. Self-check

- [ ] Pipeline `build_and_sign` зелёный.
- [ ] В Container Registry лежит ваш образ.
- [ ] Рядом с образом видна подпись (в старых версиях cosign это теги `sha256-...sig`; `проверь` для вашей версии через `cosign tree <digest>`).
- [ ] `cosign verify` локально проходит с правильным identity.
- [ ] `cosign verify` падает с неправильным issuer/branch/tag.
- [ ] В выводе verify видны ваши аннотации (`com.gitlab.ci.pipeline.id`, и т.д.).
- [ ] Запись в Rekor найдена через https://search.sigstore.dev.
- [ ] (бонус) `attach_sbom` job собрал SBOM и attestation.
- [ ] Понимаете, чем подпись по digest отличается от подписи по tag.

---

## Что дальше

- **Admission control в Kubernetes** — поднимите [Kyverno](https://kyverno.io) или [Cosign policy-controller](https://docs.sigstore.dev/policy-controller/overview/). Настройте policy, которая допускает в кластер **только** образы, подписанные вашим GitLab pipeline'ом.
- **GitLab CI/CD Component** — оберните `build_and_sign` в reusable component, опубликуйте в CI/CD Catalog (см. лабу `gitlab_ci_lab_component.md`). Другие проекты подключают одной строкой.
- **Подпись release-тегов** — добавьте rules: образы с `$CI_COMMIT_TAG` тегаются как `v1.2.3` и подписываются с identity `refs/tags/v1.2.3` — это надёжнее, чем `refs/heads/main`.
- **Vulnerability gating через SBOM** — `grype sbom.json` или `trivy sbom --severity CRITICAL,HIGH` → если CVE → break pipeline.
- **Cosign keyless без Sigstore** — для air-gapped окружений можно поднять собственный Fulcio + Rekor (документация Sigstore).

### Полезные ссылки

- [Original article](https://about.gitlab.com/blog/annotate-container-images-with-build-provenance-using-cosign-in-gitlab-ci-cd/)
- [Cosign documentation](https://docs.sigstore.dev/cosign/overview/)
- [Sigstore overview](https://docs.sigstore.dev/)
- [GitLab `id_tokens` reference](https://docs.gitlab.com/ee/ci/yaml/index.html#id_tokens)
- [Rekor search](https://search.sigstore.dev/)
- [Syft (SBOM generator)](https://github.com/anchore/syft)
- [Kyverno policies for Cosign](https://kyverno.io/policies/?policytypes=verifyImages)
