# Lab 8: ship and recover a real web release

Build a small monorepo with a Go web application, its browser UI, Helm charts and CI configuration. The result is a live **Release Board** that shows the deployed commit, environment, chart version and pipeline link.

**Time:** 2-3 hours after the runner and Agent are ready.
**Start with:** [Servercore onboarding](servercore_gitlab.md) and [Runner](gitlab_runner_lab.md); [Trivy](gitlab_ci_lab_basic_trivy_scan.md) and the [Helm deployment lab](../helm/helm_gitlab_cicd_lab.md) help. This lab is self-contained: it sets up the GitLab Agent for Kubernetes itself, in Part B.

**Menu paths** in this lab are written for the GitLab web UI of your project, for example **Settings > CI/CD > Variables**. They are in the project's left sidebar unless a step says otherwise.

## How it works

| Part | What it does |
|---|---|
| **Runner** (Kubernetes executor) | Runs every job as a pod in your VM's kind cluster. It is unprivileged and holds no cluster credentials. |
| **buildah + skopeo** | Build the image without a Docker daemon and save it as an archive; skopeo publishes that same archive. No Docker-in-Docker, no privileged pods. |
| **GitLab Agent** | Gives deploy jobs access to the cluster through a service account that can manage releases in **one namespace only**. |
| **Container Registry** | Stores the image (`$CI_REGISTRY_IMAGE/web`) and the Helm chart (`oci://$CI_REGISTRY_IMAGE/charts/release-board`). |
| **Traefik** | Serves the application on your VM. |

The pipeline stages are `verify -> build -> scan -> publish -> deploy`. Only the default branch publishes and deploys.

| Gate | What it proves |
|---|---|
| Go format, vet, race tests; browser tests | The API contract and failure feedback still work |
| Helm lint, values schema, Kubernetes schema | Invalid release configuration is rejected before deployment |
| Trivy source, Dockerfile and rendered-manifest scans | Leaked credentials and unsafe workloads are blocked |
| Build once, scan the saved image, SBOM | The bytes that passed the scan are the bytes that get published |
| Image and Helm OCI publication | Release inputs stay together and immutable |
| Helm rollout, Helm test, ingress smoke check | The right commit is really served through ingress |
| Serialized deploys, automatic rollback | A failed release never replaces the last working one |

## Where you run the lab

| Variant | Cluster | Runner, Traefik, DNS | Steps |
|---|---|---|---|
| **A. Stand (default)** | the kind cluster on your lab VM | provided by the operator | every step in this document |
| **B. Your own computer** (optional) | a kind cluster on your laptop | you install them | the same steps, plus [Variant B](#variant-b-your-own-computer-local-kind) for what differs |

Read the steps below for either variant. Variant B only lists the differences.

## Before you start (Variant A)

Ask the operator to confirm each point. In Variant B you set these up yourself, see the end of this document.

| Requirement | How to check |
|---|---|
| Your runner is online with tag `kubernetes`, uses the Kubernetes executor and is **unprivileged** | Your group: **Settings > CI/CD > Runners** shows it online |
| GitLab and its Registry have trusted HTTPS certificates and resolve from runner jobs and kind nodes | `curl -sI https://<registry-host>/v2/` answers `401`, with no certificate error. Never add insecure-registry or skip-TLS flags. |
| Traefik is installed in kind | `kubectl get ingressclass` lists `traefik` |
| The VM has a stable public address and ingress ports `80` and `443` open to your allowed CIDRs | The stand sets `lab_public_ip_mode = "static"` and `lab_ingress_ports = [80, 443]` |
| Jobs reach the VM's **private** ingress address; browsers reach the **public** one | Do not use the public address for CI's smoke check if the cloud network has no hairpin routing |

---

## Part A. Create the project and push the example

### Step 1. Create the project

**Do:**
1. In GitLab, open the **top-left menu (+) > New project/repository**, then **Create blank project**.
2. **Project name:** `release-board` (any name works). **Project URL:** select your group.
3. **Visibility Level:** **Private**.
4. **Uncheck** *Initialize repository with a README*.
5. Click **Create project**.

**Expect:** an empty project page that shows "The repository for this project is empty" with clone instructions.

**Why:** the example brings its own files, so an initial README would only create a conflicting first commit.

### Step 2. Clone the project over HTTPS

**Do:**
1. On the project page click **Code > Clone with HTTPS** and copy the URL.
2. Create an access token for the push: your **avatar > Edit profile > Access tokens > Add new token**, scopes `read_repository` and `write_repository`, short expiry. Copy it once.
3. Clone. Git asks for a username (your GitLab username) and a password (the token):

```sh
git clone https://<gitlab-host>/<group>/release-board.git
cd release-board
```

**Expect:** Git warns that you cloned an empty repository. The directory exists and `git status` says `No commits yet`.

**Why:** use HTTPS. Git over SSH to GitLab is not open from the stand's private network. Never put the token into a committed file.

### Step 3. Copy the example into the project

**Do:** copy the whole example, **including hidden files**, from your checkout of the course materials (the folder that contains `gitlab/examples/full-cycle`):

```sh
cp -R /path/to/course-materials/gitlab/examples/full-cycle/. .
ls -A
```

**Expect:** the listing shows `.gitlab-ci.yml`, `.gitlab`, `apps`, `charts`, `ci` and `.gitignore`.

**Why:** `.gitlab-ci.yml` defines the image variables (`BUILDAH_IMAGE`, `SKOPEO_IMAGE` and others) that `.gitlab/ci/web.yml` uses. Copy both files together, or jobs run in the runner's default image and fail with `buildah: not found`.

### Step 4. Commit and push the example

**Do:**

```sh
git switch -c main
git add -A
git commit -m "Lab 8: add the Release Board example"
git push -o ci.skip -u origin main
```

**Expect:** the push succeeds. In GitLab, **Code > Repository** shows the files on branch `main`, and **Build > Pipelines** shows **no** pipeline.

**Why:** `-o ci.skip` stops the first pipeline. It would fail now, because the Agent and the CI variables do not exist yet.

### Step 5. Note the project ID and path

**Do:** open **Settings > General**. The **Project ID** is shown at the top. Copy it, and note the project path from the URL, for example `students/release-board`.

**Expect:** you have the numeric `PROJECT_ID` and the path.

### Step 6. Check that the default branch is protected

**Do:**
1. **Settings > Repository > Branch rules** (called *Protected branches* in older versions): confirm `main` is protected, with merges allowed for Maintainers.
2. **Settings > Merge requests > Merge checks:** enable **Pipelines must succeed**.
3. **Settings > CI/CD > General pipelines:** enable **Prevent outdated deployment jobs**.

**Expect:** `main` is listed as protected, and both settings are saved.

**Why:** protected CI/CD variables (Step 13) reach only protected branches. Limit who can edit CI files or merge, because a deployment token needs trusted project members.

---

## Part B. Give deployments access to one namespace

**What the GitLab Agent is.** A small program (`agentk`) that runs **inside** your kind cluster and connects **outward** to GitLab's agent server (KAS). When a deploy job runs, GitLab hands it a kubeconfig whose context goes through KAS to the Agent. No cluster credentials are stored in CI/CD variables, and the cluster API needs no inbound access. The job can do only what the Agent's Kubernetes service account may do, so this lab gives it an account limited to **one namespace**.

Three names must line up. Keep them identical:

| Where | Value |
|---|---|
| Folder of the Agent configuration (Step 8) | `.gitlab/agents/full-cycle/config.yaml` |
| Name you register in GitLab (Step 9) | `full-cycle` |
| `KUBE_CONTEXT` CI variable (Step 13) | `<project path>:full-cycle` |

> **Already did the Agent lab and your Agent is active?** Check **Operate > Kubernetes clusters**: if an Agent (for example `dtu-agent`) shows **Connected**, you may skip **Step 9 and Step 10** only if that Agent runs with the restricted service account `release-board-agent` (the setting in `ci/agent-values.yaml`). Then use **its name** instead of `full-cycle` in Steps 8 and 13.
> An Agent installed the way the Agent lab does it has **cluster-admin** rights. Do **not** reuse it here: Step 11 would fail, and a deploy job would hold full cluster access. In that case do Steps 7-11 as written. The old Agent can keep running next to the new one, because a job picks the Agent by its context name (`KUBE_CONTEXT`). Remove the old Agent after the lab if you do not need it.
> Steps 7, 8, 11 and 12 onward are always needed.

### Step 7. Install the access chart

**Do:** on your VM, with the administrator kubeconfig, from the cloned project:

```sh
PROJECT_ID=123 # replace with the ID from Step 5
helm upgrade --install full-cycle-access charts/lab-access \
  --namespace gitlab-agent --create-namespace \
  --set targetNamespace="full-cycle-${PROJECT_ID}"
```

**Expect:** the release is `deployed`. Namespaces `gitlab-agent` and `full-cycle-<PROJECT_ID>` exist (`kubectl get ns`).

**Why:** the chart creates a `release-board-agent` service account whose Role covers releases in `full-cycle-<PROJECT_ID>` **only**. It grants no cluster-admin role and cannot create namespaces. It also lets the Agent hold its leader-election lease inside `gitlab-agent`; without that the Agent logs `Error retrieving lease lock` and never serves jobs.

### Step 8. Add the Agent configuration and push it

**Do:** create `.gitlab/agents/full-cycle/config.yaml` with your project path:

```yaml
ci_access:
  projects:
    - id: students/release-board
```

```sh
git add -A
git commit -m "Add the full-cycle agent configuration"
git push -o ci.skip
```

**Expect:** **Code > Repository** shows `.gitlab/agents/full-cycle/config.yaml` on `main`, and still no pipeline.

**Why:** the folder name **must equal the Agent's registered name** (`full-cycle`). GitLab reads `.gitlab/agents/<agent name>/config.yaml`; a different name means no configuration, no authorization and an empty job kubeconfig. `ci_access` lists the projects whose jobs may use the Agent.

### Step 9. Register the Agent in GitLab

**Do:**
1. Check that GitLab's agent server (KAS) answers. From the VM:
   ```sh
   curl -sI https://<gitlab-host>/-/kubernetes-agent/ | head -1
   ```
2. In GitLab: **Operate > Kubernetes clusters > Connect a cluster**.
3. In the dialog, select the Agent **`full-cycle`** from the list (GitLab finds it from the folder you pushed in Step 8). If the dialog has no list, type `full-cycle`.
4. Click **Register**.
5. Copy the **registration token** shown in the dialog, and note the **KAS address** from the install snippet (`--set config.kasAddress=...`).

**Expect:** step 1 prints `HTTP/2 426` (KAS is up; it wants a WebSocket, not a plain request). After step 4 the dialog shows a token that starts with `glagent-`, and the Agent appears in **Operate > Kubernetes clusters** with the status *Never connected*.

**Why:** the token is a secret and GitLab shows it **once**. Never put it in Git, a chat or a command line. If you lose it, open the Agent in **Operate > Kubernetes clusters**, go to its **Access tokens** tab and create a new token. If step 1 does not print `426`, ask the operator: KAS must be enabled on your GitLab.

### Step 10. Install the Agent in the cluster

**Do:** on the VM, in Bash.

1. Find your GitLab version: **Help (?) menu > About GitLab** (or open `https://<gitlab-host>/help`), for example `19.5`.
2. Pick the Agent chart that matches that minor version:
   ```sh
   helm repo add gitlab https://charts.gitlab.io
   helm repo update
   helm search repo gitlab/gitlab-agent --versions | head -5
   ```
   The `APP VERSION` column must match your GitLab minor version (chart `2.33.x` carries Agent `v19.5`). An Agent newer than your GitLab server may fail to connect.
3. Set the chart version and the KAS address, then install. The script asks for the token without echoing it:
   ```sh
   export AGENT_CHART_VERSION=2.33.0                       # from step 2
   export KAS_ADDRESS=wss://<gitlab-host>/-/kubernetes-agent/   # from Step 9, step 5

   : "${AGENT_CHART_VERSION:?Set the compatible Agent chart version}" "${KAS_ADDRESS:?Set the instance KAS address}"
   umask 077
   AGENT_TOKEN_FILE=$(mktemp)
   read -r -s -p 'New Agent registration token: ' AGENT_TOKEN
   printf '\n'
   printf '%s' "$AGENT_TOKEN" > "$AGENT_TOKEN_FILE"
   unset AGENT_TOKEN
   helm upgrade --install full-cycle-agent gitlab-agent \
     --repo https://charts.gitlab.io --version "$AGENT_CHART_VERSION" \
     --namespace gitlab-agent \
     --values ci/agent-values.yaml \
     --set-file config.token="$AGENT_TOKEN_FILE" \
     --set-string config.kasAddress="$KAS_ADDRESS"
   rm -f "$AGENT_TOKEN_FILE"
   unset AGENT_TOKEN_FILE
   ```
4. Check the pod:
   ```sh
   kubectl -n gitlab-agent get pods
   ```

**Expect:** Helm reports `STATUS: deployed`, and the pod `full-cycle-agent-gitlab-agent-v2-...` is `1/1 Running`. If the pod is not ready, read its log: `kubectl -n gitlab-agent logs deploy/full-cycle-agent-gitlab-agent-v2`. See *If something fails* at the end.

**Why:**
- `ci/agent-values.yaml` turns off the chart's own RBAC and tells the Agent to use the restricted service account from Step 7. That is the whole security boundary.
- The token goes through `--set-file` from a private temporary file instead of `--set`, so it never lands in your shell history or the process list. Helm still stores release values in a Secret in `gitlab-agent`, so only trusted people should read Secrets there.
- If GitLab's name does not resolve inside the cluster, add the chart's `hostAliases` with the GitLab private IP and hostname.

### Step 11. Check the Agent and its permission boundary

**Do:**
1. In GitLab: **Operate > Kubernetes clusters**. Look at the status of `full-cycle`.
2. On the VM, with the administrator kubeconfig:

```sh
kubectl auth can-i create deployments -n "full-cycle-${PROJECT_ID}" \
  --as=system:serviceaccount:gitlab-agent:release-board-agent
kubectl auth can-i create deployments -n kube-system \
  --as=system:serviceaccount:gitlab-agent:release-board-agent
kubectl auth can-i create namespaces \
  --as=system:serviceaccount:gitlab-agent:release-board-agent
```

**Expect:** the Agent shows **Connected**, and the three commands print `yes`, `no`, `no`.

**Why:** this proves a deploy job can touch its own namespace and nothing else. Do not reuse a cluster-admin Agent from an earlier exercise: a second, broader Agent for the same cluster defeats the boundary.

---

## Part C. Credentials and CI variables

### Step 12. Create a Deploy Token

**Do:**
1. **Settings > Repository > Deploy tokens > Add token**.
2. **Name:** `release-board-pull`. **Expiration date:** after the end of the course.
3. Scope: tick **only `read_registry`**.
4. Click **Create deploy token** and copy the username and the secret.

**Expect:** GitLab shows the username (like `gitlab+deploy-token-1`) and the secret once.

**Why:** Kubernetes needs a durable credential to pull the image after the pipeline ends. The built-in `CI_JOB_TOKEN` is temporary and must not become a workload pull secret.

### Step 13. Set the CI/CD variables

**Do:** open **Settings > CI/CD > Variables > Add variable** and add each variable below. For every one set **Type: Variable**, **Environments: All**, tick **Protect variable**, and use the *Visibility* shown:

| Variable | Value | Visibility |
|---|---|---|
| `KUBE_CONTEXT` | `<project path>:full-cycle`, for example `students/release-board:full-cycle` | **Visible** |
| `APP_HOST` | `release-board.<public-ip-with-dashes>.sslip.io`, or an operator-provided DNS name (no scheme, no port) | Visible |
| `INGRESS_IP` | your VM's **private** IP, for example `10.42.0.11` | Visible |
| `REGISTRY_PULL_USER` | Deploy Token username from Step 12 | **Masked** |
| `REGISTRY_PULL_PASSWORD` | Deploy Token secret from Step 12 | **Masked** |

**Expect:** the **Variables** list shows five variables, each with the *Protected* badge, and the last two as *Masked*.

**Why:**
- `KUBE_CONTEXT` is `<project holding the Agent configuration>:<agent name>`. Do not mask it: a masked value hides itself in logs and makes failures hard to debug.
- `APP_HOST` must resolve to the VM's public IP from your browser.
- Optional: `INGRESS_PORT` if the private ingress is not on port 80; `INGRESS_CLASS` if the class is not `traefik`. `KUBE_NAMESPACE` defaults to `full-cycle-$CI_PROJECT_ID` and must stay aligned with Step 7.
- Image and chart publication use the built-in `CI_REGISTRY_*` credentials. Merge request jobs need no deployment credentials.

### Step 14. Enable the Container Registry

**Do:** **Settings > General > Visibility, project features, permissions**, switch **Container registry** on, and click **Save changes**.

**Expect:** **Deploy > Container Registry** appears in the left sidebar.

---

## Part D. Ship the baseline and check every result

### Step 15. Validate and run the first pipeline

**Do:**
1. **Build > Pipeline editor**, then the **Validate** tab > **Validate pipeline**.
2. **Build > Pipelines > New pipeline**, branch `main`, then **New pipeline**.
3. Open the pipeline to watch the stages.

**Expect:** the lint says the configuration is valid. The pipeline runs `verify -> build -> scan -> publish -> deploy` and every job is green. Later pushes to `main` start pipelines automatically.

### Step 16. Check the job artifacts

**Do:** open **Build > Pipelines**, click the pipeline, click a job, and use the right-hand panel **Job artifacts > Browse** (or **Download**). To see every artifact of the project at once, open **Build > Artifacts**.

**Expect:** each job produced these files:

| Job | Artifact files | What to look for |
|---|---|---|
| `go-tests` | `apps/web/coverage.out` | Go coverage profile |
| `chart-lint` | `rendered.yaml` | the chart rendered by Helm |
| `security-source` | `security-source.json` | source findings, empty for HIGH/CRITICAL |
| `security-config` | `security-dockerfile.json`, `security-config.json` | Dockerfile and rendered-manifest findings |
| `build-image` | `image.tar` | the saved image (visible to Maintainers only) |
| `scan-image` | `security-image.json`, `sbom.cdx.json` | image vulnerabilities and the CycloneDX SBOM |
| `publish-image` | `release.env` | `IMAGE_DIGEST=sha256:...` |
| `publish-chart` | `dist/release-board-<version>.tgz`, `release.env` | the packaged chart and its version |
| `deploy-student` | `diagnostics/history.txt`, `diagnostics/status.txt`, `smoke-response.json` | Helm history and status, and the smoke-check answer with your commit SHA |

**Why:** the scan reports are ordinary Trivy JSON files, not GitLab security dashboards. Gates fail on HIGH or CRITICAL findings; never mark them optional to get a green pipeline. A new vulnerability database can fail an unchanged image later: update the dependency or base image and build a new release.

### Step 17. Check the image and the chart in the project's Registry

**Do:**
1. Open **Deploy > Container Registry**. The list shows two repositories: `<group>/release-board/web` and `<group>/release-board/charts/release-board`.
2. Click **`web`**. Compare the tag with your commit: **Code > Commits**, copy the full SHA.
3. Click **`charts/release-board`** and read its tag.
4. Open the `publish-image` job log (**Build > Jobs**) and find `IMAGE_DIGEST`.

**Expect:**
- `web` has a tag equal to the **full commit SHA**, and the digest shown in the registry equals `IMAGE_DIGEST` in the log.
- `charts/release-board` has a tag `0.<pipeline IID>.<job ID>`, for example `0.1.42`. It is the same version the Release Board shows in Step 18.

You can check the same from the VM with the Deploy Token (skopeo asks for the secret, so it stays out of your shell history):

```sh
REGISTRY=gitlab.example.com:5050 # your registry host, as in CI_REGISTRY
skopeo login "$REGISTRY" -u "<deploy token username>"
skopeo inspect --format '{{.Digest}}' "docker://$REGISTRY/<group>/release-board/web:<full commit SHA>"
```

**Why:** the pipeline saves the built image, scans that archive and publishes it **without rebuilding**, so the published digest is the scanned one. The chart is stamped with the image digest and its version stays unique on retries. Helm OCI charts appear in the Container Registry, not under *Package Registry*. Registry credentials never enter source values; credentials supplied at deploy time become Kubernetes and Helm release Secrets, so keep access to them restricted.

### Step 18. Open the live application

**Do:** **Operate > Environments**, open **student**, and click **Open**. In the page click **Check again**.

**Expect:** the Release Board shows the full `main` commit and links to the pipeline that deployed it. Its chart version equals the chart tag from Step 17.

**Why:** a green pod alone does not prove the right release reached the browser. The final smoke check verifies the compiled commit through ingress.

---

## Part E. Four break/fix exercises

Do the first three in **separate merge requests**, each starting from the healthy `main`. The helper changes one thing and never commits, pushes or touches the cluster.

**Procedure for every drill:**

```sh
git switch main && git pull
git switch -c drill/<name>
python3 ci/drill.py <name>
git diff
git add <files from the step>
git commit -m "Exercise <name> gate"
DRILL_COMMIT=$(git rev-parse HEAD)
git push -u origin HEAD
```

Then open the merge request: **Code > Merge requests > New merge request**, source `drill/<name>`, target `main`. Look at the failing job in the MR's **Pipelines** tab or in **Build > Pipelines**.

To undo a drill, use a **new** commit (retrying the old job runs the same broken input):

```sh
git revert "$DRILL_COMMIT"
git push
```

**Expect after every revert:** a new pipeline that is green, while the earlier red pipeline stays in **Build > Pipelines** history.

### Step 19. Drill `unit`

**Do:** `python3 ci/drill.py unit`, then commit `apps/web/main.go`.
**Expect:** `go-tests` fails. The API field changes from `version` to `revision`, and the contract test shows why the UI would lose the release identity.

### Step 20. Drill `secret`

**Do:** `python3 ci/drill.py secret`, then commit `apps/web/drill-credentials.txt`.
**Expect:** `security-source` fails on a synthetic GitLab token pattern. The redacted finding in the job log (and in `security-source.json`) names the file to remove.

**Why:** use only the generated fake value. Never commit a real credential. Deleting a leaked password from the latest commit is not enough: revoke or rotate it first, then clean the history. Do not suppress the detector or whitelist the fixture.

### Step 21. Drill `privileged`

**Do:** `python3 ci/drill.py privileged`, then commit `charts/release-board/values.yaml`.
**Expect:** `security-config` fails. Helm renders fine, but the workload requests privileged access (a HIGH finding in `security-config.json`).

### Step 22. Drill `rollout`

**Do:**
1. First confirm a successful baseline deploy (Step 18).
2. `python3 ci/drill.py rollout`, then commit `charts/release-board/values.yaml`.
3. Open the MR: the static checks **pass**, because `/not-ready` is valid but operationally wrong. Merge it into this disposable environment.
4. Watch `deploy-student` in **Build > Pipelines**. After about three minutes it fails.
5. Inspect from the VM:

```sh
helm history release-board -n "full-cycle-${PROJECT_ID}"
kubectl get pods -n "full-cycle-${PROJECT_ID}"
kubectl get events -n "full-cycle-${PROJECT_ID}" --sort-by=.lastTimestamp
```

**Expect:** the pipeline is red, Helm history shows the failed revision and a rollback, and the old ready replicas keep serving the **old** commit (**Operate > Environments > student > Open**). Fix it with a revert MR, merge, and check a fresh green deployment and the new commit in the browser.

**Why:** Helm rolls back failed rollouts. If Helm tests or the ingress commit check fail after a successful rollout, `ci/deploy.sh` rolls back to the previous revision too. A first deployment has no healthy predecessor, so a failed check removes the installation. A project resource group stops two pipelines from updating the environment at once. Never bypass the readiness check or raise the timeout to hide the defect.

---

## Part F. Evidence and cleanup

### Step 23. Collect evidence

**Do:** keep one screenshot or URL showing the live commit, the successful pipeline (**Build > Pipelines**), the image digest (**Deploy > Container Registry**), the chart version and the SBOM artifact. Keep links to the four failed pipelines and their fixed successors. Explain which gate stopped each defect and why static validation could not catch the bad readiness endpoint.

**Expect:** a short set of links and screenshots you can hand in.

### Step 24. Clean up

**Do:**
1. **Operate > Environments > student > Stop**, while the Agent and credentials still work.
2. Confirm the application is gone: `kubectl get pods -n "full-cycle-${PROJECT_ID}"` shows none.
3. Revoke the Deploy Token: **Settings > Repository > Deploy tokens > Revoke**.
4. Unregister the Agent: **Operate > Kubernetes clusters**, open the `full-cycle` row menu and choose **Delete**.
5. On the VM: `helm uninstall full-cycle-agent -n gitlab-agent` and `helm uninstall full-cycle-access -n gitlab-agent`.
6. After checking that it holds nothing you need, delete **only** `full-cycle-<PROJECT_ID>`: `kubectl delete namespace "full-cycle-${PROJECT_ID}"`.

**Expect:** no release in `gitlab-agent`, no application pods and no Deploy Token.

**Why:** the access chart keeps the namespace on purpose when uninstalled. Never delete the shared kind cluster.

---

## Variant B. Your own computer (local kind)

Use this instead of the operator-provided stand. Most steps are the same: only the cluster, the ingress, the runner and a few values differ. The table at the end of this section lists every step that changes.

**Needs:** `kind`, `kubectl`, `helm`, and Podman or Docker; a GitLab project reachable from your computer over HTTPS with a trusted certificate (Steps 1-6).

> **Safety:** your machine may have other kubectl contexts (work clusters). Before every `kubectl` or `helm` command of this lab, run `kubectl config current-context` and confirm it is your kind cluster, for example `kind-release-board`. The commands below add `--context kind-release-board` to be explicit.

### Step B1. Create the kind cluster with one published port

**Do:** save as `kind-config.yaml` and create the cluster:

```yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
    extraPortMappings:
      - containerPort: 30000   # Traefik's web port, see Step B2
        hostPort: 30000
        listenAddress: "127.0.0.1"
  - role: worker
```

```sh
kind create cluster --name release-board --config kind-config.yaml
kubectl --context kind-release-board get nodes
```

With Podman, prefix the first command with `KIND_EXPERIMENTAL_PROVIDER=podman`.

**Expect:** all nodes are `Ready`. Port `30000` on your computer now reaches the control-plane node.

### Step B2. Install Traefik

**Do:**

```sh
helm repo add traefik https://traefik.github.io/charts
helm upgrade --install traefik traefik/traefik --version 41.7.1 \
  --kube-context kind-release-board --namespace traefik --create-namespace \
  --wait --timeout 5m -f - <<'EOF'
deployment:
  replicas: 1
service:
  type: NodePort
ports:
  web:
    nodePort: 30000
ingressClass:
  isDefaultClass: true
providers:
  kubernetesIngress:
    publishedService:
      enabled: false
EOF
kubectl --context kind-release-board get ingressclass
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:30000/
```

**Expect:** `traefik` is listed as the default IngressClass, and `curl` prints `404` (Traefik answers, no route exists yet).

**Why:** kind has no cloud load balancer, so Traefik's web entry point is a **NodePort**, and Step B1 published that port to your computer.

### Step B3. Install a runner in the local cluster

**Do:**
1. In GitLab: **Settings > CI/CD > Runners > Create project runner**. Tag: `kubernetes`. Click **Create runner** and copy the token (`glrt-...`).
2. Install the runner with that token (read without echo, stored in a Secret):

```sh
read -r -s -p 'Runner token: ' RUNNER_TOKEN; printf '\n'
kubectl --context kind-release-board create namespace gitlab-runner
printf '%s' "$RUNNER_TOKEN" | kubectl --context kind-release-board -n gitlab-runner \
  create secret generic local-runner-token \
  --from-file=runner-token=/dev/stdin --from-literal=runner-registration-token=""
unset RUNNER_TOKEN

helm repo add gitlab https://charts.gitlab.io
helm upgrade --install local-runner gitlab/gitlab-runner \
  --kube-context kind-release-board --namespace gitlab-runner --wait --timeout 5m -f - <<'EOF'
gitlabUrl: https://<gitlab-host>
runners:
  secret: local-runner-token
  config: |
    [[runners]]
      [runners.kubernetes]
        namespace = "gitlab-runner"
        image = "alpine:3.22"
        privileged = false
rbac:
  create: true
EOF
```

3. **Apple Silicon / arm64 computers only.** The runner defaults to an x86_64 helper image that cannot start on arm64 nodes, so every job fails in *Preparing environment* with `ImagePullBackOff` for `gitlab-runner-helper:x86_64-...`. Add one line under `privileged = false` in the values of step 2, using your runner version (`helm show chart gitlab/gitlab-runner | grep appVersion`), then run the same `helm upgrade --install` again:

   ```toml
   helper_image = "registry.gitlab.com/gitlab-org/gitlab-runner/gitlab-runner-helper:arm64-v<runner version>"
   ```

   On amd64 (Intel, Linux, Windows) skip this step. Check your architecture with `kubectl --context kind-release-board get nodes -o jsonpath='{.items[0].status.nodeInfo.architecture}'`.
4. Make sure **only this runner** takes the project's jobs: **Settings > CI/CD > Runners**, and **turn off** *Enable instance runners for this project* (and *Enable group runners*, if shown).

**Expect:** the runner pod is `Running` in `gitlab-runner`, **Settings > CI/CD > Runners** lists the runner as **online** with tag `kubernetes`, and no other runner is enabled for the project.

**Why:** the runner is unprivileged, like the stand's. Job pods start in namespace `gitlab-runner`.

**The runner must run next to the cluster.** The deploy job's smoke check opens `INGRESS_IP:INGRESS_PORT` **from the job pod**. `INGRESS_IP` is a node address of your local kind network, which only pods inside your computer's cluster can reach. If a stand (instance or group) runner takes the job instead, the check fails with `Failed to connect ... after 0 ms`, even though Traefik is fine. To see which runner took a job, open the job: the right-hand panel shows the runner, and the first log lines say *Running on ... via ...*.

### What changes in the other steps

| Step | Variant B |
|---|---|
| **7** access chart, **10** Agent install, **11** checks | Run them against your kind cluster: add `--kube-context kind-release-board` to every `helm` and `kubectl` command. Your kind context is the "administrator kubeconfig". |
| **10** KAS address | The same public address from Step 9 (`wss://<gitlab-host>/-/kubernetes-agent/`). Your cluster needs working DNS and Internet access to reach it. |
| **13** variable `INGRESS_IP` | The control-plane node's IP on the kind network: `kubectl --context kind-release-board get nodes -o wide`, column `INTERNAL-IP` of the control-plane node, for example `10.89.0.4`. |
| **13** variable `INGRESS_PORT` | **`30000`** (new variable, protected). Without it the smoke check tries port 80 and fails. |
| **13** variable `APP_HOST` | `release-board.127.0.0.1.sslip.io` (it resolves to your computer). Do not add the port to the variable. |
| **18** open the application | `http://release-board.127.0.0.1.sslip.io:30000`. The link GitLab shows in the environment lacks `:30000`, so add it by hand. |
| **22** commands on the VM | Run them against your kind context. |
| **24** cleanup | Also remove the runner, Traefik and the cluster: `helm uninstall local-runner -n gitlab-runner`, `helm uninstall traefik -n traefik`, `kind delete cluster --name release-board`. Delete the runner in **Settings > CI/CD > Runners** too. |

**Expect:** the pipeline passes exactly like on the stand, and the Release Board opens at the address above.

---

## If something fails

| Symptom in the job or Agent log | Cause | Fix |
|---|---|---|
| `buildah: not found` or `skopeo: not found` | `BUILDAH_IMAGE` / `SKOPEO_IMAGE` are not set, so the job ran in the runner's default image | Copy `.gitlab-ci.yml` together with `.gitlab/ci/web.yml` (Step 3) |
| `Cannot connect to the Docker daemon at tcp://docker:2375` | An old `.gitlab/ci/web.yml` that still uses `docker:dind` | Use the current example: it needs no Docker daemon |
| `kubernetes cluster unreachable: context "..." does not exist` | The job's kubeconfig has no Agent context: wrong `KUBE_CONTEXT`, config folder not named after the Agent, project not in `ci_access`, or an unprotected branch | Check Steps 8, 9 and 13: folder `.gitlab/agents/<agent name>/`, `KUBE_CONTEXT=<project path>:<agent name>`, protected branch. To inspect, add `echo "$KUBECONFIG"; grep -E '^\s+name:' "$KUBECONFIG"` to the job script. |
| Agent log: `Config: ref or repository not found` | GitLab found no `.gitlab/agents/<agent name>/config.yaml` on the default branch | Fix the folder name or push the file (Step 8) |
| Agent log: `Error retrieving lease lock ... forbidden` | The access chart is older than Step 7 | `helm upgrade` the `lab-access` chart from the current example |
| Agent log: `lookup <gitlab-host> ... connection refused` | Cluster DNS (CoreDNS) is down | `kubectl -n kube-system get pods -l k8s-app=kube-dns` must show `Running` pods |
| `publish-image`: `denied` or `unauthorized` | Registry disabled (Step 14), or the job did not run on the default branch | Enable the Container Registry; merge request pipelines never publish |
| `publish-image`: `x509` or `no such host` | The registry name or certificate does not work from the runner | Ask the operator to fix the stand; never add skip-TLS flags |
| Every job fails in *Preparing environment* with `ImagePullBackOff` for `gitlab-runner-helper:x86_64-...` on a **local arm64** cluster (Variant B) | The runner's default helper image is x86_64 | Add the arm64 `helper_image` line from Step B3 |
| `deploy-student`: `Failed to connect ... via <node ip>:30000 after 0 ms` with a **local** cluster (Variant B), while `curl` from your computer works | The job ran on another runner (a stand or instance runner) that cannot reach your cluster's node IP | Install the local runner (Step B3) and turn off instance and group runners for the project |
| `deploy-student`: `Failed to connect ... :80` or `Ingress did not serve the expected commit` | Wrong `APP_HOST`, `INGRESS_IP` or `INGRESS_PORT`, or no ingress class | Check Step 13 and `kubectl get ingressclass`. If the ingress listens on another port (for example a NodePort), set `INGRESS_PORT`. |

Reference behavior: [GitLab Kubernetes executor](https://docs.gitlab.com/runner/executors/kubernetes/), [Agent CI access](https://docs.gitlab.com/user/clusters/agent/ci_cd_workflow/), [Deploy Token scopes](https://docs.gitlab.com/user/project/deploy_tokens/), [Helm OCI registries](https://helm.sh/docs/topics/registries/).
