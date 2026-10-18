# Podman (and Docker) — image optimization lab

**Goal.** Take a small Python app whose image carries C compilers and dev headers, and shrink its image from **~1.2 GB → ~76 MB** through six progressive iterations. Each iteration teaches one specific best practice.

**Why this matters.** Production image size affects:
- **Pull time** in CI/CD and at pod scheduling (cold-start of services)
- **Attack surface** — every tool inside the image is something an attacker can use
- **Storage cost** — registries charge per GB; nodes cache per GB
- **Compliance** — fewer packages → fewer CVEs to triage

**Time.** ~90 minutes to do every iteration end-to-end. Each one is self-contained — you can stop after any iteration.

**Audience.** You've already met the Dockerfile basics (Labs 0–5 of `docker-labs.md`). Now you want to know what "production-ready" actually means in concrete commands.

---

## Tooling — Podman first, Docker equivalents

Every command in this lab is written for **Podman**. The Dockerfile itself is identical for both tools. The tables list the Docker equivalent next to each Podman command, and `> **Docker:**` callouts mark places where the behaviour really differs. Platform notes (macOS machine, Linux packages) are in [podman-platforms.md](podman-platforms.md).

> Lab results below were measured on Ubuntu 24.04, `amd64`, rootful Podman 4.9.3. Your sizes will differ by a few percent. macOS and Apple Silicon were not run.

| Operation | Podman | Docker |
|-----------|--------|--------|
| Build | `podman build -t TAG .` | `docker build -t TAG .` |
| Run, throwaway | `podman run --rm -p 5000:5000 TAG` | `docker run --rm -p 5000:5000 TAG` |
| List images | `podman images` | `docker images` |
| Inspect history | `podman history TAG` | `docker history TAG` |
| Remove image | `podman rmi TAG` | `docker rmi TAG` |
| Multi-arch | `podman build --platform=...` (needs QEMU, see iteration 6) | `docker buildx build --platform=...` |
| CVE scan | `trivy image TAG` (`podman image scan` does not exist; `trivy` is not in the Ubuntu 24.04 repositories, install from aquasecurity.github.io/trivy) | `docker scout cves TAG` |
| Disk usage | `podman system df` | `docker system df` |
| Pull base | `podman pull python:3.12-slim` | `docker pull python:3.12-slim` |

> 💡 On Linux without root, Podman runs rootless by default and uses the same OCI image format Docker produces. The labs here were run rootful; rootless was not run.

## Quick CLI cheatsheet

Use this when moving between lab iterations.

| Task | Podman | Docker |
|------|--------|--------|
| Login | `podman login registry.example.com` | `docker login registry.example.com` |
| Pull | `podman pull python:3.12-slim` | `docker pull python:3.12-slim` |
| Build | `podman build -f Dockerfile.3 -t lab:3-multistage .` | `docker build -f Dockerfile.3 -t lab:3-multistage .` |
| Create only | `podman create --name lab3 lab:3-multistage` | `docker create --name lab3 lab:3-multistage` |
| Run service | `podman run --rm -p 5000:5000 lab:3-multistage` | `docker run --rm -p 5000:5000 lab:3-multistage` |
| List containers | `podman ps -a` | `docker ps -a` |
| Logs | `podman logs -f --tail=100 lab3` | `docker logs -f --tail=100 lab3` |
| Shell/debug | `podman exec -it lab3 sh` | `docker exec -it lab3 sh` |
| Describe/inspect | `podman inspect lab3` | `docker inspect lab3` |
| Tag for registry | `podman tag lab:3-multistage registry.example.com/team/lab:3` | `docker tag lab:3-multistage registry.example.com/team/lab:3` |
| Push | `podman push registry.example.com/team/lab:3` | `docker push registry.example.com/team/lab:3` |
| Delete container | `podman rm lab3` | `docker rm lab3` |
| Delete image | `podman rmi lab:3-multistage` | `docker rmi lab:3-multistage` |
| Reclaim disk | `podman rmi TAG` (by name) | `docker rmi TAG` |

> **Do not use `system prune` on a shared machine.** It removes every unused image and stopped container, including other people's, and on the course VM it can delete the images of the running kind cluster. Remove only what you built, by name.
>
> Docker/Podman do not have a `describe` command. Use `inspect` for detailed JSON metadata and `history` for layer-by-layer image changes.

---

## Setup

### 1. Workspace

```bash
mkdir docker-optimization-lab && cd docker-optimization-lab
```

### 2. `app.py` — a small Flask app that exercises `cryptography`

```python
from flask import Flask
from cryptography.fernet import Fernet
import os

app = Flask(__name__)
KEY = os.environ.get("APP_KEY", Fernet.generate_key().decode())
cipher = Fernet(KEY.encode())

@app.route("/")
def index():
    token = cipher.encrypt(b"hello from docker").decode()
    return f"signed token: {token}\n"

if __name__ == "__main__":
    app.run(host="0.0.0.0", port=5000)
```

### 3. `requirements.txt` — versions pinned for reproducibility

```text
flask==3.0.3
cryptography==42.0.7
gunicorn==22.0.0
```

> `cryptography` is included as a typical "heavy" dependency. On `amd64` and `arm64`, PyPI has prebuilt wheels for it, so pip does **not** compile it here and the build tools in the early iterations are not strictly required. The lab still installs them to reproduce the common real-world case: packages that have no wheel for your platform (for example `psycopg2`, `mysqlclient`, or any package on a rare CPU) and force a C compiler into the image. The size effect you measure is what matters.

### 4. Add 50 MB of "junk" that shouldn't ship with the image

This simulates accidentally committed logs / debug dumps / test fixtures.

- **Linux / macOS:** `dd if=/dev/zero of=temp-dump.log bs=1M count=50`
- **Windows (PowerShell):** `fsutil file createnew temp-dump.log 52428800`

---

# Iteration 0 — The naive baseline (~1.2 GB)

**Goal.** Reproduce how an inexperienced engineer writes their first Dockerfile.

### `Dockerfile.0`

```dockerfile
# Naive: full Python image, copy everything, install deps
FROM python:3.12

WORKDIR /app

# Copy EVERYTHING (including temp-dump.log, .git, .env, …)
COPY . .

# Install deps — works because python:3.12 has gcc baked in
RUN pip install -r requirements.txt

CMD ["python", "app.py"]
```

### Build & measure

```bash
podman build -f Dockerfile.0 -t lab:0-naive .
podman images lab:0-naive --format "{{.Size}}"
```

**Observation:** ~**1.2 GB** (1.22 GB on the test VM).

- Full Debian + GCC + dev headers + every Python stdlib component → ~1 GB before we even run pip
- Our 50 MB junk file shipped with it
- Every system tool an attacker could need is in there

> 🎯 **Why this is wrong.** "It works" is not enough. You're shipping a development environment, not a runtime.

---

# Iteration 1 — Context hygiene + cache order (~1.1 GB, but fast rebuilds)

**Goal.** Cut the build context, stop the junk leaking in, and make dependency installation cacheable.

### `.dockerignore` — block junk from ever entering the context

```text
# Big files
temp-dump.log
*.log
*.tar
*.zip

# VCS / dev
.git
.gitignore
.github/
.vscode/
.idea/

# Local cruft
__pycache__/
*.pyc
.venv/
venv/
.env
.env.*

# Other Dockerfiles in this lab
Dockerfile*
README*
```

### `Dockerfile.1` — pin tag, copy deps before code

```dockerfile
# Pin a specific minor version — never use 'latest'
FROM python:3.12.4

WORKDIR /app

# 1. Dependencies first — change rarely, cache-friendly
COPY requirements.txt .
RUN pip install -r requirements.txt

# 2. Application code last — changes most often
COPY app.py .

CMD ["python", "app.py"]
```

### Build & measure

```bash
podman build -f Dockerfile.1 -t lab:1-hygiene .
```

Now **change `app.py`** (add a print) and rebuild:

```bash
time podman build -f Dockerfile.1 -t lab:1-hygiene .
```

**Observation:**

- Image size ~**1.1 GB** (1.08 GB on the test VM) — same base, same deps; the junk file is gone
- But the rebuild after a code change takes **seconds** (0.6 s on the test VM) instead of 1–2 min, because pip's layer was cached
- Build context is now KB instead of 50+ MB

> Create `.dockerignore` **before** the first build of iteration 1, otherwise you rebuild once more.

> 🎯 **Lesson.** `.dockerignore` and instruction order are *free* wins. Always do them first.

> ⚠ **Tag pinning trade-off.** `python:3.12.4` is reproducible but stops getting security updates. For prod, pin to a digest: `python:3.12-slim@sha256:abcd…` — fully immutable, you decide when to rebase.

---

# Iteration 2 — Slim base + clean up build tools (~155 MB)

**Goal.** Drop the full Debian + dev image. Install only what we need to compile `cryptography`, then remove it in the same layer.

### `Dockerfile.2`

```dockerfile
FROM python:3.12-slim

WORKDIR /app

COPY requirements.txt .

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
         build-essential \
         libffi-dev \
    && pip install --no-cache-dir -r requirements.txt \
    && apt-get purge -y --auto-remove build-essential libffi-dev \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/* /root/.cache/pip

COPY app.py .

CMD ["python", "app.py"]
```

### Build & measure

```bash
podman build -f Dockerfile.2 -t lab:2-slim .
podman images lab:2-slim --format "{{.Size}}"
```

**Observation:** ~**155 MB** (154 MB on the test VM). Down from ~1.1 GB.

- `python:3.12-slim` is ~130 MB instead of ~1 GB
- Build tools added then removed *in the same RUN* → not stored in the final layer
- `pip install --no-cache-dir` → no pip wheel cache left behind
- `rm -rf /root/.cache/pip` is belt-and-braces

> 🎯 **Lesson 1.** Anything you install in a layer is **permanent** — even if you `rm` it in a later layer, the bytes are still there in the lower layer's history. The only way to truly remove something is to never let it land or to remove it within the same `RUN`.

> 🎯 **Lesson 2.** `--no-install-recommends` cuts ~30 % off Debian package installs.

### Inspect layers

```bash
podman history lab:2-slim
```

Notice the single fat `RUN` layer absorbs all the install/remove work.

---

# Iteration 3 — Multi-stage build (~145 MB)

**Goal.** Use a heavy "builder" image to compile deps, then copy only the artifacts into a fresh, pristine runtime.

### `Dockerfile.3`

```dockerfile
# ===== Stage 1 — builder =====
FROM python:3.12-slim AS builder

WORKDIR /build

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
         build-essential \
         libffi-dev \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .

# Install into a self-contained prefix we can later copy wholesale
RUN pip install --no-cache-dir --prefix=/install -r requirements.txt


# ===== Stage 2 — runtime =====
FROM python:3.12-slim AS runtime

WORKDIR /app

# Copy ONLY the installed Python packages from the builder
COPY --from=builder /install /usr/local

COPY app.py .

CMD ["python", "app.py"]
```

### Build & measure

```bash
podman build -f Dockerfile.3 -t lab:3-multistage .
podman run --rm -p 5000:5000 lab:3-multistage
```

In another terminal: `curl http://localhost:5000/` — you should see a signed token. The app works despite the runtime stage having no `gcc`.

```bash
podman images lab:3-multistage --format "{{.Size}}"
```

**Observation:** ~**145 MB** (144 MB on the test VM). Only a few percent below iteration 2: the slim base already removed most of the weight. Multi-stage pays off more when the build stage needs a lot of tools that iteration 2 could not remove cleanly.

- The `builder` stage carried `gcc`, `libffi-dev`, dev headers, apt caches → **all discarded**
- Only `/install` (the compiled packages + Python bytecode) crossed the stage boundary
- `gcc` is no longer in the runtime image → attacker can't compile a payload inside

> 🎯 **Lesson.** Multi-stage is the most reliable way to keep build tools out of the final image, and the gap grows with the number of build-only tools. On this small app a good slim base already did most of the work, so the saving here is small.

### Confirm gcc is gone

```bash
# (exit code 1 — not found)

podman run --rm lab:3-multistage which gcc
```

---

# Iteration 4 — Security hardening (~145 MB but production-grade)

**Goal.** Same size — but now: non-root user, OCI labels, healthcheck, exec-form CMD, no extraneous metadata.

### `Dockerfile.4`

```dockerfile
# ===== Stage 1 — builder =====
FROM python:3.12-slim AS builder

WORKDIR /build

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
         build-essential \
         libffi-dev \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .

RUN pip install --no-cache-dir --prefix=/install -r requirements.txt


# ===== Stage 2 — runtime =====
FROM python:3.12-slim AS runtime

# OCI annotations — useful for registries, CVE scanners, supply-chain tooling
LABEL org.opencontainers.image.title="docker-optimization-lab"
LABEL org.opencontainers.image.description="Sample Flask + cryptography app, optimized image."
LABEL org.opencontainers.image.source="https://example.invalid/repo"
LABEL org.opencontainers.image.licenses="MIT"

# Create an unprivileged user (uid:gid 10001:10001 by convention)
RUN groupadd --system --gid 10001 app \
    && useradd  --system --uid 10001 --gid app --no-create-home --shell /sbin/nologin app

WORKDIR /app

COPY --from=builder /install /usr/local

COPY --chown=app:app app.py .

# Drop privileges
USER 10001:10001

# Pythonic runtime hygiene
ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

EXPOSE 5000

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD python -c "import urllib.request,sys; sys.exit(0 if urllib.request.urlopen('http://127.0.0.1:5000/').status==200 else 1)"

# Exec form → PID 1 is python, SIGTERM is delivered to it, graceful shutdown works
CMD ["python", "app.py"]
```

### Build & measure

```bash
podman build --format docker -f Dockerfile.4 -t lab:4-hardened .
podman image inspect lab:4-hardened --format '{{.Config.User}}'
podman image inspect lab:4-hardened --format '{{json .HealthCheck}}'
```

The first `--format` line prints `10001:10001`. The second prints a JSON object with `Test`, `Interval` and the other health check settings.

> **Podman:** Podman builds OCI images by default, and the OCI format has no `HEALTHCHECK`, so a plain `podman build` silently drops it (`null` in the output above). `--format docker` keeps it.
>
> **Docker:** `docker build -f Dockerfile.4 -t lab:4-hardened .` keeps `HEALTHCHECK` by default, and `docker inspect lab:4-hardened --format '{{.Config.User}} | {{.Config.Healthcheck.Test}}'` works. The `.Config.Healthcheck` field does not exist in Podman's `inspect` template.

**Verify the process runs as non-root inside the container:**

```bash
# uid=10001(app) gid=10001(app) groups=10001(app)

podman run --rm lab:4-hardened id
```

**Observation:** Size still **~145 MB**. The hardening is essentially free.

> 🎯 **Lesson.** Security-by-default doesn't cost size. Non-root + healthcheck + OCI labels + exec-form CMD are all production table-stakes.

> 💡 **Why non-root matters.** A container breakout that lands as root has many more kernel surfaces to attack. Running as uid 10001 limits the blast radius even before namespace user-remap is configured.

---

# Iteration 5 — Distroless runtime (~76 MB)

**Goal.** Strip the runtime image to the absolute minimum — no shell, no package manager, no `apt`. Just Python and your code.

### `Dockerfile.5`

> ⚠ **Two version-coupling traps to avoid.**
> 1. `gcr.io/distroless/python3-debian12` ships **Python 3.11** (it's built on Debian 12 / Bookworm). The builder **must** use the same `3.11` minor — compiled wheels for `cryptography` are ABI-tied to a specific CPython.
> 2. Distroless's `sys.path` doesn't always include `/usr/local/lib/python3.11/site-packages/`. Instead of guessing what's on `sys.path`, install with `pip --target=/pkg` (flat layout) and add `/pkg` to `PYTHONPATH` — explicit beats implicit.

```dockerfile
# ===== Stage 1 — builder =====
# IMPORTANT: must match runtime Python version (distroless/python3-debian12 → 3.11)
FROM python:3.11-slim AS builder

WORKDIR /build

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
         build-essential \
         libffi-dev \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .

# --target installs flat into /pkg — no Python-version-specific subdirectory.
# Works regardless of what distroless's site-packages search path is.
RUN pip install --no-cache-dir --target=/pkg -r requirements.txt


# ===== Stage 2 — distroless runtime =====
# Distroless: only the language runtime + its deps. No shell, no apt, no curl.
FROM gcr.io/distroless/python3-debian12:nonroot AS runtime

WORKDIR /app

# The 'nonroot' variant already runs as uid 65532; this line is explicit insurance.
USER nonroot:nonroot

# Copy installed packages and make Python find them via PYTHONPATH
COPY --from=builder /pkg /pkg
ENV PYTHONPATH=/pkg

COPY --chown=nonroot:nonroot app.py .

EXPOSE 5000

# distroless/python3 sets ENTRYPOINT to /usr/bin/python3 — CMD is just the args.
CMD ["app.py"]
```

### Build & measure

```bash
podman build -f Dockerfile.5 -t lab:5-distroless .
podman images lab:5-distroless --format "{{.Size}}"
```

**Observation:** ~**76 MB** (75.9 MB on the test VM). About **16× smaller** than the naive baseline.

**Verify it works:**

```bash
podman run --rm -p 5000:5000 lab:5-distroless
# in another shell:
curl http://localhost:5000/
```

**Try to debug inside it:**

```bash
podman run --rm -it --entrypoint sh lab:5-distroless
# error: exec sh: no such file or directory
```

`--entrypoint sh` нужен, потому что ENTRYPOINT образа — `/usr/bin/python3`, и `sh` без переопределения его не вызовет. Для Podman: `podman run --rm -it --entrypoint sh lab:5-distroless`.

Right — there's no shell. That's the point.

> 🎯 **Lesson 1.** Distroless gets you the smallest possible Python runtime that still works. Attack surface is near-zero.

> 🎯 **Lesson 2.** Debugging is harder — you can't `kubectl exec -- sh`. Use *ephemeral debug containers* (`kubectl debug` / `docker debug`) or attach `busybox` as a sidecar.

> ⚠ **Alternative:** `python:3.12-alpine` is a small image too (~60 MB) but uses musl libc — some Python wheels aren't built for musl and pip falls back to source compilation. Trade-off: smaller but sometimes slower builds and rarer ABI bugs.

---

# Iteration 6 (bonus) — Multi-arch with `podman build --platform`

**Goal.** Build the same image for both `amd64` and `arm64` from a single source.

> This iteration was not run on the test VM (no QEMU emulation and no registry). Treat the commands as a starting point.

### Prerequisites

- **QEMU user-mode emulation.** Building an image for a CPU that is not yours executes its `RUN` steps under emulation. On Linux install it first (`sudo apt install qemu-user-static`); `podman build --platform` alone is not enough. On macOS check that your `podman machine` has the emulation registered (not verified).
- **A registry you can push to.** A multi-arch manifest list cannot be kept as a normal local image, so the final step pushes it. Replace `registry.example.com/myapp:1.0.0` with your own.

### Build and push

```bash
podman build \
  --platform linux/amd64,linux/arm64 \
  -f Dockerfile.5 \
  --manifest registry.example.com/myapp:1.0.0 \
  .

podman manifest push registry.example.com/myapp:1.0.0
```

> **Docker:** use `buildx`.
>
> ```bash
> # First time only: create a multi-arch builder backed by BuildKit + QEMU
> docker buildx create --name multi --use --bootstrap
>
> docker buildx build \
>   --platform linux/amd64,linux/arm64 \
>   -f Dockerfile.5 \
>   -t registry.example.com/myapp:1.0.0 \
>   --push .
> ```

**Observation:** the resulting manifest list points at one image per architecture. The pulling client (Podman, Docker, Kubernetes kubelet, etc.) automatically picks the right one for its node.

> 💡 **Why multi-arch?** Apple Silicon laptops + AWS Graviton nodes (both `arm64`) make `amd64`-only images a real pain. Building both at once costs minutes; debugging a wrong-arch image at 3am costs hours.

---

# Final leaderboard

Run this to see every iteration's size side-by-side:

```bash
podman images --format "table {{.Repository}}:{{.Tag}}\t{{.Size}}" | grep lab:
```

**Expected (approximate, varies by arch and base image version):**

| Iteration | Tag | Size | Δ from previous | Key technique |
|-----------|-----|------|-----------------|---------------|
| 0 | `lab:0-naive` | ~1.2 GB | — | baseline |
| 1 | `lab:1-hygiene` | ~1.1 GB | -11 % | `.dockerignore` + cache order + pinned tag |
| 2 | `lab:2-slim` | ~155 MB | **-86 %** | slim base + install/remove build tools in one `RUN` |
| 3 | `lab:3-multistage` | ~145 MB | **-6 %** | multi-stage: builder + runtime |
| 4 | `lab:4-hardened` | ~145 MB | 0 % | non-root + OCI labels + healthcheck (security, not size) |
| 5 | `lab:5-distroless` | ~76 MB | **-47 %** | distroless runtime — no shell, minimal CVE surface |

**Net result:** ~**16× smaller** image with **strictly less** attack surface.

---

# Inspection toolbox — use these on every image

| What | Podman | Docker |
|------|--------|--------|
| Quick size | `podman images TAG` | `docker images TAG` |
| Layer-by-layer size | `podman history TAG` | `docker history TAG` |
| Full image metadata | `podman image inspect TAG` | `docker inspect TAG` |
| What user runs as | `podman image inspect TAG --format '{{.Config.User}}'` | `docker inspect TAG --format '{{.Config.User}}'` |
| CVE scan | `trivy image TAG` | `docker scout cves TAG` |
| Files inside | `podman run --rm TAG ls /usr/local/lib` (not on distroless: no `ls`, and Python would try to open a file named `ls`) | `docker run --rm TAG ls /usr/local/lib` |
| Disk usage | `podman system df` (read-only) | `docker system df` |
| Dive (deep layer browser) | `dive TAG` (not installed on the test VM; see the link below) | `dive TAG` |

> 💡 Install `dive` (https://github.com/wagoodman/dive) for an interactive TUI that shows every file in every layer with size sorting. Invaluable for "what's making this huge?" investigations.

---

# Cleanup

```bash
podman rmi lab:0-naive lab:1-hygiene lab:2-slim lab:3-multistage lab:4-hardened lab:5-distroless
```

Remove only the images you built, by name. Do **not** run `podman system prune` or `docker system prune` on a shared machine: they delete every unused image and stopped container there, not just yours.

---

# What to take away

1. **`.dockerignore` and cache order are free** — do them on day one, never lose them.
2. **Anything installed in a layer stays in the image** — install + clean must happen in the *same* `RUN`.
3. **Multi-stage is the highest-leverage technique** — builder gets dirty, runtime stays clean.
4. **Non-root + healthcheck + exec-form CMD don't cost size** — they're table-stakes for production.
5. **Distroless / minimal runtimes** trade debuggability for attack surface — pick by your operability story.
6. **Multi-arch from day one** — Apple Silicon laptops + Graviton nodes make `arm64` mandatory.

**Reading list:**

- Docker docs — Building best practices: https://docs.docker.com/build/building/best-practices/
- Distroless project: https://github.com/GoogleContainerTools/distroless
- `dive`: https://github.com/wagoodman/dive
- Podman build reference: https://docs.podman.io/en/latest/markdown/podman-build.1.html
- Trivy (CVE scanner): https://aquasecurity.github.io/trivy/
