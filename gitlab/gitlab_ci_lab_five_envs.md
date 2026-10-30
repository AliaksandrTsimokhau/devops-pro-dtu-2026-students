# Lab: Continuous Deployment в 5 окружений через child pipelines

Построим parent -> child -> grand-child pipeline с отдельными OpenTofu state для review, integration, QA, staging и production. На Servercore GitLab CE используем Path B: каждое окружение создаёт проект в группе-песочнице. Path A оставляет вариант с AWS.

**Требуется:** GitLab 18.4+, Maintainer в CI-проекте, Owner группы-песочницы для Path B, container executor и пройденная [лаба по state](gitlab_ci_lab_tf.md). На Servercore сначала выполните [подготовку](servercore_gitlab.md). Время: 60-90 минут. Job-ам нужен доступ к `ghcr.io` (образ OpenTofu), к GitLab API и к `registry.opentofu.org` (провайдер `gitlabhq/gitlab`).

| Окружение | Триггер | Plan / apply | State |
|---|---|---|---|
| `review/<branch>` | feature branch | auto / auto | `review-<slug>` |
| `integration` | default branch | auto / auto | `integration` |
| `qa` | grand-child из integration | manual / auto | `qa` |
| `staging` | tag | auto / auto | `staging` |
| `production` | grand-child из staging | manual / manual | `production` |

Manual job - кнопка запуска. На CE ограничьте push в default branch и создание release-тегов. Обязательные CODEOWNERS approvals и protected environments требуют соответствующего платного тарифа; эта лаба от них не зависит.

## Шаг 1. Проект и общие jobs

Создайте private-проект `five-envs-cd-lab` с README и склонируйте URL из UI. Создайте:

```bash
mkdir -p terraform vars .gitlab-ci
```

Скопируйте общий файл `.gitlab-ci/opentofu.yml` целиком из [шага 2 лабы по state](gitlab_ci_lab_tf.md#шаг-2-общие-jobs-gitlab-ciopentofuyml). В нём закреплён OpenTofu 1.13.1 и описаны `.tofu-validate`, `.tofu-plan`, `.tofu-apply`, `.tofu-destroy`. Все jobs выполняют свой `init`, используют токен backend текущей job и передают plan через artifacts.

В `terraform/backend.tf`:

```hcl
terraform {
  required_version = ">= 1.13.1, < 2.0"
  backend "http" {}
}
```

В `.gitignore` добавьте:

```gitignore
**/.terraform/
**/plan.cache
**/plan.json
*.tfstate
*.tfstate.*
```

## Шаг 2. Инфраструктура: выберите один Path

### Path B: проекты GitLab, без облачного аккаунта

Создайте отдельную группу `five-envs-sandbox`. Запишите её ID. Создайте PAT со scope `api` и сроком на время лабы. В CI/CD variables родительского проекта задайте:

- `TF_VAR_gitlab_token`: PAT, masked, unprotected для feature branches этой учебной песочницы.
- `TF_VAR_gitlab_namespace_id`: ID группы.
- `TF_VAR_gitlab_base_url`: **URL вашего GitLab с `/api/v4/`**, например `https://gitlab.lab.example.com/api/v4/`. На GitLab.com: `https://gitlab.com/api/v4/`.

Давайте доступ к проекту только участникам лабы: unprotected PAT доступен коду их веток. Удалите его после destroy. Значение PAT попадает в сохранённый план и `plan.json` (artifacts доступны только Maintainer и живут сутки), поэтому не открывайте эти файлы посторонним. Не задавайте CI/CD variables `TF_STATE_NAME`, `TF_VAR_resource_name` и `TF_VAR_environment_label`: их задают pipeline-файлы для каждого окружения.

`terraform/main.tf`:

```hcl
terraform {
  required_providers {
    gitlab = {
      source  = "gitlabhq/gitlab"
      version = "~> 17.0" # проверьте совместимость версии провайдера с версией GitLab стенда
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
  description            = "Demo environment: ${var.environment_label}"
  visibility_level       = "private"
  initialize_with_readme = true
  default_branch         = "main"
}

resource "gitlab_project_variable" "env_label" {
  project = gitlab_project.demo.id
  key     = "ENVIRONMENT"
  value   = var.environment_label
}
```

`terraform/variables.tf`:

```hcl
variable "gitlab_token" {
  type      = string
  sensitive = true
}
variable "gitlab_namespace_id" {
  type = number
}
variable "gitlab_base_url" {
  type = string
}
variable "resource_name" {
  type = string
}
variable "environment_label" {
  type = string
}
```

Создайте пять пустых файлов `vars/review.tfvars`, `vars/integration.tfvars`, `vars/qa.tfvars`, `vars/staging.tfvars`, `vars/production.tfvars`. Здесь значения приходят через `TF_VAR_*`; файлы оставлены для следующего упражнения с различиями окружений.

### Path A: AWS

Вместо файлов Path B используйте этот `terraform/main.tf`:

```hcl
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }
}
provider "aws" {
  region = var.aws_region
}
resource "aws_vpc" "main" {
  cidr_block = var.vpc_cidr
  tags = {
    Name = var.resource_name
  }
}
resource "aws_subnet" "public" {
  vpc_id     = aws_vpc.main.id
  cidr_block = var.public_cidr
}
resource "aws_subnet" "private" {
  vpc_id     = aws_vpc.main.id
  cidr_block = var.private_cidr
}
resource "aws_instance" "sandbox" {
  ami           = var.ami_id
  instance_type = "t3.micro"
  subnet_id     = aws_subnet.public.id
  tags = {
    Name = var.resource_name
  }
}
```

`terraform/variables.tf`:

```hcl
variable "aws_region" {
  type    = string
  default = "eu-west-3"
}
variable "ami_id" {
  type = string
}
variable "vpc_cidr" {
  type = string
}
variable "public_cidr" {
  type = string
}
variable "private_cidr" {
  type = string
}
variable "resource_name" {
  type = string
}
```

CI/CD variables: `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` и `TF_VAR_ami_id` (валидный AMI для региона). Для временных AWS credentials добавьте `AWS_SESSION_TOKEN`. Нужны права на VPC, subnets и EC2. Ресурсы тарифицируются; удалите их до отзыва credentials. Это пример создания EC2, без Internet Gateway и публичного SSH-доступа к ней.

В `vars/review.tfvars`:

```hcl
vpc_cidr     = "10.1.0.0/16"
public_cidr  = "10.1.1.0/24"
private_cidr = "10.1.2.0/24"
```

Для integration, qa, staging, production используйте соответственно `10.2`, `10.3`, `10.4`, `10.5` вместо `10.1` во всех трёх CIDR.

Для обоих Path: выполните `tofu -chdir=terraform init -backend=false` локально, затем `tofu -chdir=terraform fmt` и затем `tofu -chdir=terraform providers lock -platform=linux_amd64 -platform=linux_arm64 -platform=darwin_arm64` и закоммитьте `.terraform.lock.hcl`: без этого lock-файл содержит хэши только вашей платформы, и `init` в CI на linux может отказать. Используйте ту же версию OpenTofu, что в CI.

## Шаг 3. Parent: `.gitlab-ci.yml`

```yaml
stages: [environments]

.environment:
  stage: environments
  trigger:
    include: .gitlab-ci/first-layer.yml
    strategy: depend
    forward:
      yaml_variables: true
      pipeline_variables: false

review:
  extends: .environment
  variables:
    TF_STATE_NAME: review-$CI_COMMIT_REF_SLUG
    TF_ENVIRONMENT: review/$CI_COMMIT_REF_SLUG
    TF_VAR_resource_name: demo-review-$CI_COMMIT_REF_SLUG
    TF_VAR_environment_label: review-$CI_COMMIT_REF_SLUG
    TF_CLI_ARGS_plan: -var-file=../vars/review.tfvars
    TF_CLI_ARGS_destroy: -var-file=../vars/review.tfvars
  rules:
    - if: '$CI_COMMIT_BRANCH && $CI_COMMIT_BRANCH != $CI_DEFAULT_BRANCH'

integration:
  extends: .environment
  variables:
    TF_STATE_NAME: integration
    TF_ENVIRONMENT: integration
    TF_VAR_resource_name: demo-integration
    TF_VAR_environment_label: integration
    TF_CLI_ARGS_plan: -var-file=../vars/integration.tfvars
    TF_CLI_ARGS_destroy: -var-file=../vars/integration.tfvars
  rules:
    - if: '$CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH'

staging:
  extends: .environment
  variables:
    TF_STATE_NAME: staging
    TF_ENVIRONMENT: staging
    TF_VAR_resource_name: demo-staging
    TF_VAR_environment_label: staging
    TF_CLI_ARGS_plan: -var-file=../vars/staging.tfvars
    TF_CLI_ARGS_destroy: -var-file=../vars/staging.tfvars
  rules:
    - if: '$CI_COMMIT_TAG'
```

Path A просто не использует `TF_VAR_environment_label`; `TF_VAR_resource_name` задаёт AWS tags. Не передаём произвольные pipeline variables вниз: state и имя ресурса определены в YAML.

## Шаг 4. Child: `.gitlab-ci/first-layer.yml`

```yaml
include:
  - local: .gitlab-ci/opentofu.yml

stages: [validate, plan, deploy, cleanup, promote]

validate:
  extends: .tofu-validate

plan:
  extends: .tofu-plan

apply:
  extends: .tofu-apply
  needs:
    - job: plan
      artifacts: true
  environment:
    name: $TF_ENVIRONMENT
    on_stop: destroy

destroy:
  extends: .tofu-destroy
  needs: []
  when: manual
  allow_failure: true
  environment:
    name: $TF_ENVIRONMENT
    action: stop

.next-layer:
  stage: promote
  trigger:
    include: .gitlab-ci/second-layer.yml
    strategy: depend
    forward:
      yaml_variables: true
      pipeline_variables: false

qa:
  extends: .next-layer
  variables:
    NEXT_ENVIRONMENT: qa
    NEXT_RESOURCE_NAME: demo-qa
  rules:
    - if: '$CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH'

production:
  extends: .next-layer
  variables:
    NEXT_ENVIRONMENT: production
    NEXT_RESOURCE_NAME: demo-production
  rules:
    - if: '$CI_COMMIT_TAG'
```

`destroy` есть у всех first-layer окружений, включая staging. Он получает исходники и заново делает init; `GIT_STRATEGY: none` здесь не подходит. `needs: []` позволяет вызвать Stop независимо от ручного гейта QA/production.

## Шаг 5. Grand-child: `.gitlab-ci/second-layer.yml`

```yaml
include:
  - local: .gitlab-ci/opentofu.yml

stages: [validate, plan, deploy, cleanup]

variables:
  TF_STATE_NAME: $NEXT_ENVIRONMENT
  TF_VAR_resource_name: $NEXT_RESOURCE_NAME
  TF_VAR_environment_label: $NEXT_ENVIRONMENT
  TF_CLI_ARGS_plan: -var-file=../vars/$NEXT_ENVIRONMENT.tfvars
  TF_CLI_ARGS_destroy: -var-file=../vars/$NEXT_ENVIRONMENT.tfvars

validate:
  extends: .tofu-validate

plan:
  extends: .tofu-plan
  when: manual
  allow_failure: false

apply:
  extends: .tofu-apply
  needs:
    - job: plan
      artifacts: true
  environment:
    name: $NEXT_ENVIRONMENT
    on_stop: destroy
  rules:
    - if: '$CI_COMMIT_TAG'
      when: manual
    - if: '$CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH'
  allow_failure: false

destroy:
  extends: .tofu-destroy
  needs: []
  when: manual
  allow_failure: true
  environment:
    name: $NEXT_ENVIRONMENT
    action: stop
```

На втором переходе передаются только `NEXT_*`. Переменные state первого child не пересылаются повторно: QA и production получают собственные state и имена проектов. Проектные CI/CD variables с PAT и URL доступны каждому pipeline этого проекта.

## Шаг 6. Запуск и проверка

1. Проверьте YAML в **Build -> Pipeline editor -> Validate** на вашей версии GitLab. Закоммитьте все файлы.
2. Push в feature branch создаёт `review/<slug>`. В Path B появляется `demo-review-<slug>` с правильным `ENVIRONMENT`. Нажмите Stop: проект удалится.
3. Merge в default branch создаёт integration. Откройте grand-child QA, запустите `plan`: затем apply выполняется автоматически.
4. Push тега `v1.0.0` создаёт staging. В production grand-child нажмите сначала `plan`, потом `apply`.
5. В **Operate -> Terraform states** должны быть разные state: review, integration, qa, staging, production. Проверьте labels у всех пяти проектов Path B.

Если план старше суток или другой pipeline успел изменить state, повторите plan перед apply. Backend lock и `resource_group` сериализуют операции с одним state, но не делают старый план актуальным.

## Шаг 7. Governance и security scan

На Free запретите прямой push в default branch, разрешите merge только Maintainers и защитите release-теги. Ручной apply сам по себе не заменяет approvals.

На Premium/Ultimate можно добавить `CODEOWNERS`, обязательное code-owner approval и protected environments для production. Пример `CODEOWNERS`:

```text
vars/production.tfvars @operations-group
.gitlab-ci/ @operations-group
```

Замените группу на существующую и включите требование approval для защищённой ветки. Для IaC security scan используйте отдельную [DevSecOps-лабу](gitlab_ci_lab_devsecops.md); добавляйте сканер после успешного прохождения основного pipeline.

## Шаг 8. Cleanup

**Сначала destroy, затем отзыв PAT/AWS credentials.** Для каждого active environment нажмите Stop или запустите его `destroy` в соответствующем child pipeline. Включая qa, staging и production: у них теперь есть собственные destroy-jobs. Для review, integration и staging `destroy` находится в first-layer child pipeline; для qa и production в их grand-child pipeline (откройте bridge-job `qa` или `production`, затем **Downstream pipeline**).

Проверьте отсутствие demo-проектов Path B или ресурсов AWS Path A. Только затем отзовите PAT/credentials и удалите CI/CD variables. State-файлы можно удалить через **Operate -> Terraform states** после проверки, что ресурсы удалены. Удаление одного state не удаляет инфраструктуру.

Для Path B допустимо удалить всю отдельную группу-песочницу через UI, если pipeline недоступен. После этого state не соответствует ресурсам: не запускайте старый apply.

## Самопроверка

- [ ] Review создаётся на feature branch и удаляется через Stop.
- [ ] Integration выполняется автоматически на default branch.
- [ ] QA ждёт plan, затем выполняет apply автоматически.
- [ ] Staging выполняется на теге, production ждёт plan и apply.
- [ ] Все окружения имеют разные state, resource_name и environment_label.
- [ ] План передаётся в apply через artifacts; `.terraform` и секреты не попадают в cache.
- [ ] Все ресурсы удалены до отзыва credentials.

[GitLab state](https://docs.gitlab.com/user/infrastructure/iac/terraform_state/) | [Child pipelines](https://docs.gitlab.com/ci/pipelines/downstream_pipelines/) | [GitLab provider](https://registry.terraform.io/providers/gitlabhq/gitlab/latest/docs)
