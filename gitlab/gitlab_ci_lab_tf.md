# Lab: OpenTofu state в GitLab

Создадим pipeline `validate -> plan -> apply -> destroy` с GitLab HTTP backend. Пример работает на GitLab.com и на Servercore GitLab CE. Используем OpenTofu 1.13.1 напрямую; внешние CI/CD components и удалённые Terraform templates не нужны.

**Требуется:** роль Maintainer или Owner в проекте (для записи и блокировки state), runner с Docker или Kubernetes executor, доступ из job к GitLab API и `ghcr.io`. На Servercore сначала пройдите [подготовку стенда](servercore_gitlab.md). Shell executor не исполняет указанный `image:`.

## Шаг 1. Проект и минимальная инфраструктура

Создайте private-проект `terraform-gitlab-ci-lab` с README, склонируйте его по URL из UI и создайте каталоги:

```bash
mkdir -p terraform .gitlab-ci
```

В `terraform/main.tf`:

```hcl
terraform {
  required_version = ">= 1.13.1, < 2.0"
  backend "http" {}
}

resource "terraform_data" "example" {
  input = "gitlab-ci-lab"
}
```

Этот ресурс встроен в OpenTofu: облачный аккаунт и PAT для первой лабы не нужны. Добавьте в `.gitignore`:

```gitignore
**/.terraform/
**/plan.cache
**/plan.json
*.tfstate
*.tfstate.*
```

При добавлении внешних providers создайте и закоммитьте `.terraform.lock.hcl`. Не коммитьте state и планы: они могут содержать секреты.

## Шаг 2. Общие jobs: `.gitlab-ci/opentofu.yml`

Скопируйте весь блок в этот файл. Этот же файл используется в [лабе с пятью окружениями](gitlab_ci_lab_five_envs.md).

```yaml
.tofu:
  image:
    name: ghcr.io/opentofu/opentofu:1.13.1
    entrypoint: [""]
  variables:
    TF_ROOT: terraform
    TF_IN_AUTOMATION: "true"
    TF_INPUT: "false"
  before_script:
    - |
      export TF_HTTP_ADDRESS="${CI_API_V4_URL}/projects/${CI_PROJECT_ID}/terraform/state/${TF_STATE_NAME}"
      export TF_HTTP_LOCK_ADDRESS="${TF_HTTP_ADDRESS}/lock"
      export TF_HTTP_UNLOCK_ADDRESS="${TF_HTTP_ADDRESS}/lock"
      export TF_HTTP_LOCK_METHOD=POST TF_HTTP_UNLOCK_METHOD=DELETE
      export TF_HTTP_USERNAME=gitlab-ci-token TF_HTTP_PASSWORD="$CI_JOB_TOKEN"
      export TF_HTTP_RETRY_WAIT_MIN=5
      cd "$CI_PROJECT_DIR/$TF_ROOT"
      tofu init -reconfigure

.tofu-validate:
  extends: .tofu
  stage: validate
  script:
    - tofu fmt -check
    - tofu validate

.tofu-plan:
  extends: .tofu
  stage: plan
  resource_group: state-$TF_STATE_NAME
  script:
    - tofu plan -lock-timeout=60s -out=plan.cache
    - tofu show -json plan.cache > plan.json
  artifacts:
    access: maintainer
    expire_in: 1 day
    paths:
      - ${TF_ROOT}/plan.cache
      - ${TF_ROOT}/plan.json
      - ${TF_ROOT}/.terraform.lock.hcl

.tofu-apply:
  extends: .tofu
  stage: deploy
  resource_group: state-$TF_STATE_NAME
  script:
    - tofu apply -lock-timeout=60s -auto-approve plan.cache

.tofu-destroy:
  extends: .tofu
  stage: cleanup
  resource_group: state-$TF_STATE_NAME
  script:
    - tofu destroy -lock-timeout=60s -auto-approve
```

`TF_STATE_NAME` задаёт вызывающий pipeline. Токен backend берётся из окружения текущей job, поэтому apply не использует уже истёкший токен plan-job. Каталог `.terraform` не передаём через cache. Планы передаём как artifacts с доступом Maintainer; `plan.json` здесь обычный JSON-артефакт, без обещания MR-widget на Free.

Образ закреплён на 1.13.1. `artifacts:access: maintainer` требует GitLab 18.4+; Servercore ставит актуальный пакет. Для более старого GitLab используйте поддерживаемое им ограничение доступа к artifacts. Отсутствие `.terraform.lock.hcl` у встроенного `terraform_data` даёт предупреждение при загрузке artifacts; файлы плана всё равно загружаются.

## Шаг 3. Главный pipeline: `.gitlab-ci.yml`

```yaml
include:
  - local: .gitlab-ci/opentofu.yml

stages: [validate, plan, deploy, cleanup]

variables:
  TF_STATE_NAME: tf-state

validate:
  extends: .tofu-validate

plan:
  extends: .tofu-plan

apply:
  extends: .tofu-apply
  needs:
    - job: plan
      artifacts: true
  rules:
    - if: '$CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH'
      when: manual
  allow_failure: false

destroy:
  extends: .tofu-destroy
  rules:
    - if: '$CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH'
      when: manual
  allow_failure: true
```

В **Build -> Pipeline editor -> Validate** проверьте объединённую конфигурацию. Затем:

```bash
git add .
git commit -m "ci: add OpenTofu state pipeline"
git push
```

На default branch выполните `apply`, затем проверьте **Operate -> Terraform states**: появится `tf-state`. Запустите `destroy` и проверьте обновлённый state. Если план старше суток, повторите plan-job перед apply. При ошибке stale plan создайте новый план.

## Самопроверка

- [ ] Job выполняется в образе OpenTofu, `tofu version` показывает 1.13.1.
- [ ] `validate` и `plan` зелёные; artifacts содержат `plan.cache` и `plan.json`.
- [ ] `apply` получает план через `needs`, требует ручного запуска и создаёт state.
- [ ] `destroy` удаляет ресурс и оставляет обновлённый state.
- [ ] Backend использует URL текущего GitLab и токен текущей job.

## Ссылки

- [GitLab-managed state](https://docs.gitlab.com/user/infrastructure/iac/terraform_state/)
- [Почему backend-пароль передают через окружение](https://docs.gitlab.com/user/infrastructure/iac/troubleshooting/)
- [OpenTofu releases](https://github.com/opentofu/opentofu/releases)
