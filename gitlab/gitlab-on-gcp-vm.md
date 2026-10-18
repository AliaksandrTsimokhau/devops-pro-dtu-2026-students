# GitLab CE on GCP VM + kind Cluster Runner

## What you will build

A self-hosted GitLab CE instance running on a Google Cloud VM, with a GitLab Runner installed inside a local **kind** Kubernetes cluster. By the end, you can push a `.gitlab-ci.yml` and watch CI jobs run as pods inside your kind cluster.

---

## Prerequisites

| Tool | Purpose |
|---|---|
| `gcloud` CLI authenticated | Create the VM and SSH into it |
| `kubectl` pointed at a kind cluster | Install the runner |
| `helm` v3 | Install the GitLab Runner Helm chart |
| A dynamic-DNS hostname | Expose GitLab under a stable URL |

**Kind cluster note:** if your cluster runs on Apple Silicon (arm64), you must override the runner helper image (covered below).

---

## Part 1 — Create the GCP VM

### 1.1 Find the network details

```bash
gcloud compute networks list --project=<YOUR_PROJECT>
gcloud compute networks subnets list \
  --project=<YOUR_PROJECT> \
  --network=<NETWORK_NAME>
```

### 1.2 Create the VM

```bash
gcloud compute instances create demo-test \
  --project=ufl-backend-playground \
  --zone=europe-west4-a \
  --machine-type=e2-standard-4 \
  --description="delete me if you want" \
  --image-family=ubuntu-2204-lts \
  --image-project=ubuntu-os-cloud \
  --boot-disk-size=50GB \
  --boot-disk-type=pd-ssd \
  --network=playground-network \
  --subnet=playground-subnetwork \
  --tags=gitlab-server,http-server,https-server \
  --metadata=startup-script='#!/bin/bash
set -euxo pipefail
exec > /var/log/gitlab-install.log 2>&1

apt-get update -y
apt-get install -y curl openssh-server ca-certificates tzdata perl

curl -fsSL https://packages.gitlab.com/install/repositories/gitlab/gitlab-ce/script.deb.sh | bash

EXTERNAL_URL="http://gitlab-devops-dtu.ip-dynamic.org" apt-get install -y gitlab-ce

echo "GitLab CE install complete"
'
```

**Machine sizing:** GitLab needs a minimum of 4 vCPU + 4 GB RAM.  `e2-standard-4` (4 vCPU, 16 GB) gives comfortable headroom for runners and background jobs.

Note the external IP printed at the end — you will need it to set up DNS and for the runner config.

### 1.3 Open ports

```bash
gcloud compute firewall-rules create gitlab-server-allow \
  --project=ufl-backend-playground \
  --network=playground-network \
  --direction=INGRESS \
  --action=ALLOW \
  --rules=tcp:22,tcp:80,tcp:443 \
  --target-tags=gitlab-server \
  --source-ranges=0.0.0.0/0 \
  --description="Allow SSH, HTTP, HTTPS to GitLab"
```

### 1.4 Point DNS

Log in to your dynamic-DNS provider and create an **A record**:

```
gitlab-devops-dtu.ip-dynamic.org  →  <EXTERNAL_IP>
```

GitLab bakes the external URL into every generated link at first reconfigure, so DNS must match before you open the web UI for the first time.

### 1.5 Wait for install to finish

```bash
gcloud compute ssh demo-test \
  --project=ufl-backend-playground \
  --zone=europe-west4-a

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

Open `http://gitlab-devops-dtu.ip-dynamic.org`, log in as `root`.

### 2.2 Disable public sign-ups

Admin Area → Settings → General → **Sign-up restrictions** → uncheck **Sign-up enabled** → Save.

Or via API (useful for scripted setup):

```bash
curl -X PUT "http://<EXTERNAL_IP>/api/v4/application/settings" \
  -H "PRIVATE-TOKEN: <ROOT_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{"signup_enabled": false}'
```

### 2.3 Create an admin API token (scripted setup)

If you want to configure GitLab without using the web UI, create a Personal Access Token on the VM:

```bash
sudo gitlab-rails runner "
  user = User.find_by_username('root')
  token = user.personal_access_tokens.create!(
    scopes: ['api', 'read_user', 'sudo'],
    name: 'admin-setup-token',
    expires_at: 365.days.from_now
  )
  puts token.token
"
```

Use the printed token as `PRIVATE-TOKEN` in all subsequent API calls.

### 2.4 Create a group and project

Via API:

```bash
# Group
curl -X POST "http://<EXTERNAL_IP>/api/v4/groups" \
  -H "PRIVATE-TOKEN: <TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{"name": "devops-dtu", "path": "devops-dtu", "visibility": "private"}'

# Project inside the group (use namespace_id from the group response)
curl -X POST "http://<EXTERNAL_IP>/api/v4/projects" \
  -H "PRIVATE-TOKEN: <TOKEN>" \
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
  -H "PRIVATE-TOKEN: <TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "runner_type": "instance_type",
    "description": "kind-cluster-runner",
    "tag_list": ["kind", "kubernetes"],
    "run_untagged": true
  }'
```

Save the `token` field from the response — it starts with `glrt-`.

---

## Part 4 — Install GitLab Runner in kind

### 4.1 Add the Helm repository

```bash
helm repo add gitlab https://charts.gitlab.io
helm repo update gitlab
```

### 4.2 Create namespace

```bash
kubectl create namespace gitlab-runner
```

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
        image = "ubuntu:22.04"
        pull_policy = ["if-not-present"]
        privileged = false
        # Required on Apple Silicon (arm64): the runner auto-detects its own arch
        # for the helper image tag, but must set it explicitly for Kubernetes jobs
        # because the helper sidecar is pulled by the kind node (also arm64).
        helper_image = "registry.gitlab.com/gitlab-org/gitlab-runner/gitlab-runner-helper:arm64-v19.1.0"
```

> **`clone_url`** — overrides the URL the runner uses for `git clone`. Without this, the runner uses GitLab's `external_url` (the hostname), which doesn't resolve inside kind pods when DNS isn't configured for that hostname.
>
> **`helper_image`** — the helper is a sidecar container that handles repo cloning and artifact upload. It is a platform-specific binary. On `arm64` hosts (Apple Silicon), the runner must pin this to the `arm64` tag; the default `x86_64` tag will fail with `no match for platform in manifest`.

### 4.4 Install

```bash
helm upgrade --install gitlab-runner gitlab/gitlab-runner \
  --namespace gitlab-runner \
  --values runner-values.yaml \
  --wait
```

### 4.5 Verify

```bash
# Pod running
kubectl get pods -n gitlab-runner

# Runner online in GitLab
curl -s "http://<EXTERNAL_IP>/api/v4/runners/all" \
  -H "PRIVATE-TOKEN: <TOKEN>" \
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
curl -X POST "http://<EXTERNAL_IP>/api/v4/projects/<PROJECT_ID>/repository/files/%2Egitlab-ci.yml" \
  -H "PRIVATE-TOKEN: <TOKEN>" \
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
  -H "PRIVATE-TOKEN: <TOKEN>" \
  | python3 -c "import sys,json; p=json.load(sys.stdin)[0]; print(p['id'], p['status'])"
```

Expected: `status: success`.

---

## Part 6 — Optional: enable HTTPS with Let's Encrypt

Once your DNS A record is live and port 80 is reachable from the internet:

```bash
# On the VM:
sudo vim /etc/gitlab/gitlab.rb
```

Change:

```ruby
external_url "https://gitlab-devops-dtu.ip-dynamic.org"
letsencrypt['enable'] = true
letsencrypt['contact_emails'] = ['your@email.com']
```

Then:

```bash
sudo gitlab-ctl reconfigure
```

GitLab Omnibus provisions the Let's Encrypt cert automatically — no certbot needed.

---

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `no match for platform in manifest` | Helper image arch mismatch | Set `helper_image` to `arm64-vX.Y.Z` under `[runners.kubernetes]` |
| `fatal: repository … not found` (exit 128) | DNS not resolving inside kind pods | Add `clone_url = "http://<IP>"` to `[[runners]]` in runner config |
| `runner_configuration_error` with no image errors | `helper_image` set at wrong TOML level | Move it inside `[runners.kubernetes]`, not top-level `[[runners]]` |
| Pipeline fails instantly, 0 jobs, `yaml_errors: null` | Transient background worker startup on fresh install | Retry triggering the pipeline after a minute |
| `glab mr list` returns 403 on one repo | Repo archived in GitLab | Skip it; archived repos reject all write operations |
