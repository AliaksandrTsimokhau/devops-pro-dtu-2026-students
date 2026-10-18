# Lab: Continuous Deployment в 5 окружений через child pipelines

В этой лабораторной мы построим production-grade GitLab CI/CD pipeline, который автоматически разворачивает инфраструктуру в **пяти разных окружениях** с разной политикой триггеров — review (feature branches), integration (main), QA (main, ручной build), staging (tag), production (tag, ручной запуск).

Опираемся на статью GitLab: [Using child pipelines to continuously deploy to five environments](https://about.gitlab.com/blog/using-child-pipelines-to-continuously-deploy-to-five-environments/).

> ⚠️ **Шаблон `Terraform.gitlab-ci.yml` больше не опубликован GitLab.com** (проверено через templates API:
> в списке шаблонов его нет). Все `include: template: Terraform.gitlab-ci.yml` в этой лабе упадут
> с ошибкой include. Вместо него используйте **OpenTofu CI/CD component** из **GitLab CI/CD Catalog**
> (**проверь точное имя компонента и версию в Catalog**: Explore → CI/CD Catalog → поиск «OpenTofu»).
> Имя компонента здесь намеренно не указано — возьмите его из Catalog. У компонента другие имена
> jobs и inputs, поэтому переопределения jobs (`fmt`, `validate`, `build`, `deploy`, `destroy`,
> `.terraform:destroy`) в child-файлах придётся адаптировать под его документацию (**проверь:** список jobs и inputs компонента).
> Места, которые нужно поменять, помечены ⚠️ ниже.

> ⭐ **Premium/Ultimate.** Отдельные возможности помечены **⭐ Premium/Ultimate — проверь тариф**.
> Для каждой из них дан вариант для Free.

> ⚠️ Оригинальная статья деплоит ресурсы в **AWS**. У многих слушателей нет AWS-аккаунта.
> Поэтому в этой лабе есть **два варианта инфраструктуры**:
> - **Path A — AWS (как в оригинале):** VPC + Subnets + EC2.
> - **Path B — GitLab (альтернатива):** создаём 5 GitLab-проектов через `gitlab` provider — никаких внешних аккаунтов, всё внутри вашей GitLab-инсталляции.
>
> Pipeline-логика **идентична** в обоих вариантах. Различаются только Terraform-файлы.

## Что мы построим

| Окружение | Триггер | Lifecycle | Layer |
|---|---|---|---|
| `review/<branch>` | push в feature branch | auto-create, destroy по кнопке Stop (environment `on_stop`) | 1st |
| `integration` | push в `main` | auto-deploy, manual destroy | 1st |
| `staging` | git tag (`v*`) | auto-deploy, destroy вручную (см. Cleanup) | 1st |
| `qa` | push в `main` | ручной `build` (▶), затем автоматический `deploy`; destroy вручную | 2nd (grand-child) |
| `production` | git tag | ручной `deploy` (▶) | 2nd (grand-child) |

**Архитектура pipeline:**

```
.gitlab-ci.yml (parent — routing)
   ├─→ .first-layer.gitlab-ci.yml  (review / integration / staging — Terraform + deploy)
   │      └─→ .second-layer.gitlab-ci.yml  (qa / production — Terraform на отдельном state)
```

## Prerequisites

- GitLab **16+** (для современного `trigger:include` и `environment:on_stop`).
- Установленный `git` локально.
- Знание базы Terraform (`init / plan / apply`).
- Право создавать Personal Access Token в GitLab.
- **Path A:** AWS-аккаунт с IAM-юзером (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`) и правами на VPC/EC2.
- **Path B:** ничего дополнительного — используем сам GitLab.
- 60–90 минут.

---

## Шаг 1. Создаём parent-проект

1. В GitLab нажмите **New project → Create blank project**.
2. Имя: `five-envs-cd-lab`.
3. Visibility: `Private`.
4. Отметьте **Initialize repository with a README**.
5. Создайте проект.

Клонируйте:

```bash
git clone https://gitlab.example.com/<your-namespace>/five-envs-cd-lab.git
cd five-envs-cd-lab
```

Создайте структуру директорий:

```bash
mkdir -p .gitlab-ci terraform vars
touch .gitlab-ci.yml \
      .gitlab-ci/.first-layer.gitlab-ci.yml \
      .gitlab-ci/.second-layer.gitlab-ci.yml \
      terraform/main.tf \
      terraform/variables.tf \
      terraform/backend.tf \
      vars/review.tfvars \
      vars/integration.tfvars \
      vars/qa.tfvars \
      vars/staging.tfvars \
      vars/production.tfvars
```

---

## Шаг 2. Выберите Path — AWS или GitLab

### Path A — AWS (как в оригинале статьи)

**`terraform/main.tf`:**

```terraform
provider "aws" {
  region = var.aws_default_region
}

resource "aws_vpc" "main" {
  cidr_block = var.aws_vpc_cidr
  tags = {
    Name = var.aws_resources_name
  }
}

resource "aws_subnet" "public_subnet" {
  vpc_id     = aws_vpc.main.id
  cidr_block = var.aws_public_subnet_cidr
  tags = {
    Name = "Public Subnet"
  }
}

resource "aws_subnet" "private_subnet" {
  vpc_id     = aws_vpc.main.id
  cidr_block = var.aws_private_subnet_cidr
  tags = {
    Name = "Private Subnet"
  }
}

resource "aws_instance" "sandbox" {
  ami           = var.aws_ami_id
  instance_type = var.aws_instance_type
  subnet_id     = aws_subnet.public_subnet.id
  tags = {
    Name = var.aws_resources_name
  }
}
```

**`terraform/variables.tf`:**

```terraform
variable "aws_ami_id" {
  description = "The AMI ID of the image being deployed."
  type        = string
}

variable "aws_instance_type" {
  type    = string
  default = "t2.micro"
}

variable "aws_vpc_cidr" {
  type    = string
  default = "10.0.0.0/16"
}

variable "aws_public_subnet_cidr" {
  type    = string
  default = "10.0.1.0/24"
}

variable "aws_private_subnet_cidr" {
  type    = string
  default = "10.0.2.0/24"
}

variable "aws_default_region" {
  type    = string
  default = "eu-west-3"
}

variable "aws_resources_name" {
  type    = string
  default = "demo"
}
```

**`vars/review.tfvars`:**

```hcl
aws_vpc_cidr            = "10.1.0.0/16"
aws_public_subnet_cidr  = "10.1.1.0/24"
aws_private_subnet_cidr = "10.1.2.0/24"
```

> Заполните остальные `.tfvars` аналогично, меняя CIDR на `10.2.x.x`, `10.3.x.x`, `10.4.x.x`, `10.5.x.x` — чтобы окружения не пересекались.

**CI/CD-переменные** (Settings → CI/CD → Variables, masked):
- `AWS_ACCESS_KEY_ID`
- `AWS_SECRET_ACCESS_KEY`
- `AWS_DEFAULT_REGION` = `eu-west-3`
- `AWS_AMI_ID` = валидный AMI для региона (например, `ami-0a73e96a849c232cc` для Ubuntu в eu-west-3 — проверьте актуальный)
- `AWS_INSTANCE_TYPE` = `t2.micro`

→ **Переходите к шагу 3.**

---

### Path B — GitLab (альтернатива без AWS)

**Идея:** вместо облачных ресурсов будем создавать **5 GitLab-проектов** через `gitlab` Terraform provider. Каждое «окружение» = отдельный демонстрационный проект внутри вашей группы. Имя проекта задаётся **на каждое окружение** (`demo-<env>`), иначе все окружения попытаются создать проект `demo` в одной группе и упадут с коллизией имён.

#### Подготовка

1. Создайте **группу-песочницу**, например `five-envs-sandbox`.
2. Зафиксируйте её **Group ID** (видно на странице группы под именем).
3. Создайте **Personal Access Token** (Settings → Access tokens):
   - Scope: `api`, `write_repository`.
   - TTL: на длину лабораторной.
   - Сохраните токен.

**`terraform/main.tf`:**

```terraform
terraform {
  required_providers {
    gitlab = {
      source  = "gitlabhq/gitlab"
      version = "~> 17.0"
    }
  }
}

provider "gitlab" {
  token    = var.gitlab_token
  base_url = var.gitlab_base_url
}

resource "gitlab_project" "demo" {
  name                   = var.resource_name
  namespace_id           = var.gitlab_namespace_id
  description            = "Demo project for environment: ${var.environment_label}"
  visibility_level       = var.project_visibility
  initialize_with_readme = true
  default_branch         = "main"
}

resource "gitlab_project_variable" "env_label" {
  project = gitlab_project.demo.id
  key     = "ENVIRONMENT"
  value   = var.environment_label
}

resource "gitlab_project_variable" "deployed_at" {
  project = gitlab_project.demo.id
  key     = "DEPLOYED_FROM_PIPELINE"
  value   = var.source_pipeline_id
}
```

**`terraform/variables.tf`:**

```terraform
variable "gitlab_token" {
  description = "GitLab Personal Access Token with api scope"
  type        = string
  sensitive   = true
}

variable "gitlab_base_url" {
  type    = string
  default = "https://gitlab.com/api/v4/"
}

variable "gitlab_namespace_id" {
  description = "Numeric group ID where demo projects will be created"
  type        = number
}

variable "resource_name" {
  description = "Project slug (becomes the demo repo name)"
  type        = string
  default     = "demo"
}

variable "environment_label" {
  description = "Logical environment name stored as project variable"
  type        = string
  default     = "review"
}

variable "project_visibility" {
  type    = string
  default = "private"
}

variable "source_pipeline_id" {
  description = "ID of pipeline that created the project (audit trail)"
  type        = string
  default     = ""
}
```

**`vars/review.tfvars`:**

```hcl
project_visibility = "private"
```

**`vars/integration.tfvars`:**

```hcl
project_visibility = "private"
```

**`vars/qa.tfvars`:**

```hcl
project_visibility = "private"
```

**`vars/staging.tfvars`:**

```hcl
project_visibility = "private"
```

**`vars/production.tfvars`:**

```hcl
project_visibility = "private"
```

> `visibility = internal` доступна только в self-hosted GitLab, поэтому везде используем `private`.

**CI/CD-переменные** (Settings → CI/CD → Variables):
- `TF_VAR_gitlab_token` — ваш PAT (masked). **Не отмечайте `protected`**: защищённая переменная не передаётся в feature-ветки и не-защищённые теги, а review и staging работают именно там. Для лабы оставьте её **unprotected**.
- `TF_VAR_gitlab_namespace_id` — group ID песочницы.

> ⚠️ **Не задавайте** `TF_VAR_resource_name` как переменную проекта: переменная проекта перекроет
> имя, которое задаёт trigger-job для каждого окружения (**проверь:** порядок приоритета переменных
> в вашей версии GitLab). Имя задаётся в YAML (Шаг 4 и Шаг 6).

> Названия `TF_VAR_*` — это стандартный механизм Terraform: переменные окружения с этим префиксом автоматически становятся `var.*`.

→ **Переходите к шагу 3.**

---

## Шаг 3. Общий backend и шаблон Terraform job

Для обоих Path используем **GitLab-managed Terraform state** — встроенный HTTP backend.

**`terraform/backend.tf`** (для обоих Path):

```terraform
terraform {
  backend "http" {
  }
}
```

Параметры backend (URL, токен) подставит GitLab template `Terraform.gitlab-ci.yml`, который мы подключим в child pipeline.

> ⚠️ **Этот шаблон больше не доступен на GitLab.com.** Замените его на OpenTofu CI/CD component из Catalog
> (см. предупреждение в начале лабы). Backend-параметры компонент подставит сам — **проверь** в его документации.

---

## Шаг 4. Parent pipeline (`.gitlab-ci.yml`)

Это **контроллер** — он не выполняет Terraform сам, а триггерит child pipeline под нужное окружение.

```yaml
stages:
  - test
  - environments

.environment:
  stage: environments
  variables:
    TF_ROOT: terraform
    TF_CLI_ARGS_plan: "-var-file=../vars/$variables_file.tfvars"
  trigger:
    include: .gitlab-ci/.first-layer.gitlab-ci.yml
    strategy: depend
    forward:
      yaml_variables: true
      pipeline_variables: true

review:
  extends: .environment
  variables:
    environment: review/$CI_COMMIT_REF_SLUG
    TF_STATE_NAME: $CI_COMMIT_REF_SLUG
    variables_file: review
    TF_VAR_aws_resources_name: $CI_COMMIT_REF_SLUG     # Path A
    TF_VAR_environment_label: review-$CI_COMMIT_REF_SLUG # Path B
    TF_VAR_resource_name: demo-review-$CI_COMMIT_REF_SLUG # Path B: уникальное имя проекта
  rules:
    - if: $CI_COMMIT_BRANCH && $CI_COMMIT_BRANCH != $CI_DEFAULT_BRANCH

integration:
  extends: .environment
  variables:
    environment: integration
    TF_STATE_NAME: $environment
    variables_file: $environment
    TF_VAR_resource_name: demo-integration               # Path B
  rules:
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH

staging:
  extends: .environment
  variables:
    environment: staging
    TF_STATE_NAME: $environment
    variables_file: $environment
    TF_VAR_resource_name: demo-staging                   # Path B
  rules:
    - if: $CI_COMMIT_TAG

include:
  - template: Security/SAST-IaC.gitlab-ci.yml
```

> Используйте либо строки с `TF_VAR_aws_resources_name`, либо с `TF_VAR_environment_label` — в зависимости от Path. Закомментируйте лишнее. Строку `TF_VAR_resource_name` оставляйте только для Path B.

**Ключевые моменты:**
- `.environment` — anchor-template для всех environment-jobs.
- `trigger: include:` + `strategy: depend` — parent **ждёт** окончания child.
- `forward:` — пробрасывает все YAML и pipeline variables вниз.
- `rules:` — определяют, кто и когда триггерится:
  - review: любая ветка кроме main.
  - integration: main.
  - staging: любой tag.

---

## Шаг 5. First-layer child (`.gitlab-ci/.first-layer.gitlab-ci.yml`)

Этот pipeline делает реальную работу: Terraform `plan / apply / destroy` плюс security scan.

```yaml
variables:
  TF_VAR_aws_ami_id: $AWS_AMI_ID
  TF_VAR_aws_instance_type: $AWS_INSTANCE_TYPE
  TF_VAR_aws_default_region: $AWS_DEFAULT_REGION

include:
  - template: Terraform.gitlab-ci.yml        # ⚠️ шаблон удалён с GitLab.com: замените на OpenTofu CI/CD component из Catalog (проверь имя и версию)

default:
  cache:
    - key: cache-$CI_COMMIT_REF_SLUG
      fallback_keys:
        - cache-$CI_DEFAULT_BRANCH
      paths:
        - .

stages:
  - validate
  - test
  - build
  - deploy
  - cleanup
  - 2nd_layer

fmt:
  rules:
    - when: always

validate:
  rules:
    - when: always

kics-iac-sast:
  rules:
    - if: $SAST_DISABLED == 'true' || $SAST_DISABLED == '1'
      when: never
    - if: $SAST_EXCLUDED_ANALYZERS =~ /kics/
      when: never
    - when: on_success

iac-sast:
  rules:
    - if: $SAST_DISABLED == 'true' || $SAST_DISABLED == '1'
      when: never
    - if: $SAST_EXCLUDED_ANALYZERS =~ /kics/
      when: never
    - when: on_success

build:
  environment:
    name: $TF_STATE_NAME
    action: prepare
  rules:
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH
    - if: $CI_COMMIT_TAG

deploy:
  environment:
    name: $TF_STATE_NAME
    action: start
    on_stop: destroy
  rules:
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH
    - if: $CI_COMMIT_TAG

destroy:
  extends: .terraform:destroy                # ⚠️ имя jobs из шаблона; у OpenTofu-компонента — свои (проверь)
  variables:
    GIT_STRATEGY: none
  dependencies:
    - build
  environment:
    name: $TF_STATE_NAME
    action: stop
  rules:
    - if: $CI_COMMIT_TAG
      when: never
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH && $TF_DESTROY == "true"
      when: manual

build_review:
  extends: build
  rules:
    - if: $CI_COMMIT_TAG
      when: never
    - if: $CI_COMMIT_BRANCH != $CI_DEFAULT_BRANCH
      when: on_success

deploy_review:
  extends: deploy
  dependencies:
    - build_review
  environment:
    name: $environment
    action: start
    on_stop: destroy_review
  rules:
    - if: $CI_COMMIT_TAG
      when: never
    - if: $CI_COMMIT_BRANCH != $CI_DEFAULT_BRANCH
      when: on_success

destroy_review:
  extends: destroy
  dependencies:
    - build_review
  environment:
    name: $environment
    action: stop
  rules:
    - if: $CI_COMMIT_TAG
      when: never
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH
      when: never
    - when: manual

.2nd_layer:
  stage: 2nd_layer
  variables:
    TF_ROOT: terraform
  trigger:
    include: .gitlab-ci/.second-layer.gitlab-ci.yml
    forward:
      yaml_variables: true
      pipeline_variables: true

qa:
  extends: .2nd_layer
  variables:
    TF_STATE_NAME_2: qa
    environment: $TF_STATE_NAME_2
    TF_CLI_ARGS_plan_2: "-var-file=../vars/$TF_STATE_NAME_2.tfvars"
    TF_VAR_resource_name: demo-qa                       # Path B
  rules:
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH

production:
  extends: .2nd_layer
  variables:
    TF_STATE_NAME_2: production
    environment: $TF_STATE_NAME_2
    TF_CLI_ARGS_plan_2: "-var-file=../vars/$TF_STATE_NAME_2.tfvars"
    TF_VAR_resource_name: demo-production               # Path B
  rules:
    - if: $CI_COMMIT_TAG
```

**Что делает каждый job:**
- `fmt`, `validate` — синтаксические проверки Terraform.
- `kics-iac-sast` / `iac-sast` — security scan IaC (KICS + native GitLab analyzer).
- `build` / `deploy` — для main и tag: prepare state + `terraform apply`.
- `build_review` / `deploy_review` — для feature branches.
- `destroy` / `destroy_review` — `terraform destroy` через `on_stop`.
- `.2nd_layer` — триггерит grand-child для qa и production.

> **Path B nuance:** статья изначально написана под AWS, поэтому переменные `TF_VAR_aws_*` остались в child. Если идёте Path B — эти строки безвредны (они просто не используются), либо удалите их из `variables:` блока.
>
> **Staging не имеет destroy-job** в этом файле: его ресурсы удаляются в Cleanup (Шаг 13).

---

## Шаг 6. Second-layer grand-child (`.gitlab-ci/.second-layer.gitlab-ci.yml`)

Отдельный pipeline для **production** и **qa** — с независимым Terraform state. Для production запуск `deploy` вручную.

```yaml
include:
  template: Terraform.gitlab-ci.yml          # ⚠️ шаблон удалён с GitLab.com: замените на OpenTofu CI/CD component из Catalog (проверь имя и версию)

stages:
  - validate
  - test
  - build
  - deploy

fmt:
  rules:
    - when: never

validate:
  rules:
    - when: never

kics-iac-sast:
  rules:
    - if: $SAST_DISABLED == 'true' || $SAST_DISABLED == '1'
      when: never
    - if: $SAST_EXCLUDED_ANALYZERS =~ /kics/
      when: never
    - when: always

build:
  cache:
    - key: $TF_STATE_NAME_2
      fallback_keys:
        - cache-$CI_DEFAULT_BRANCH
      paths:
        - .
  environment:
    name: $TF_STATE_NAME_2
    action: prepare
  before_script:
    - TF_STATE_NAME=$TF_STATE_NAME_2
    - TF_CLI_ARGS_plan=$TF_CLI_ARGS_plan_2
  rules:
    - when: manual

deploy:
  cache:
    - key: $TF_STATE_NAME_2
      fallback_keys:
        - cache-$CI_DEFAULT_BRANCH
      paths:
        - .
  environment:
    name: $TF_STATE_NAME_2
    action: start
  before_script:
    - TF_STATE_NAME=$TF_STATE_NAME_2
    - TF_CLI_ARGS_plan=$TF_CLI_ARGS_plan_2
  rules:
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH
    - if: $CI_COMMIT_TAG && $TF_AUTO_DEPLOY == "true"
    - if: $CI_COMMIT_TAG
      when: manual
```

**Ключевое:**
- `fmt` и `validate` отключены — мы уже их запускали в first-layer.
- `TF_STATE_NAME_2` переопределяет `TF_STATE_NAME` в `before_script` → отдельный state файл.
- **Фактическое поведение QA:** на `main` `build` в grand-child — **ручной** (▶), после его запуска `deploy` выполняется **автоматически** (правило без `when:`).
- **Production:** `build` ручной; `deploy` на теге — `when: manual` (если не задан `TF_AUTO_DEPLOY=true`), на `main` — автоматически.
- `TF_CLI_ARGS_plan` в `before_script` второго уровня — тот же механизм, что и в first-layer; **проверь** после замены шаблона на OpenTofu-компонент, что переменная `-var-file` применяется.

---

## Шаг 7. CODEOWNERS — governance

⭐ **Premium/Ultimate — проверь тариф.** Code-owner approval (обязательное одобрение владельца файла) — функция Premium.

Защищаем критичные файлы — изменения требуют approval от ответственных:

**`CODEOWNERS`:**

```
vars/production.tfvars  @operations-group
vars/staging.tfvars     @<your-username> @operations-group
CODEOWNERS              @<your-username>
```

> Замените `@operations-group` и `@<your-username>` на реальных. Группа должна существовать.

Дальше в **Settings → Repository → Protected branches**:
- `main` — Allowed to merge: Maintainers, Allowed to push: No one (это **Free**: защита ветки доступна без тарифа).
- ⭐ В **Merge request approvals** — добавьте `vars/` пути как требующие code owner approval (**Premium**, **проверь тариф**).

**Free-вариант:** без CODEOWNERS-approval защищайте `vars/*.tfvars` через правило «только maintainers может мержить в `main`» и ревью в MR вручную.

---

## Шаг 8. Защищаем environments

⭐ **Premium — проверь тариф.** Protected environments доступны не на Free.

**Settings → CI/CD → Protected environments:**
- `production` — Allowed to deploy: maintainers группы Operations.
- `staging` — Allowed to deploy: maintainers.

Это предотвращает деплой в prod через PAT обычного разработчика.

**Free-вариант:** для `production` оставьте `when: manual` в YAML (Шаг 6) и ограничьте, кто может пушить теги (Settings → Repository → Protected tags, **Free**). Это не согласование, а только кнопка и правило на тегах.

---

## Шаг 9. Запускаем review flow

```bash
git checkout -b feature/test-deploy
echo "# Adding feature" >> README.md
git add .
git commit -m "feature: test review deploy"
git push origin feature/test-deploy
```

**Что должно произойти:**

1. Parent pipeline стартует.
2. `review` job триггерится → first-layer pipeline.
3. В first-layer: `fmt` → `validate` → `kics-iac-sast` → `build_review` → `deploy_review`.
4. **Path A:** в AWS console появляется VPC с tag `feature-test-deploy`.
5. **Path B:** в вашей группе появляется проект `demo-review-feature-test-deploy` с переменной `ENVIRONMENT=review-feature-test-deploy`.
6. В GitLab UI **Operate → Environments** виден `review/feature-test-deploy` в состоянии **Active**.
7. Результаты KICS: в **Security tab MR** — ⭐ **Ultimate**; на Free смотрите лог job `kics-iac-sast` и артефакты.

**Cleanup:**
- Откройте environment → нажмите кнопку **Stop** → запустится `destroy_review`.
- Ресурсы удалятся, environment в **Stopped**.

---

## Шаг 10. Запускаем integration flow

Замерджите MR в `main` (или прямо запушьте, если main не защищён):

```bash
git checkout main
git pull
git merge feature/test-deploy
git push origin main
```

**Что должно произойти:**

1. Parent → `integration` job → first-layer.
2. `build` → `deploy` (без manual — auto на main).
3. **Path A:** новая VPC с tag `demo`, отдельный state `integration`.
4. **Path B:** новый проект `demo-integration` с visibility `private` в группе.
5. Environment `integration` в **Active**.
6. Параллельно стартует `qa` (через `.2nd_layer`): его `build` ждёт **ручного запуска** (▶), после него `deploy` выполняется автоматически.

**Чтобы задеплоить QA:** Build → Pipelines → найдите grand-child → нажмите ▶ у `build`; `deploy` запустится после него.

---

## Шаг 11. Запускаем staging + production через tag

```bash
git tag v1.0.0
git push origin v1.0.0
```

**Что должно произойти:**

1. Parent → `staging` job (`$CI_COMMIT_TAG`) → first-layer.
2. Staging auto-deploy.
3. Также стартует `production` job → grand-child.
4. В grand-child: `build` для production ждёт нажатия ▶, `deploy` на теге — `when: manual` — **ждёт нажатия кнопки**.

**Чтобы выпустить в prod:** maintainer группы Operations нажимает ▶ у production `build` и затем у `deploy`.

---

## Шаг 12. Self-check

- [ ] Parent pipeline корректно роутит триггеры по веткам/тегам.
- [ ] First-layer создаёт review-окружение на feature branch.
- [ ] Review-окружение можно остановить через UI → ресурсы destroyed.
- [ ] Integration auto-deploy при push в main.
- [ ] Staging auto-deploy при push tag'а.
- [ ] QA — отдельный state, ручной `build`, затем автоматический `deploy`.
- [ ] Production — отдельный state, требует tag + ручной запуск.
- [ ] KICS IaC scan запускается (лог job; в Security tab MR — ⭐ Ultimate).
- [ ] CODEOWNERS защищает `vars/production.tfvars` (⭐ Premium — проверь тариф; на Free — ручное ревью).
- [ ] Path A: ресурсы создаются в правильных AWS-аккаунтах и регионах.
- [ ] Path B: 5 demo-проектов (`demo-review-…`, `demo-integration`, `demo-staging`, `demo-qa`, `demo-production`) появились в sandbox-группе с правильными `ENVIRONMENT` переменными.

---

## Шаг 13. Cleanup

Перед очисткой **отзовите PAT**, если он больше не нужен: User Settings → Access tokens → Revoke.

**Path A — AWS.** Ресурсы review и integration удаляйте через UI (Stop / manual `destroy`). Для staging, qa и production destroy-job в YAML нет, поэтому удаляйте локально тем же backend и тем же state, что использовал pipeline:

```bash
cd terraform
# state-имя — как в pipeline: integration, staging, qa или production (для review — имя ветки)
export TF_STATE_NAME=staging
terraform init \
  -backend-config="address=https://gitlab.example.com/api/v4/projects/<PROJECT_ID>/terraform/state/${TF_STATE_NAME}" \
  -backend-config="lock_address=https://gitlab.example.com/api/v4/projects/<PROJECT_ID>/terraform/state/${TF_STATE_NAME}/lock" \
  -backend-config="unlock_address=https://gitlab.example.com/api/v4/projects/<PROJECT_ID>/terraform/state/${TF_STATE_NAME}/lock" \
  -backend-config="username=<ваш-логин>" \
  -backend-config="password=<PAT с scope api>" \
  -backend-config="lock_method=POST" \
  -backend-config="unlock_method=DELETE"
terraform destroy -var-file=../vars/staging.tfvars -var "aws_ami_id=<AMI>" -auto-approve
```

- `CI_JOB_TOKEN` здесь не подходит: он действует только внутри job'а CI. Нужен PAT.
- Адреса `lock_address`/`unlock_address` и методы — **проверь** по документации GitLab по Terraform state (HTTP backend) для вашей версии.
- Переменные из `-var` (AMI, токен и т.п.) должны совпадать с теми, что использовал pipeline; у `aws_ami_id` нет default.

**Path B — GitLab.** Самый простой путь — **удалить песочницу целиком**: откройте группу `five-envs-sandbox` → Settings → General → Advanced → Delete group. Все demo-проекты удалятся вместе с ней.

Частичная очистка: review-окружения останавливаются через **Stop** в UI; `integration` — manual `destroy` (если задан `TF_DESTROY=true`). Для staging, qa и production используйте локальный `terraform destroy` по образцу Path A, либо удалите соответствующий проект `demo-<env>` вручную.

> ⚠️ Не забудьте: AWS-ресурсы тарифицируются. EC2 t2.micro попадает под free tier, но VPC + NAT (если добавите) — нет.

---

## Что дальше

- **Application code deploy** — мы деплоили только инфраструктуру. Следующий шаг — добавить `compile/test/build` стадии для самого приложения, и `deploy` контейнера в созданную VPC/проект.
- **CI/CD Components вместо local include** — оборачиваем first-layer и second-layer в reusable component, публикуем в GitLab CI/CD Catalog. Другие проекты подключают одной строкой (см. лабу `gitlab_ci_lab_component.md`).
- **Pipeline schedules** — nightly destroy всех review-окружений → экономия.
- ⭐ **Approvals as code** — Compliance Framework, чтобы production deploy требовал N approvals из конкретного списка (**Premium/Ultimate — проверь тариф**).
- ⭐ **Merge train** — для команды 10+ человек: serialized deploy на main, чтобы избежать race conditions (**Premium — проверь тариф**).

### Полезные ссылки

- [Original article](https://about.gitlab.com/blog/using-child-pipelines-to-continuously-deploy-to-five-environments/)
- [GitLab Terraform integration](https://docs.gitlab.com/ee/user/infrastructure/iac/)
- [GitLab CI/CD Catalog](https://docs.gitlab.com/ee/ci/components/#cicd-catalog) — OpenTofu-компонент (**проверь имя**)
- [GitLab Terraform provider docs](https://registry.terraform.io/providers/gitlabhq/gitlab/latest/docs)
- [AWS provider docs](https://registry.terraform.io/providers/hashicorp/aws/latest/docs)
- [`trigger:` keyword reference](https://docs.gitlab.com/ee/ci/yaml/index.html#trigger)
- [`environment:` keyword reference](https://docs.gitlab.com/ee/ci/yaml/index.html#environment)
- [CODEOWNERS](https://docs.gitlab.com/ee/user/project/codeowners/)
