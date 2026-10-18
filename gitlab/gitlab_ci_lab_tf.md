> ⚠️ **Шаблон `Terraform.gitlab-ci.yml` больше не опубликован GitLab.com** (проверено через templates API).
> Образ `registry.gitlab.com/gitlab-org/terraform-images/stable:latest` из этой лабы и обёртка
> `gitlab-terraform` тоже могут быть изменены или удалены — **проверь** их доступность до начала работы.
> Актуальный вариант: OpenTofu CI/CD component из GitLab CI/CD Catalog (**проверь точное имя компонента и версию в Catalog**: Explore → CI/CD Catalog → поиск «OpenTofu»). Имя не угадывайте.
> Если используете компонент, jobs и переменные ниже придётся адаптировать под его документацию (**проверь:** имена jobs и inputs).

## Useful Resources

- [Managing Multiple Environments with Terraform and GitLab CI](https://about.gitlab.com/blog/managing-multiple-environments-with-terraform-and-gitlab-ci/)
- [GitLab Terraform State Documentation](https://docs.gitlab.com/user/infrastructure/iac/terraform_state/)

---

## Шаг 1: Создайте репозиторий

1. **Создайте репозиторий**
   - **New project → Create blank project**.
   - Имя: `terraform-gitlab-ci-lab`.
   - Visibility: Private.
   - Отметьте **Initialize repository with a README**.
   - Нажмите **Create project**.

2. **Клонируйте репозиторий локально**

   ```bash
   git clone https://gitlab.example.com/<your-namespace>/terraform-gitlab-ci-lab.git
   cd terraform-gitlab-ci-lab
   ```

> Access token и CI/CD-переменные для state не нужны: `gitlab-terraform` авторизуется в HTTP backend через `CI_JOB_TOKEN`, который GitLab выдаёт каждому job'у.

---

## Шаг 2: Создайте `provider.tf` и `main.tf`

**provider.tf**
```hcl
terraform {
  backend "http" {} # for gitlab
}
```

**main.tf** — минимальный ресурс для проверки pipeline (например, null_resource или любой ресурс):

```hcl
resource "null_resource" "example" {}
```

Закоммитьте файлы:

```bash
git add provider.tf main.tf
git commit -m "terraform: add provider and main config"
git push origin main
```

---

## Шаг 3: Настройте хранение Terraform State в GitLab

GitLab предоставляет встроенный HTTP backend для хранения Terraform state — никакого дополнительного S3 или Consul не нужно. State хранится в **Operate → Terraform states** вашего проекта.

Адрес state задаётся переменной `TF_ADDRESS` в `.gitlab-ci.yml` (шаг 4). `CI_API_V4_URL` и `CI_PROJECT_ID` — предопределённые переменные, GitLab подставит их сам.

Просмотреть state файлы можно через:
- Left sidebar → **Operate → Terraform states**.

---

## Шаг 4: Создайте `.gitlab-ci.yml`

Создайте файл `.gitlab-ci.yml` в корне репозитория:

```yaml
image: registry.gitlab.com/gitlab-org/terraform-images/stable:latest

variables:
  TF_STATE: "tf_state"
  TF_ADDRESS: ${CI_API_V4_URL}/projects/${CI_PROJECT_ID}/terraform/state/${TF_STATE}

cache:
  key: ${TF_STATE}
  paths:
    - .terraform

stages:
  - validate
  - build
  - deploy
  - destroy

init:
  stage: .pre
  script:
    - gitlab-terraform init

fmt:
  stage: .pre
  script:
    - gitlab-terraform fmt

validate:
  stage: validate
  script:
    - gitlab-terraform validate

plan:
  stage: build
  script:
    - gitlab-terraform plan
    - gitlab-terraform plan-json
  artifacts:
    name: plan
    paths:
      - plan.cache
    reports:
      terraform: plan.json

apply:
  stage: deploy
  script:
    - gitlab-terraform apply
  dependencies:
    - plan
  rules:
    - if: '$CI_COMMIT_BRANCH == "main"'
      when: manual

destroy:
  stage: destroy
  script:
    - gitlab-terraform destroy
  dependencies:
    - apply
  rules:
    - if: '$CI_COMMIT_BRANCH == "main"'
      when: manual
```

Закоммитьте и запушьте:

```bash
git add .gitlab-ci.yml
git commit -m "ci: add terraform pipeline"
git push origin main
```

После пуша pipeline запустится автоматически. Стадии `init` и `fmt` выполнятся в `.pre`, затем `validate` и `plan`. Стадии `apply` и `destroy` — ручные (кнопка в UI).

---

## Шаг 5: Мониторинг pipeline

Откройте **CI/CD → Pipelines** в GitLab UI. Вы увидите запущенный pipeline с этапами:

- `.pre` — `init` + `fmt` (автоматически)
- `validate` — проверка синтаксиса (автоматически)
- `build` — `plan` + `plan-json`, артефакт plan.cache (автоматически)
- `deploy` — `apply` (нажмите ▶ вручную)
- `destroy` — `destroy` (нажмите ▶ вручную, осторожно!)

После `apply` откройте **Operate → Terraform states** — должен появиться state файл `tf_state`.

---

## Чеклист самопроверки

- [ ] Репозиторий создан и склонирован локально.
- [ ] `provider.tf` и `main.tf` закоммичены и запушены.
- [ ] `.gitlab-ci.yml` закоммичен и запушен.
- [ ] Pipeline запустился: `.pre`, `validate`, `build` (plan) прошли зелёными.
- [ ] `plan` job создал артефакт `plan.cache`.
- [ ] `apply` выполнен вручную через UI.
- [ ] Terraform state появился в **Operate → Terraform states**.
- [ ] `destroy` выполнен вручную — ресурсы удалены, state обновился.
