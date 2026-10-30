# GitLab labs на Servercore

На стенде уже есть GitLab CE на отдельной VM и kind-кластер на VM каждого студента. GitLab заново в kind не устанавливаем. Оператор настраивает сервер и runner; студент создаёт учебные проекты внутри своей группы.

## Что получить у оператора

- URL GitLab, например `https://<gitlab-host>`, и учётную запись.
- Свою группу GitLab и роль Maintainer в учебных проектах.
- Номер VM (`lab-1` или `lab-2`) и команду SSH.
- Online group runner этой VM: executor `kubernetes`, tags `lab-1,kubernetes` (для второй VM - `lab-2,kubernetes`), **Run untagged jobs: enabled**. Точные теги проверьте в **Settings > CI/CD > Runners**, не угадывайте.

Runner берёт только проекты вашей группы. Обычные примеры без `tags:` работают без правок. Для проверки конкретного runner добавьте `tags: [lab-1]`. В runner-лабе теги `lab-docker` и `lab-k8s` относятся к runner'ам, которые вы создаёте сами; встроенному runner их не добавляйте.

Оператор сначала запускает `make configure` без runner-токенов, создаёт group runners в UI, затем добавляет `GITLAB_RUNNER_TOKEN_LAB_01` / `GITLAB_RUNNER_TOKEN_LAB_02` в `.env` и повторяет `make configure`. Не передавайте эти токены студентам и не записывайте их в учебные проекты.

## Первая проверка

Создайте private-проект в своей группе. Клонируйте URL из UI по HTTPS. Из VM используйте HTTPS: Git-over-SSH на GitLab из приватной сети стенда не открыт.

В `.gitlab-ci.yml`:

```yaml
stages: [test]

smoke:
  image: alpine:3.22
  script:
    - 'echo "runner=$CI_RUNNER_DESCRIPTION tags=$CI_RUNNER_TAGS"'
    - cat /etc/alpine-release
    - printf 'artifact-ok\n' > result.txt
  artifacts:
    paths: [result.txt]

consume:
  image: alpine:3.22
  needs:
    - job: smoke
      artifacts: true
  script:
    - test "$(cat result.txt)" = artifact-ok
```

Обе jobs должны пройти, `result.txt` должен скачиваться из UI. На VM во время pipeline: `kubectl -n gitlab-ci get pods`. Jobs выполняются в отдельных Pod, поэтому `image:` действительно выбирает окружение.

## HTTPS и Registry

Полный путь с Container Registry требует доверенного HTTPS. Оператор выдаёт сертификат и открывает 443/5050 для студенческих сетей. Registry того же GitLab доступен как `<gitlab-host>:5050`; в jobs используйте `CI_REGISTRY` и `CI_REGISTRY_IMAGE`.

Ansible добавляет приватный IP для GitLab hostname на VM, в kind nodes и в job Pod. Имя сертификата сохраняется, обход через публичный floating IP не нужен. Проверку TLS не отключайте.

`ACME_STAGING=true` проверяет выдачу тестового сертификата и оставляет HTTP. Это режим проверки DNS/ACME, не готовый путь для registry build/push. Перед registry-лабами оператор переключает `ACME_STAGING=false` и повторяет `make configure`.

## Docker-in-Docker

По умолчанию runner unprivileged, Docker daemon в jobs нет. Docker-in-Docker (Trivy-лаба, `docker build`) работает только там, где оператор включил privileged **на вашей VM**: попросите оператора, это его отдельная настройка. Она разрешает privileged containers всем jobs этого runner; после упражнений попросите вернуть как было. Лаба [полного цикла](gitlab_ci_lab_full_cycle.md) privileged не требует: образы там собираются buildah без Docker.

Проверка build/push/pull после включения:

```yaml
registry-smoke:
  image: docker:27
  services:
    - name: docker:27-dind
      alias: docker
      command: ["--mtu=1450"]
  variables:
    DOCKER_HOST: tcp://docker:2375
    DOCKER_TLS_CERTDIR: ""
  before_script:
    - 'for attempt in $(seq 1 60); do docker info >/dev/null 2>&1 && break; sleep 2; done'
    - docker info
    - echo "$CI_REGISTRY_PASSWORD" | docker login "$CI_REGISTRY" -u "$CI_REGISTRY_USER" --password-stdin
  script:
    - printf 'FROM alpine:3.22\nRUN echo registry-ok > /proof\n' > Dockerfile
    - docker build -t "$CI_REGISTRY_IMAGE:smoke" .
    - docker push "$CI_REGISTRY_IMAGE:smoke"
    - docker image rm "$CI_REGISTRY_IMAGE:smoke"
    - docker pull "$CI_REGISTRY_IMAGE:smoke"
    - docker run --rm "$CI_REGISTRY_IMAGE:smoke" cat /proof
```

Используйте этот же `alias`, ожидание Docker и MTU в DinD jobs Trivy. Podman на VM нужен для kind; CI-job получает настоящий Docker client и DinD из указанных образов.

## Деплой в kind

Container jobs не получают прежний `/home/gitlab-runner/.kube/config`. Адрес `127.0.0.1:6443` внутри job указывает на её Pod, а не на VM. Для деплоя пройдите [GitLab Agent lab](gitlab_agent_k8s_lab.md) и используйте её `KUBE_CONTEXT`.

На Servercore установлен Linux package GitLab. KAS включён в нём по умолчанию: Шаг 0 [GitLab Agent lab](gitlab_agent_k8s_lab.md) (включение KAS в GitLab-в-kind) пропустите. Возьмите KAS URL из UI регистрации агента. Для HTTPS-инсталляции WebSocket endpoint обычно `wss://<gitlab-host>/-/kubernetes-agent/`; используйте значение своей версии.

Если имя GitLab не резолвится внутри Pod агента, создайте файл `servercore-agent-values.yaml` с содержимым ниже и добавьте `-f servercore-agent-values.yaml` к команде `helm upgrade --install` при установке agentk (в Agent lab - Шаг 2; в full-cycle - вместе с `--values ci/agent-values.yaml`). Если имя резолвится, файл не нужен. Файл направляет соединение в приватную сеть:

```yaml
hostAliases:
  - ip: "10.42.0.10"  # приватный IP GitLab, спросите у оператора
    hostnames: ["<gitlab-host>"]
```

Замените оба значения своими. После `make cluster-reset` повторно установите agentk и приложения: кластер очищен. Manager runner и его сертификаты Ansible восстановит сам.

## Какие лабы проходить

1. Walkthrough, Runner, Rules, Helm, Monorepo, Components.
2. [OpenTofu state](gitlab_ci_lab_tf.md) и [пять окружений](gitlab_ci_lab_five_envs.md), Path B. Лаба пяти окружений требует доработки: шаблон `Terraform.gitlab-ci.yml` больше не опубликован, поэтому как обязательную лабу стенда её не берите, пока замена не проверена. Если запускаете: задайте `TF_VAR_gitlab_base_url=https://<gitlab-host>/api/v4/`, песочницу создавайте подгруппой своей группы, и учтите, что стенд на CE: CODEOWNERS-одобрения, protected environments и вкладка Security недоступны (используйте бесплатные варианты из лабы).
3. Trivy после включения privilege; DevSecOps scanners работают в обычных container jobs.
4. GitLab Agent для деплоя в свой kind. Агент по умолчанию получает права cluster-admin: используйте его только для этой лабы; для полного цикла ставится отдельный агент с ограниченным ServiceAccount.
5. [Полный CI/CD цикл](gitlab_ci_lab_full_cycle.md): web app, image и Helm OCI в GitLab Registry, деплой в kind и четыре практических break/fix упражнения.

[Cosign keyless](gitlab_ci_lab_cosign.md) с публичным Sigstore выполняйте на GitLab.com. HTTPS и OIDC на Servercore не добавляют его issuer в доверенные публичного Fulcio. Вариант для self-managed требует собственного Sigstore; он не входит в этот стенд.

При `pending` проверьте группу, online runner, tags и Run untagged jobs. При TLS-ошибке проверьте доверенный сертификат и hostname. При `Cannot connect to Docker daemon` проверьте, что оператор включил privileged на вашей VM, и дождитесь DinD. После удаления runner-токена оператор повторяет configure: сервис останавливается; запись runner в GitLab удаляется отдельно через UI.
