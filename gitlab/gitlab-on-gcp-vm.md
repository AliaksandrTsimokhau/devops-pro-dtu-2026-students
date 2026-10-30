# GitLab CE on GCP VM + kind Cluster Runner

> **Optional alternative.** The Servercore stand already gives you GitLab CE with trusted Let's Encrypt HTTPS, a registry on port 5050, an unprivileged Kubernetes runner and a kind cluster on your VM. You do **not** need this lab for the other labs. Do it only if you want your own GitLab on GCP.
>
> **Cost warning.** The VM (`e2-standard-4`, about 0.15 USD/hour) and its 50 GB SSD bill until you delete them. Do the **Cleanup** part at the end. Use `<PROJECT>`, `<NETWORK>`, `<SUBNET>`, `<ZONE>`, `<REGION>`, `<GITLAB_HOST>` as placeholders for your own values. This lab installs its own runner in the namespace `gitlab-runner-gcp` (release `gcp-runner`) so it does not touch the stand runner in `gitlab-runner`.

## What you will build

A self-hosted GitLab CE instance running on a Google Cloud VM, with a GitLab Runner installed inside a local **kind** Kubernetes cluster. By the end, you can push a `.gitlab-ci.yml` and watch CI jobs run as pods inside your kind cluster.

---

## Prerequisites

| Tool | Purpose |
|---|---|
| `gcloud` CLI authenticated | Create the VM and SSH into it |
| `kubectl` pointed at a kind cluster | Install the runner |
| `helm` v3 | Install the GitLab Runner Helm chart |
| A dynamic-DNS hostname (`<GITLAB_HOST>`) | Expose GitLab under a stable URL |
| A GCP project with billing and the Compute API | `gcloud services enable compute.googleapis.com --project=<PROJECT>` |

**Kind cluster note:** if your cluster runs on Apple Silicon (arm64), you must override the runner helper image (covered below).

---

## Part 1 — Create the GCP VM

### 1.1 Find the network details

```bash
gcloud compute networks list --project=<PROJECT>
gcloud compute networks subnets list \
  --project=<PROJECT> \
  --network=<NETWORK>
```

### 1.2 Create the VM

```bash
gcloud compute instances create demo-test \
  --project=<PROJECT> \
  --zone=<ZONE> \
  --machine-type=e2-standard-4 \
  --description="delete me if you want" \
  --image-family=ubuntu-2204-lts \
  --image-project=ubuntu-os-cloud \
  --boot-disk-size=50GB \
  --boot-disk-type=pd-ssd \
  --network=<NETWORK> \
  --subnet=<SUBNET> \
  --tags=gitlab-server,http-server,https-server \
  --metadata=startup-script='#!/bin/bash
set -euxo pipefail
exec > /var/log/gitlab-install.log 2>&1

apt-get update -y
apt-get install -y curl openssh-server ca-certificates tzdata perl

curl -fsSL https://packages.gitlab.com/install/repositories/gitlab/gitlab-ce/script.deb.sh | bash

EXTERNAL_URL="http://<GITLAB_HOST>" apt-get install -y gitlab-ce

echo "GitLab CE install complete"
'
```

**Machine sizing:** GitLab needs a minimum of 4 vCPU + 4 GB RAM.  `e2-standard-4` (4 vCPU, 16 GB) gives comfortable headroom for runners and background jobs.

Note the external IP printed at the end — you will need it to set up DNS and for the runner config. The IP is ephemeral: it changes if you stop and start the VM, and DNS then breaks. To keep it, reserve it with `gcloud compute addresses create gitlab-ip --region=<REGION> --project=<PROJECT>` and pass `--address=gitlab-ip` to `instances create` (delete it in Cleanup).

> The first install is plain HTTP, so keep the firewall narrow (next step) and switch to HTTPS in Part 6 **before** you put real tokens in. Until then, anyone who reaches port 80 can see the traffic.

### 1.3 Open ports

Do not open SSH or the fresh GitLab to the whole internet. Allow only your own IP:

```bash
MY_IP=$(curl -s https://ifconfig.me)
gcloud compute firewall-rules create gitlab-server-allow \
  --project=<PROJECT> \
  --network=<NETWORK> \
  --direction=INGRESS \
  --action=ALLOW \
  --rules=tcp:22,tcp:80,tcp:443 \
  --target-tags=gitlab-server \
  --source-ranges="${MY_IP}/32" \
  --description="SSH, HTTP, HTTPS to GitLab from my IP"
```

**Expect:** the rule is listed by `gcloud compute firewall-rules list --project=<PROJECT> --filter=name=gitlab-server-allow`.

Let's Encrypt (Part 6) needs port 80 reachable from the internet. Open it to `0.0.0.0/0` only for that step, and restrict it again after the certificate is issued. Alternatively, use IAP for SSH (`--source-ranges=35.235.240.0/20` and `gcloud compute ssh --tunnel-through-iap`) instead of your IP on port 22. If your IP changes, update the rule with `gcloud compute firewall-rules update`.

### 1.4 Point DNS

Log in to your dynamic-DNS provider and create an **A record**:

```
<GITLAB_HOST>  →  <EXTERNAL_IP>
```

GitLab bakes the external URL into every generated link at first reconfigure, so DNS must match before you open the web UI for the first time.

### 1.5 Wait for install to finish

```bash
gcloud compute ssh demo-test \
  --project=<PROJECT> \
  --zone=<ZONE>

# Inside the VM:
tail -f /var/log/gitlab-install.log
# Wait for: "GitLab CE install complete"
```

---

## Part 2 — First login and basic configuration

### 2.1 Get the one-time root password

```bash
# On the VM:
sudo cat /etc/gitlab/initial_root_password
```

This file is **auto-deleted 24 hours** after installation. Copy the password now.

Open `http://<GITLAB_HOST>`, log in as `root`, and **change the root password** right away (User settings > Password).

### 2.2 Disable public sign-ups

Admin Area → Settings → General → **Sign-up restrictions** → uncheck **Sign-up enabled** → Save.

Or via API (useful for scripted setup):

```bash
curl -X PUT "http://<EXTERNAL_IP>/api/v4/application/settings" \
  -H "PRIVATE-TOKEN: $GL_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"signup_enabled": false}'
```

### 2.3 Create an admin API token (scripted setup)

If you want to configure GitLab without using the web UI, create a short-lived Personal Access Token. The simplest way is the UI: **User settings > Access tokens**, scope `api`, expiry in 2 days. To script it on the VM:

```bash
sudo gitlab-rails runner "
  user = User.find_by_username('root')
  token = user.personal_access_tokens.create!(
    scopes: ['api'],
    name: 'admin-setup-token',
    expires_at: 2.days.from_now
  )
  puts token.token
"
```

> **Verify:** on recent GitLab versions (17.x and newer, so also 19.x) `create!` may fail with an error about a missing organization. If it does, create the token in the UI instead, or check the GitLab docs "Create a personal access token programmatically" for the exact attributes of your version.

Keep the token in a shell variable, not in the command text, so it does not land in the shell history:

```bash
read -rs GL_TOKEN && export GL_TOKEN   # paste the token, press Enter
```

Use `$GL_TOKEN` as `PRIVATE-TOKEN` in all API calls below. **Revoke the token at the end** (see Cleanup).

### 2.4 Create a group and project

Via API:

```bash
# Group
curl -X POST "http://<EXTERNAL_IP>/api/v4/groups" \
  -H "PRIVATE-TOKEN: $GL_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "devops-dtu", "path": "devops-dtu", "visibility": "private"}'

# Project inside the group (use namespace_id from the group response)
curl -X POST "http://<EXTERNAL_IP>/api/v4/projects" \
  -H "PRIVATE-TOKEN: $GL_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "hello-gitlab",
    "namespace_id": <GROUP_ID>,
    "visibility": "private",
    "initialize_with_readme": true
  }'
```

---

## Part 3 — Create a runner token

GitLab 16+ uses token-based runner registration. Create an instance runner via API:

```bash
curl -X POST "http://<EXTERNAL_IP>/api/v4/user/runners" \
  -H "PRIVATE-TOKEN: $GL_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "runner_type": "instance_type",
    "description": "kind-cluster-runner",
    "tag_list": ["kind", "kubernetes"],
    "run_untagged": true
  }'
```

Save the `token` field from the response — it starts with `glrt-` and is shown once. Put it only in `runner-values.yaml` (next part) and do not commit that file.

---

## Part 4 — Install GitLab Runner in kind

### 4.1 Add the Helm repository

```bash
helm repo add gitlab https://charts.gitlab.io
helm repo update gitlab
```

### 4.2 Create namespace

```bash
kubectl create namespace gitlab-runner-gcp
```

This is a separate namespace on purpose: the stand runner of your VM lives in `gitlab-runner`, and this lab must not overwrite or delete it.

### 4.3 Write Helm values

Create `runner-values.yaml`:

```yaml
gitlabUrl: http://<EXTERNAL_IP>

runnerToken: "glrt-<YOUR_TOKEN>"

rbac:
  create: true

serviceAccount:
  create: true

runners:
  config: |
    [[runners]]
      # Use the IP so kind pods can reach GitLab without DNS resolution.
      # GitLab's external_url uses the hostname, but git clone inside the
      # job pod would fail if that hostname doesn't resolve inside the cluster.
      clone_url = "http://<EXTERNAL_IP>"
      [runners.kubernetes]
        namespace = "{{.Release.Namespace}}"
        image = "alpine:3.22"
        pull_policy = ["if-not-present"]
        privileged = false
        # Required on Apple Silicon (arm64): the runner auto-detects its own arch
        # for the helper image tag, but must set it explicitly for Kubernetes jobs
        # because the helper sidecar is pulled by the kind node (also arm64).
        helper_image = "registry.gitlab.com/gitlab-org/gitlab-runner/gitlab-runner-helper:arm64-v<RUNNER_VERSION>"
```

Take `<RUNNER_VERSION>` from the chart you install: `helm show chart gitlab/gitlab-runner | grep appVersion` (for example `appVersion: 19.1.0` gives `arm64-v19.1.0`). On x86_64 hosts remove the `helper_image` line.

> **`clone_url`** — overrides the URL the runner uses for `git clone`. Without this, the runner uses GitLab's `external_url` (the hostname), which doesn't resolve inside kind pods when DNS isn't configured for that hostname.
>
> **`helper_image`** — the helper is a sidecar container that handles repo cloning and artifact upload. It is a platform-specific binary. On `arm64` hosts (Apple Silicon), the runner must pin this to the `arm64` tag; the default `x86_64` tag will fail with `no match for platform in manifest`.

### 4.4 Install

```bash
helm upgrade --install gcp-runner gitlab/gitlab-runner \
  --namespace gitlab-runner-gcp \
  --values runner-values.yaml \
  --wait
```

### 4.5 Verify

```bash
# Pod running
kubectl get pods -n gitlab-runner-gcp

# Runner online in GitLab
curl -s "http://<EXTERNAL_IP>/api/v4/runners/all" \
  -H "PRIVATE-TOKEN: $GL_TOKEN" \
  | python3 -c "
import sys, json
for r in json.load(sys.stdin):
    print(r['description'], '|', r['status'])
"
```

---

## Part 5 — Test with a CI pipeline

Push `.gitlab-ci.yml` to your project (via UI or API):

```yaml
stages:
  - test

hello-world:
  stage: test
  script:
    - echo Hello from kind cluster runner
    - date
    - hostname
```

Via API:

```bash
curl -X POST "http://<EXTERNAL_IP>/api/v4/projects/<PROJECT_ID>/repository/files/%2Egitlab-ci%2Eyml" \
  -H "PRIVATE-TOKEN: $GL_TOKEN" \
  -H "Content-Type: application/json" \
  --data-binary '{
    "branch": "main",
    "content": "stages:\n  - test\n\nhello-world:\n  stage: test\n  script:\n    - echo Hello from kind cluster runner\n    - date\n    - hostname\n",
    "commit_message": "Add CI pipeline"
  }'
```

Watch the pipeline:

```bash
curl -s "http://<EXTERNAL_IP>/api/v4/projects/<PROJECT_ID>/pipelines?per_page=1&order_by=id&sort=desc" \
  -H "PRIVATE-TOKEN: $GL_TOKEN" \
  | python3 -c "import sys,json; p=json.load(sys.stdin)[0]; print(p['id'], p['status'])"
```

Expected output: `<pipeline id> success`. The job pod appears briefly in `kubectl get pods -n gitlab-runner-gcp`.

---

## Part 6 — Optional: enable HTTPS with Let's Encrypt

Once your DNS A record is live and port 80 is reachable from the internet:

```bash
# On the VM:
sudo vim /etc/gitlab/gitlab.rb
```

Change:

```ruby
external_url "https://<GITLAB_HOST>"
letsencrypt['enable'] = true
letsencrypt['contact_emails'] = ['your@email.com']
```

Then:

```bash
sudo gitlab-ctl reconfigure
```

GitLab Omnibus provisions the Let's Encrypt cert automatically — no certbot needed. **Expect:** `https://<GITLAB_HOST>` opens with a trusted certificate, and `http://` redirects to it.

**After HTTPS is on, update everything that used `http://<EXTERNAL_IP>`.** The IP redirects to HTTPS and does not match the certificate, so the old runner config and `curl` calls break:

1. In `runner-values.yaml` set `gitlabUrl: https://<GITLAB_HOST>` and **delete** the `clone_url = "http://<EXTERNAL_IP>"` line (the hostname must resolve from the kind pods; if it does not, check DNS from a pod first).
2. Run `helm upgrade gcp-runner gitlab/gitlab-runner --namespace gitlab-runner-gcp --values runner-values.yaml --wait`.
3. Use `https://<GITLAB_HOST>` instead of `http://<EXTERNAL_IP>` in the `curl` commands.
4. If you opened port 80 to the world for Let's Encrypt, restrict the firewall rule to your IP again.

> The comments in Part 4 explaining `clone_url` apply only to the HTTP-by-IP setup. Do Part 6 before you create real projects and tokens if you can.

---

## Cleanup

Cloud resources bill until deleted. When you finish:

```bash
helm uninstall gcp-runner --namespace gitlab-runner-gcp
kubectl delete namespace gitlab-runner-gcp

gcloud compute instances delete demo-test --project=<PROJECT> --zone=<ZONE> --delete-disks=all
gcloud compute firewall-rules delete gitlab-server-allow --project=<PROJECT>
# only if you reserved a static IP:
gcloud compute addresses delete gitlab-ip --project=<PROJECT> --region=<REGION>
```

Also: delete the DNS A record for `<GITLAB_HOST>`, and revoke the admin token (**User settings > Access tokens > Revoke**; it expires in 2 days anyway). **Expect:** `gcloud compute instances list --project=<PROJECT>` and `gcloud compute disks list --project=<PROJECT>` show no `demo-test`. Do not delete the `gitlab-runner` namespace: it belongs to the stand runner.

---

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `no match for platform in manifest` | Helper image arch mismatch | Set `helper_image` to `arm64-vX.Y.Z` under `[runners.kubernetes]` |
| `fatal: repository … not found` (exit 128) | DNS not resolving inside kind pods | Add `clone_url = "http://<IP>"` to `[[runners]]` in runner config |
| `runner_configuration_error` with no image errors | `helper_image` set at wrong TOML level | Move it inside `[runners.kubernetes]`, not top-level `[[runners]]` |
| Pipeline fails instantly, 0 jobs, `yaml_errors: null` | Transient background worker startup on fresh install | Retry triggering the pipeline after a minute |
