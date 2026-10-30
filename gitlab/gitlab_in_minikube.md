# GitLab on Kubernetes with Minikube

_Source: [GitLab Helm Charts - Minikube Guide](https://docs.gitlab.com/charts/development/minikube/)_

This guide helps you set up a local Kubernetes development environment using Minikube.

> **Optional alternative.** The Servercore stand already gives you GitLab CE with trusted Let's Encrypt HTTPS, a registry on port 5050, an unprivileged Kubernetes runner and a kind cluster on your VM. You do **not** need this lab for the other labs. Do it only if you want your own GitLab on your laptop. It needs about 4 CPU and 10 GB RAM free.

---

## Prerequisites

- **kubectl**: install with your OS package manager (for example `brew install kubectl`) or via the [Google Cloud SDK](https://cloud.google.com/sdk/docs/install) (`gcloud components install kubectl`).
- **helm** v3: for example `brew install helm`. It is used for GitLab and for the runner below.
- **minikube**: Download from the [official releases](https://github.com/kubernetes/minikube/releases).
- A driver: **Docker** (`--driver=docker`) works on macOS, including Apple Silicon, and on Linux. VirtualBox is not available on Apple Silicon. Other drivers: VMware Fusion, HyperV, KVM.

---

## Starting Minikube

GitLab requires more resources than Minikube's defaults:

- **CPUs**: 4 (minimum 3)
- **Memory**: 10 GB (minimum 6 GB)
- **Disk**: 20 GB+

Start Minikube:
```sh
minikube start --driver=docker --cpus 4 --memory 10240
```

> With the Docker driver on macOS the minikube IP is usually not reachable from the host, so `https://gitlab.<minikube-ip>.nip.io` may not open. In that case use `minikube tunnel`, or the VirtualBox/other VM driver on Intel. **Verify** on your machine with `curl -k https://gitlab.$(minikube ip).nip.io` after the install (`-k` skips certificate checks: use it only as a reachability probe against your own self-signed GitLab and never send tokens or passwords with it). The values below assume the default VirtualBox IP `192.168.99.100` unless you override it.

Get the cluster IP:
```sh
minikube ip
```

Stop Minikube:
```sh
minikube stop
```

---

## Using Minikube

- Acts as a single-node Kubernetes cluster.
- PersistentVolumes use `hostPath` (data may not persist across reboots).
- No Load Balancers or advanced scheduling.

---

## Enable Add-ons

Enable Ingress for GitLab:
```sh
minikube addons enable ingress
```

Access the Kubernetes dashboard:
```sh
minikube dashboard --url
```

---

## Deploying GitLab

Clone the GitLab Helm chart repository at the **same tag as `gitlab_in_kind.md`** (`v9.11.7`), not `master`:
```sh
git clone --depth 1 --branch v9.11.7 https://gitlab.com/gitlab-org/charts/gitlab.git
cd gitlab
```

> Версия чарта должна совпадать с `gitlab_in_kind.md` (v10+ не содержит bundled PostgreSQL/Redis, см. ту лабу).
> Пример values берём из клона, а не с `master`: **проверь:** `ls examples | grep minikube` — имена файлов в v9.11.7 могут отличаться.

### Recommended Settings (4 CPU, 10 GB RAM)
```sh
helm dependency update
helm upgrade --install gitlab . \
  --timeout 600s \
  -f examples/values-minikube.yaml
```

### Minimal Settings (3 CPU, 6 GB RAM)
```sh
helm dependency update
helm upgrade --install gitlab . \
  --timeout 600s \
  -f examples/values-minikube-minimum.yaml
```

If your Minikube IP is not `192.168.99.100`, override endpoints:
```sh
--set global.hosts.domain=$(minikube ip).nip.io \
--set global.hosts.externalIP=$(minikube ip)
```

---

## DNS Configuration

The example uses `nip.io` for dynamic DNS. If unavailable, add entries to `/etc/hosts`:
```
192.168.99.100 gitlab.some.domain registry.some.domain minio.some.domain
```

---

## Self-Signed Certificates

If using self-signed certs, fetch the CA certificate after deployment and add it to your system trust store. See [BounCA's tutorial](https://www.bounca.org/tutorials/install_root_certificate.html) for details.

> The runner pods will not trust this CA either, so a job may fail with `x509: certificate signed by unknown authority` when it contacts GitLab. The runner chart accepts a Kubernetes secret with the CA via `certsSecretName` (secret key `<hostname>.crt`). **Verify** the exact secret and key names in the GitLab Runner chart documentation for your version before relying on this.

---

## Accessing GitLab

Visit: `https://gitlab.<minikube-ip>.nip.io`

To get the initial root password:
```sh
kubectl get secret <release-name>-gitlab-initial-root-password -ojsonpath='{.data.password}' | base64 --decode ; echo
```
Replace `<release-name>` with your Helm release name (default: `gitlab`).

**Expect:** the page opens (accept the self-signed certificate warning) and you can log in as `root` with the printed password.

---

## Installing GitLab Runner with Helm

See [GitLab Runner on Kubernetes](https://docs.gitlab.com/runner/install/kubernetes/) for full details.
## Create a GitLab Project Runner

To create a project runner in your GitLab project:

1. In the left sidebar, select **Search or go to** and find your project.
2. Go to **Settings > CI/CD**.
3. Expand the **Runners** section.
4. Click **New project runner**.
5. Choose your operating system.
6. In the **Tags** section, select the **Run untagged** checkbox if you want the runner to pick up untagged jobs (optional).
7. Click **Create runner**.

This will generate an authentication token (`glrt-...`, показывается один раз) to use when configuring your GitLab Runner. Старый registration token в этом flow не используется.

1. **Prepare your `values.yaml`**  
   - Set `gitlabUrl` to your GitLab instance URL (e.g., `https://gitlab.<minikube-ip>.nip.io`).
   - Set `rbac.create: true` to allow the runner to create pods.
   - `values.yaml` content:
     ```yaml
     rbac:
       create: true
     gitlabUrl: https://gitlab.<minikube-ip>.nip.io
     runnerToken: "glrt-xxxxx"
     runners:
       config: |
         [[runners]]
           [runners.kubernetes]
             namespace = "gitlab-runner"
             image = "alpine:3.22"
             privileged = false
     ```
     The runner is **unprivileged**, like the stand's. Do not set `privileged: true`: it gives job pods root on the node. Your own `runners.config` replaces the chart default, so the default `image` must be set here, otherwise jobs without `image:` fail with "no image specified".
   - Set `runnerToken` to the authentication token (`glrt-...`) from your GitLab UI. Не храните этот файл в git: токен в нём виден тем, кто читает файл.

2. **Add the GitLab Helm repository:**
    ```sh
    helm repo add gitlab https://charts.gitlab.io
    helm repo update gitlab
    ```

3. **Check available GitLab Runner versions:**
    ```sh
    helm search repo -l gitlab/gitlab-runner
    ```

4. **Install GitLab Runner:**
    - For Helm 3:
      ```sh
      helm install --namespace gitlab-runner --create-namespace gitlab-runner -f <CONFIG_VALUES_FILE> gitlab/gitlab-runner
      ```
    - Replace `<CONFIG_VALUES_FILE>` with your `values.yaml` path. The namespace is `gitlab-runner` and the release name is `gitlab-runner`.

    ### Verifying Your GitLab Runner Installation

    After installing the GitLab Runner, verify that everything is working as expected:

    1. **Check the Helm release:**
        ```sh
        helm --namespace gitlab-runner get all gitlab-runner
        ```

    2. **List all resources in the namespace:**
        ```sh
        kubectl -n gitlab-runner get all
        ```

    3. **View logs for the GitLab Runner Pod:**
        ```sh
        kubectl -n gitlab-runner logs -f pod/<gitlab-runner-pod-name>
        ```
        Replace `<gitlab-runner-pod-name>` with the actual pod name from the previous command.

    4. **Verify the runner is registered in GitLab:**
        - Go to your GitLab project.
        - Navigate to **Settings > CI/CD > Runners**.
        - Confirm that your new runner appears in the list with a green (online) status.

    5. **Run a test pipeline.** Add this `.gitlab-ci.yml` to the project and push:
        ```yaml
        test:
          image: alpine:3.22
          script:
            - echo "hello from minikube runner"
        ```
        **Expect:** the pipeline (**Build > Pipelines**) turns green, and a job pod appears briefly in `kubectl -n gitlab-runner get pods`.

    ---

5. **Upgrade GitLab Runner:**
    ```sh
    helm upgrade --namespace gitlab-runner -f <CONFIG_VALUES_FILE> gitlab-runner gitlab/gitlab-runner
    ```
    - Add `--version <RUNNER_HELM_CHART_VERSION>` to specify a version.

6. **Uninstall GitLab Runner:**
    ```sh
    helm delete --namespace gitlab-runner gitlab-runner
    ```

7. **Cleanup.** When you finish, remove everything and free the resources:
    ```sh
    helm uninstall gitlab-runner --namespace gitlab-runner
    kubectl delete namespace gitlab-runner
    helm uninstall gitlab
    minikube delete
    ```
    Also revoke the runner in GitLab (project **Settings > CI/CD > Runners**) and delete `values.yaml` from disk (it contains the `runnerToken`). `minikube delete` removes the cluster, its volumes and the 10 GB VM or container.

Refer to the [GitLab Runner Helm Chart documentation](https://docs.gitlab.com/runner/install/kubernetes.html) for advanced configuration and troubleshooting.
