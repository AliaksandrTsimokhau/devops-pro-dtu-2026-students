# Container Scanning Configuration Tutorial

This tutorial guides you through configuring container scanning in your pipeline:

1. [Create a new project](#create-a-new-project)
2. [Add a Dockerfile](#add-a-dockerfile-to-new-project)
3. [Create pipeline configuration](#create-pipeline-configuration)
4. [Check for reported vulnerabilities](#check-for-reported-vulnerabilities)
5. [Update the Docker image and rescan](#update-the-docker-image)

---

## Create a New Project

To create a new project:

1. On the left sidebar, at the top, select **Create new** ( ![plus icon]( ) ) and **New project/repository**.
2. Select **Create blank project**.
3. In **Project name**, enter `Tutorial container scanning project`.
4. In **Project URL**, select a namespace for the project.
5. Check **Initialize repository with a README**.
6. Select **Create project**.

Clone the project locally:

```bash
git clone https://gitlab.example.com/<your-namespace>/tutorial-container-scanning-project.git
cd tutorial-container-scanning-project
```

---

## Add a Dockerfile to New Project

Create a minimal Dockerfile in your local clone:

```bash
cat > Dockerfile << 'EOF'
FROM hello-world:latest
EOF
```

Commit and push:

```bash
git add Dockerfile
git commit -m "add minimal Dockerfile for scanning"
git push origin main
```

> Docker images created from this Dockerfile are based on the `hello-world` Docker image. You can swap it for a more vulnerable image later (see the last section). `hello-world:latest` is for this demo only: `latest` changes over time, so use a pinned tag in a real project.

---

## Create Pipeline Configuration

The pipeline will build a Docker image from the `Dockerfile` and scan it with Trivy.

Create `.gitlab-ci.yml` in your local clone:

```yaml
variables:
  # проверь: актуальная версия на https://github.com/aquasecurity/trivy/releases (выберите конкретную, не "latest")
  TRIVY_VERSION: "<x.y.z>"
  # проверь тег образа: https://hub.docker.com/r/aquasec/trivy/tags (используется в job trivy-repo-scan)
  TRIVY_IMAGE: "aquasec/trivy:<tag>"

trivy:
  stage: test
  image: docker:27
  services:
    - name: docker:27-dind
      entrypoint: ["env", "-u", "DOCKER_HOST"]
      command: ["dockerd-entrypoint.sh"]
  variables:
    DOCKER_HOST: tcp://docker:2375/
    DOCKER_DRIVER: overlay2
    DOCKER_TLS_CERTDIR: ""
    IMAGE: trivy-ci-test:$CI_COMMIT_SHA
    TRIVY_NO_PROGRESS: "true"
    TRIVY_CACHE_DIR: ".trivycache/"
  before_script:
    - echo "Trivy $TRIVY_VERSION"
    - wget --no-verbose https://github.com/aquasecurity/trivy/releases/download/v${TRIVY_VERSION}/trivy_${TRIVY_VERSION}_Linux-64bit.tar.gz -O - | tar -zxvf -
  allow_failure: true
  script:
    - docker build -t $IMAGE .
    - ./trivy image --exit-code 0 --format template --template "@contrib/gitlab.tpl" -o gl-container-scanning-report.json $IMAGE
    - ./trivy image --exit-code 0 --severity HIGH $IMAGE
    - ./trivy image --exit-code 1 --severity CRITICAL $IMAGE
  cache:
    paths:
      - .trivycache/
  artifacts:
    when: always
    reports:
      container_scanning: gl-container-scanning-report.json
```

Commit and push:

```bash
git add .gitlab-ci.yml
git commit -m "ci: add trivy container scanning"
git push origin main
```

> Notes on the job above:
> - `TRIVY_VERSION` is pinned on purpose. The earlier version asked the GitHub API for the `latest` release on every run. Unauthenticated GitHub API calls are rate-limited, so the job could fail for reasons unrelated to your code. Update the pin deliberately.
> - `--template "@contrib/gitlab.tpl"` points to the template in the release tarball. The `@/contrib/...` path works only in the `aquasec/trivy` image. `проверь расположение gitlab.tpl в архиве` (`tar -tzf trivy_<version>_Linux-64bit.tar.gz | grep gitlab`), and check the archive file name for your version.
> - `artifacts: when: always` uploads the report even when a job step fails.

After the push, a new pipeline starts automatically. When it’s finished, you can check the results of the scan.

---

## Check for Reported Vulnerabilities

Vulnerabilities for a scan are located on the pipeline that ran the scan. To check for reported vulnerabilities:

1. Select **CI/CD > Pipelines** and select the most recent pipeline. This pipeline should have a job called `trivy` in the test stage.
2. Open the `trivy` job log: the `--severity HIGH` step prints a table of findings.
3. On GitLab Ultimate, the `container_scanning` report also fills the pipeline's **Security** tab. On Free and Premium the tab is not available; use the job log instead.

---

## Update the Docker Image

A Docker image based on `hello-world:latest` is unlikely to show any vulnerabilities. For an example of a scan that reports vulnerabilities, use an older base image with known CVEs:

```bash
# Replace the base image with an older, more vulnerable one
cat > Dockerfile << 'EOF'
FROM python:3.8.0
EOF

git add Dockerfile
git commit -m "ci: use older python image to demonstrate vulnerabilities"
git push origin main
```

After the push, a new pipeline starts with the updated Dockerfile. When it finishes, check the `trivy` job log (or the **Security** tab on Ultimate) for reported CVEs.

---

## Scan the Repository with Trivy

You can also scan your repository's files (such as source code and configuration files) for vulnerabilities and misconfigurations using Trivy's repository scanning feature.

To add a pipeline job for repository scanning, add the following job to your `.gitlab-ci.yml`:

```yaml
trivy-repo-scan:
  stage: test
  image:
    name: $TRIVY_IMAGE
    entrypoint: [""]
  script:
    - trivy repo --exit-code 0 --format table .
    - trivy repo --exit-code 0 --format json --output trivy-repo-report.json .
    - trivy repo --exit-code 1 --severity CRITICAL .
  allow_failure: true
  artifacts:
    when: always
    paths:
      - trivy-repo-report.json
```

Commit and push:

```bash
git add .gitlab-ci.yml
git commit -m "ci: add trivy repository scan job"
git push origin main
```

This job uses the official Trivy image to scan source code and configuration files for vulnerabilities. Results are printed in the job log and saved as the `trivy-repo-report.json` artifact. For more details, see the [Trivy repository scanning documentation](https://trivy.dev/v0.64/docs/target/repository/).

> **Note for local runners (kind/Minikube):** Docker-in-Docker (`docker:dind`) requires a privileged runner. If your runner is not privileged, the `trivy` image-scan job will fail. In that case, use the `trivy-repo-scan` job (no DinD required) or configure `privileged: true` in your runner's `config.toml`.

---

## Self-Check

- [ ] Project created and cloned locally.
- [ ] `Dockerfile` committed and pushed (`FROM hello-world:latest`).
- [ ] `.gitlab-ci.yml` with the `trivy` job committed and pushed.
- [ ] Pipeline ran and the `trivy` job completed (green or yellow with `allow_failure: true`).
- [ ] Vulnerabilities visible in the `trivy` job log (may be empty for `hello-world`); on Ultimate also in the **Security** tab.
- [ ] Updated `Dockerfile` to use an older image — new pipeline shows CVEs.
- [ ] (optional) Added `trivy-repo-scan` job — pipeline reports source code findings.