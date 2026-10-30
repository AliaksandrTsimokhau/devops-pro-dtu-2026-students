# Lab 8: ship and recover a real web release

Build a small monorepo containing a Go web application, its browser UI, Helm charts and CI configuration. The result is a live Release Board showing the deployed commit, environment, chart version and pipeline link. Budget 2-3 hours after runner and Agent setup.

Start with [Servercore onboarding](servercore_gitlab.md), [Runner](gitlab_runner_lab.md), [Trivy](gitlab_ci_lab_basic_trivy_scan.md) and [Agent](gitlab_agent_k8s_lab.md). Helm knowledge from the [Helm deployment lab](../helm/helm_gitlab_cicd_lab.md) helps.

## What the pipeline proves

| Gate | Practical purpose |
|---|---|
| Go formatting, vet, race tests; browser tests | Preserve the API contract and usable failure feedback |
| Helm lint, values schema, Kubernetes schema | Reject invalid release configuration before deployment |
| Trivy source, Dockerfile and rendered manifest scans | Block leaked credentials and unsafe workloads |
| Build once, scan the saved image, produce an SBOM | Publish the same bytes that passed the image scan |
| GitLab image and Helm OCI publication | Keep immutable release inputs together |
| Helm rollout, Helm test and ingress smoke check | Verify readiness, service routing and the actual deployed commit |
| Serialized deploys and automatic rollback | Keep a failed release from replacing the last working version |

The Kubernetes executor runs jobs in the student's kind cluster. Buildah builds the image without a Docker daemon and saves it as an archive; skopeo publishes that same archive. GitLab Agent gives deployment jobs access through a namespace-scoped service account. Traefik serves the application on the VM. The Helm chart uses a digest-pinned private image, two replicas, probes, resource requests, a disruption budget and a non-root container with a read-only filesystem.

## 1. Prepare the project and stand

Create an empty private GitLab project. Copy the complete example, including hidden files, from your local student materials checkout into a clone of that project:

```sh
cp -R /path/to/devops-pro-dtu-2026-students/gitlab/examples/full-cycle/. /path/to/project/
cd /path/to/project
```

The example source is [gitlab/examples/full-cycle](examples/full-cycle). Its layout is `apps/web`, `charts/release-board`, `charts/lab-access`, `ci` and `.gitlab/ci`. Application and deployment changes trigger branch pipelines. Merge requests always run validation, including documentation-only changes and reverted drills with an empty diff. Use a code change for each exercise.

Ask the operator to check these stand prerequisites before the first push:

- The runner has tag `kubernetes`, accepts this project's branches and uses the Kubernetes executor. The runner stays unprivileged: the pipeline needs no Docker-in-Docker, no `docker:dind` service and no privileged pods.
- GitLab and its Registry have trusted HTTPS certificates and resolve correctly from runner jobs and kind nodes. Do not add insecure-registry or skip-TLS flags.
- Traefik is installed in kind. The VM has a stable public address and ingress ports exposed to the student's allowed source CIDRs: `lab_public_ip_mode = "static"`, `lab_ingress_ports = [80, 443]` in the stand configuration. Apply infrastructure changes through the operator's normal workflow.
- Jobs can reach the VM's private ingress address. Browsers reach its public address. Do not use the public address for CI's internal smoke check if the cloud network lacks hairpin routing.

Protect the default branch, use merge requests, require successful pipelines before merging and enable GitLab's prevention of outdated deployment jobs. Limit who can edit CI or merge changes. A deployment token requires trusted project members.

## 2. Give deployments access to one namespace

Find the numeric project ID in GitLab. On the student VM, use its administrator kubeconfig once to install the access chart from the copied project:

```sh
PROJECT_ID=123 # replace
helm upgrade --install full-cycle-access charts/lab-access \
  --namespace gitlab-agent --create-namespace \
  --set targetNamespace="full-cycle-${PROJECT_ID}"
```

This creates the application namespace and a `release-board-agent` service account in `gitlab-agent`. Its Role permits release management only in `full-cycle-<PROJECT_ID>`. It grants no cluster administrator role and cannot create namespaces.

Create `.gitlab/agents/full-cycle/config.yaml`, replacing the project path:

```yaml
ci_access:
  projects:
    - id: students/your-project
```

Commit the copied example and this configuration, then push the initial default branch with `git push -o ci.skip`. This one-time bootstrap publishes the Agent configuration before deployment credentials exist. Then register an Agent named `full-cycle` in GitLab's Kubernetes integration. Follow the [Agent lab](gitlab_agent_k8s_lab.md) for token handling and the KAS address shown by your instance. Install a separate Agent with the supplied `ci/agent-values.yaml`; it disables chart-created RBAC and uses the restricted service account above:

```sh
# Run this block in Bash on the VM.
# Set AGENT_CHART_VERSION to a pinned version compatible with your GitLab,
# and KAS_ADDRESS to the address shown by your instance.
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

Use the onboarding procedure's private hostname routing when the GitLab hostname cannot resolve internally. If necessary, pass the Agent chart's `hostAliases` with the GitLab private IP and hostname. Never paste the token into committed values or upload Helm release values as artifacts. Remove the temporary token file after installation.

Verify the Agent is connected. From the VM administrator context, verify the permission boundary:

```sh
kubectl auth can-i create deployments -n "full-cycle-${PROJECT_ID}" \
  --as=system:serviceaccount:gitlab-agent:release-board-agent
kubectl auth can-i create deployments -n kube-system \
  --as=system:serviceaccount:gitlab-agent:release-board-agent
kubectl auth can-i create namespaces \
  --as=system:serviceaccount:gitlab-agent:release-board-agent
```

Expect `yes`, `no`, `no`. Do not reuse a cluster-admin Agent from an earlier exercise.

## 3. Configure registry credentials and CI variables

Create a project Deploy Token with **only `read_registry`**, an expiry covering the course and a recognizable name. Kubernetes needs this durable credential after the pipeline finishes. The built-in `CI_JOB_TOKEN` is temporary and must not become a workload pull secret.

Add these project CI/CD variables:

| Variable | Example / meaning | Protection |
|---|---|---|
| `KUBE_CONTEXT` | `students/your-project:full-cycle` | Protected |
| `APP_HOST` | `release-board.<public-ip-with-dashes>.sslip.io`, or an operator-provided DNS name | Protected |
| `INGRESS_IP` | This student's VM private IP, for example `10.42.0.11` | Protected |
| `REGISTRY_PULL_USER` | Deploy Token username | Masked, protected |
| `REGISTRY_PULL_PASSWORD` | Deploy Token secret | Masked, protected |

The hostname must resolve to the VM public IP from your browser. Set `INGRESS_PORT` only if the private ingress uses a port other than 80. Check `kubectl get ingressclass`; set `INGRESS_CLASS` if the class name differs from the default `traefik`. `KUBE_NAMESPACE` defaults to `full-cycle-$CI_PROJECT_ID`; keep it aligned with the access chart. Protect the default branch so it receives protected variables. MR verification jobs do not need deployment credentials.

Image and chart publication use built-in `CI_REGISTRY_*` credentials. The image is stored at `$CI_REGISTRY_IMAGE/web`; the chart at `oci://$CI_REGISTRY_IMAGE/charts/release-board`. Both appear in GitLab's **Container Registry**: this lab uses Helm OCI artifacts, rather than GitLab's older Helm package API. Enable the project's Container Registry.

## 4. Ship the healthy baseline

Validate the combined configuration with GitLab CI Lint. After the Agent and all variables are ready, use **Build > Pipelines > New pipeline** on the default branch to run the initial release. Subsequent application pushes create pipelines automatically. The jobs progress through `verify -> build -> scan -> publish -> deploy`. Only the default branch publishes and deploys.

Inspect the source and configuration scan reports, image vulnerability report and CycloneDX SBOM in artifacts. These are ordinary Trivy JSON reports, not GitLab-specific security dashboard reports. Security gates fail on HIGH or CRITICAL findings; do not mark them optional to obtain a green pipeline. A new vulnerability database can make an unchanged image fail later: update the dependency or base image and build a new release.

The pipeline saves the built image, scans that archive and publishes it without rebuilding. The chart is copied to a staging directory and stamped with the image digest. Its version `0.<pipeline IID>.<job ID>` stays unique on retries. Source values never receive registry credentials. Credentials supplied during deployment become Kubernetes and Helm release Secrets; keep access to those Secrets restricted.

Confirm that the image reached your project's registry. Open **Deploy > Container Registry**: the repository `<your-project>/web` must have a tag equal to the full commit SHA, and its digest must match `IMAGE_DIGEST` in the `publish-image` job log. The chart appears under `<your-project>/charts/release-board` after `publish-chart`. From the VM, check the same thing with the Deploy Token's `read_registry` access (skopeo asks for the token, so it stays out of your shell history):

```sh
REGISTRY=gitlab.example.com:5050 # replace with your registry host, as in CI_REGISTRY
skopeo login "$REGISTRY" -u "<deploy token username>"
skopeo inspect --format '{{.Digest}}' "docker://$REGISTRY/<group>/<project>/web:<full commit SHA>"
```

If `publish-image` fails, read the last lines of its log: `denied: requested access to the resource is denied` or `unauthorized` means the project's Container Registry is disabled or the job did not run on the default branch; `x509` or `no such host` means the registry name or certificate does not work from the runner, so fix that in the stand instead of adding skip-TLS flags. Jobs on merge request branches never publish.

Open GitLab's `student` environment URL. The board must show the full default-branch commit and link to the pipeline that deployed it. Click **Check again**. Compare its chart version with the OCI chart tag. A green pod alone does not prove that the correct release reached the browser: the final smoke check verifies the compiled commit through ingress.

## 5. Four purposeful break/fix exercises

Complete the first three in separate merge requests. Start each from the healthy default branch. The helper changes one thing and never commits, pushes or touches the cluster:

```sh
git switch -c drill/unit
python3 ci/drill.py unit
git diff
# Add only the files changed by the drill, inspect, commit and push.
git add apps/web/main.go
git commit -m "Exercise API contract gate"
DRILL_COMMIT=$(git rev-parse HEAD)
git push -u origin HEAD
```

After observing the expected failure, revert that exercise commit and push the fix:

```sh
git revert "$DRILL_COMMIT"
git push
```

The new commit creates a new pipeline. Retrying the failed job on the old commit runs the same broken input. Check that the new pipeline passes and the previous failure remains understandable in history.

| Drill command | Commit these files | Expected failure and useful evidence |
|---|---|---|
| `python3 ci/drill.py unit` | `apps/web/main.go` | `go-tests`: API field changes from `version` to `revision`; contract test shows why the UI would lose release identity |
| `python3 ci/drill.py secret` | `apps/web/drill-credentials.txt` | `security-source`: synthetic GitLab PAT pattern; redacted finding shows which file must be removed |
| `python3 ci/drill.py privileged` | `charts/release-board/values.yaml` | `security-config`: Helm renders successfully, but the workload requests privileged access; inspect the HIGH finding |
| `python3 ci/drill.py rollout` | `charts/release-board/values.yaml` | `deploy-student`: an incorrect readiness path prevents rollout; Helm restores the last healthy release |

For the secret drill, use only the generated fake value. Never commit a real credential. Removing a genuine leaked password from the latest commit is insufficient: revoke or rotate it first, then follow your repository's history-cleanup procedure. Do not suppress the detector or whitelist the fake fixture to pass the exercise.

For the **rollout** drill, first verify a successful baseline deployment. Open its MR and observe that static checks pass: `/not-ready` is syntactically valid but operationally wrong. Merge this defect only into this disposable training environment. Watch the deploy job time out after about three minutes, then inspect Helm history and pod events from the VM:

```sh
helm history release-board -n "full-cycle-${PROJECT_ID}"
kubectl get pods -n "full-cycle-${PROJECT_ID}"
kubectl get events -n "full-cycle-${PROJECT_ID}" --sort-by=.lastTimestamp
```

The old ready replicas must continue serving the old commit. The pipeline must stay red. Fix the readiness path through a revert MR, merge and verify a fresh green deployment and the new commit in the browser. Do not bypass the readiness check or increase the timeout to hide the defect.

Helm handles rollout failures with rollback. If Helm tests or the ingress commit check fail after a successful rollout, `ci/deploy.sh` rolls back to the previous revision as well. On a first deployment there is no healthy predecessor; a failed post-deployment check removes the failed installation. Deployments use a project resource group so two pipelines cannot update the environment together.

## Completion evidence and cleanup

Keep one screenshot or URL showing the live commit, the successful pipeline, image digest, chart version and SBOM artifact. Keep links to the four failed pipelines and their fixed successors. Explain which gate prevented each defect from reaching users and why static validation could not detect the bad readiness endpoint.

Use the environment's **Stop** action while the Agent and credentials still work. Confirm the application is gone, then revoke the Deploy Token and unregister the Agent. The access chart intentionally keeps the namespace when uninstalled; after checking it contains no needed resources, remove the access and Agent releases and delete only this project's namespace. Do not delete the shared kind cluster.

Reference behavior: [GitLab Kubernetes executor](https://docs.gitlab.com/runner/executors/kubernetes/), [Agent CI access](https://docs.gitlab.com/user/clusters/agent/ci_cd_workflow/), [Deploy Token scopes](https://docs.gitlab.com/user/project/deploy_tokens/), [Helm OCI registries](https://helm.sh/docs/topics/registries/).
