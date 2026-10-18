
# LAB: Create a Repository to Deploy Helm Charts Using Helmfile

## Lab Requirements

- `kubectl`
- `helm`
- Existing or new Helm chart(s)
- Kubernetes cluster (local: kind, microk8s, minikube, or managed: GKE, EKS, AKS, etc.)
- GitLab server

**Estimated time:** 1h 30min

---

## 1. Create a GitLab Repository

Create a new repository in GitLab for your Helmfile-based deployment setup.

### Instructions to Create a GitLab Repository

1. Log in to your GitLab account.
2. Click the **"New project"** button (usually found on the dashboard or under the Projects menu).
3. Choose **"Create blank project"**.
4. Enter a **Project name** (e.g., `release-helmfile`).
5. Optionally, add a description and set the visibility (Private, Internal, or Public).
6. Click **"Create project"**.
7. Clone the repository to your local machine:

    ```sh
    git clone https://<your-gitlab-hostname>/<your-namespace>/<your-repo>.git
    cd <your-repo>
    ```

8. Start adding your files and commit your changes.

---

## 2. Repository Structure

Design your repository with logical abstraction layers for releases and values.

<details>
<summary>Example structure</summary>

```
release-helmfile/
├── ci/                  # GitLab CI YAML files and scripts
├── charts/              # Local Helm charts for development
├── release/             # Values for releases, organized by logical tiers
│   ├── common-tier/
│   │   ├── release-name1/
│   │   ├── release-name2/
│   │   └── helmfile.yaml
│   ├── another-tier/
│   │   ├── release-name1/
│   │   ├── release-name2/
│   │   └── helmfile.yaml
│   └── common.yaml      # Common Helmfile settings and values paths
├── .gitignore
├── .gitlab.yaml         # Main GitLab pipeline file
├── environments.yaml    # Environment-specific settings and feature flags
├── helmfile.yaml        # Main Helmfile referencing logical tiers
└── README.md
```
</details>

**Requirement:** Create at least two abstraction layers and deploy at least two charts (one local, one from an artifact registry).

---

## 3. Add Helm Charts

- Place an existing or new Helm chart in the `charts/` folder. you could resuse chart from lab `gitlab_ci_lab_helm.md`

#! give an example of release description one for local and for external


---

## 4. Create Helmfile for Each Logical Tier

- In each logical tier folder (e.g., `common-tier`, `another-tier`), create a `helmfile.yaml`.
- Add release descriptions for:
    - A local chart (from `charts/`)
    - An external chart (from a remote registry)

**Example:**

```yaml
# release/common-tier/helmfile.yaml
releases:
    - name: my-local-app
        chart: ../../charts/my-local-app #local chart
        values:
            - values.yaml

    - name: nginx-ingress
        chart: stable/nginx-ingress #external repo chart
        version: 4.0.6
        values:
            - values-nginx.yaml
```

---
## Example: `common.yaml`

This file defines shared Helm repository settings and default Helmfile behaviors.

```yaml
# release/common.yaml

repositories:
    - name: remote
        url: "{{ .StateValues.registry.helm }}"
        oci: true

helmDefaults:
    wait: {{ .StateValues | get "helmDefaults.wait" true | toYaml }}
    timeout: {{ .StateValues | get "helmDefaults.timeout" 1200 | toYaml }}
    historyMax: {{ .StateValues | get "helmDefaults.historyMax" 3 | toYaml }}
    atomic: {{ .StateValues | get "helmDefaults.atomic" false | toYaml }}
    cleanupOnFail: {{ .StateValues | get "helmDefaults.cleanupOnFail" false | toYaml }}
    waitForJobs: {{ .StateValues | get "helmDefaults.waitForJobs" true | toYaml }}
    # Default "" value means current Kubernetes context.
    kubeContext: {{ .StateValues | get "helmDefaults.kubeContext" "" | toYaml }}
    # Example: syncArgs can be used to skip schema validation if needed
    # syncArgs:
    #   - --skip-schema-validation
```

```yaml
templates:
    values: &values
        missingFileHandler: Warn
        values:
            - "{{ .Release.Name }}/values.yaml"
            - "{{ .Release.Name }}/values.yaml.gotmpl"
            - "{{ .Release.Name }}/values-hosting-{{ .StateValues.hosting }}.yaml"
            - "{{ .Release.Name }}/values-hosting-{{ .StateValues.hosting }}.yaml.gotmpl"
            - "{{ .Release.Name }}/values-tier-{{ .StateValues.tier }}.yaml"
            - "{{ .Release.Name }}/values-tier-{{ .StateValues.tier }}.yaml.gotmpl"
            - "{{ .Release.Name }}/values-network-{{ .StateValues.network }}.yaml"
            - "{{ .Release.Name }}/values-network-{{ .StateValues.network }}.yaml.gotmpl"
            - "{{ .Release.Name }}/values-type-{{ .StateValues.type }}.yaml"
            - "{{ .Release.Name }}/values-type-{{ .StateValues.type }}.yaml.gotmpl"
            - "{{ .Release.Name }}/values-env-{{ .Environment.Name }}.yaml"
            - "{{ .Release.Name }}/values-env-{{ .Environment.Name }}.yaml.gotmpl"
```


## 5. Reference Logical Tiers in the Main Helmfile

- In the root `helmfile.yaml`, reference all logical tier helmfiles.

**Example:**

```yaml
# helmfile.yaml
helmfiles:
    - path: release/common-tier/helmfile.yaml
    - path: release/another-tier/helmfile.yaml
```

You can also use more advanced referencing and base files:

```yaml
# helmfile.yaml

bases:
    - environments.yaml

helmfiles:
    # Shared monitoring helmfile
    # Must be installed in every environment regardless of cluster type
    - path: "releases/shared-monitoring/helmfile.yaml"
        values:
            - action: "create"
            - {{ toYaml .Environment.Values | indent 6 }}

    # Shared infra helmfile
    # Must be installed in every environment regardless of cluster type
    - path: "releases/infra-shared/helmfile.yaml"
        values:
            - {{ toYaml .Environment.Values | indent 6 }}
```


---

## 5a. Specify `kubeContext` in Environment Values

To ensure Helmfile deploys to the correct Kubernetes cluster, define the `kubeContext` for each environment in your `environments.yaml`. You can set this directly under each environment or within `helmDefaults` for more granular control.

**Example:**

```yaml
# environments.yaml
environments:
    dev:
        kubeContext: kind-dev
        helmDefaults:
            kubeContext: kind-dev
    prod:
        kubeContext: prod-context
        helmDefaults:
            kubeContext: prod-context
```

- The `kubeContext` field tells Helmfile which Kubernetes context to use for that environment.
- Setting `helmDefaults.kubeContext` overrides the default context for all releases in that environment.
- You can reference these values in your Helmfile templates to ensure deployments target the intended cluster.

--- 


## Example: Release Descriptions for Local and External Charts

Below is an example of how to define releases in a `helmfile.yaml` for a logical tier. This demonstrates deploying both an external chart (from a remote registry) and a local chart (from your repository).

```yaml
# release/common-tier/helmfile.yaml

releases:
    # External chart: Prometheus Stack (Grafana, Prometheus, Alertmanager)
    - name: prometheus-stack
        chart: strikerz/kube-prometheus-stack
        namespace: monitoring
        labels:
            component: monitoring
        version: {{ .StateValues.monitoring.chartVersion }}
        installed: {{ .Values | get "monitoring.prometheus.enabled" false | toYaml }}
        {{ if or (eq .Environment.Name "backoffice") (eq .Environment.Name "midoffice") }}
        hooks:
            - events:
                    - postsync
                showlogs: true
                command: "../../ci/kubectl_patch_servicemonitor_with_basicauth.sh"
                args:
                    - "{{ `{{ .Release.KubeContext }}` }}"
        {{ end }}
        <<: *values
        needs:
            - tls-secret
            - prometheus-stack-secrets

    # Local chart: Kubernetes Alerts
    - name: alerts-k8s
        chart: ../../charts/alerts-k8s
        namespace: monitoring
        labels:
            component: monitoring
        version: 0.3.0
        installed: {{ .Values | get "monitoring.enabled" false | toYaml }}
        # Alerts are defined by CRD that will be created after prometheus-stack installation
        needs:
            - victoria-metrics/prometheus-operator-crds
        <<: *values
```

**Instructions:**
- Place this example in the `helmfile.yaml` of your logical tier (e.g., `release/common-tier/helmfile.yaml`).
- Adjust chart names, versions, and values as needed for your environment.
- The first release (`prometheus-stack`) pulls from an external registry, while the second (`alerts-k8s`) uses a local chart from your `charts/` directory.
- Use the `needs` field to specify dependencies between releases.
- Use `<<: *values` to inherit common values templates as defined in your `common.yaml`.


---

## 6. Define Common Settings in `common.yaml`

- Add remote registry aliases and describe all values paths for templating.
- Paths can be templated; missing files will be skipped.

**Example:**

```yaml
# release/common.yaml
repositories:
    - name: stable
        url: https://charts.helm.sh/stable

valuesPaths:
    - "{{ .Release.Name }}/values.yaml"
    - "common-values.yaml"
```

---

## 7. Define Environments in `environments.yaml`

- Describe environments, values, feature flags, etc.

**Example:**

```yaml
# environments.yaml
environments:
    dev:
        kubeContext: kind-dev
        clusterName: dev-cluster
        featureFlags:
            enableFeatureX: true
    prod:
        kubeContext: prod-context
        clusterName: prod-cluster
        featureFlags:
            enableFeatureX: false
```

## Another Example: `environments.yaml`

Below is an alternative example of how to structure your `environments.yaml` file, including multiple environments with custom values and settings:

```yaml
environments:
    stagings:
        values:
            - env-values/env-base.yaml
            - env-values/env-type-meta.yaml
        helmDefaults:
            kubeContext: baremetal-backend-stagings
        tier: dev
        nat:
            enabled: true
        dashboard:
            enabled: true

    demo:
        values:
            - env-values/env-base.yaml
            - env-values/env-type-meta.yaml
        helmDefaults:
            kubeContext: baremetal-backend-demo
        tier: prod
        dashboard:
            enabled: true

    production:
        values:
            - env-values/env-base.yaml
            - env-values/env-type-prod.yaml
        helmDefaults:
            kubeContext: baremetal-backend-prod
        tier: prod
        nat:
            enabled: false
        dashboard:
            enabled: false
```

This example demonstrates:
- How to define multiple environments (`stagings`, `demo`, `production`)
- How to specify environment-specific values files
- How to set custom `helmDefaults` and other environment-specific variables
- How to enable or disable features per environment
---

## 8. Create GitLab Pipeline in `.gitlab.yaml`

- Define pipeline stages: scan, validate, plan, deploy.
- Store reusable parts in `ci/` and include them.
- Use CI/CD variables for secrets (e.g., registry, cluster auth).

**Example:**

```yaml
# .gitlab.yaml
stages:
    - scan
    - validate
    - plan
    - deploy

include:
    - local: 'ci/before_script.yaml'

scan:
    stage: scan
    script:
        - trivy config .

validate:
    stage: validate
    script:
        - helm lint charts/*

plan:
    stage: plan
    script:
        - helmfile diff

deploy:
    stage: deploy
    script:
        - helmfile apply
    environment:
        name: $CI_ENVIRONMENT_NAME
```

---

## 9. Add `.gitignore`

- Ignore files and folders as needed (e.g., `.DS_Store`, `*.tgz`, etc.).

---

## 10. Document Your Repository

- In `README.md`, describe the repo's purpose, structure, and usage instructions.
- Include any setup or developer notes.

---

