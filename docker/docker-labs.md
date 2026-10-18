# Containers with Podman — Self-Paced Labs

Companion handout for the slide deck `../docker-basics-slides.md`. Based on practical examples from *Docker Deep Dive* (Nigel Poulton, May 2025). The file keeps its old name so existing links keep working.

## How to use this document

- Follow the labs in order **once** to build the mental model — then revisit as a reference.
- Each lab is **self-contained** with its own cleanup step.
- Commands prefixed with `$` are run on your **host** shell; `#` is inside a container.
- Replace `<placeholder>` values with your own.
- The estimated times are targets — don't rush, and don't worry if it takes longer the first time.
- **Podman is the main tool.** Every command is written as `podman`. Where macOS and Linux differ, or Podman and Docker differ, you will see a short callout: `> **macOS:**`, `> **Linux:**` or `> **Docker:**`. All differences are collected in [podman-platforms.md](podman-platforms.md).
- Labs 0–8 were run on Ubuntu 24.04 with rootful Podman 4.9.3. macOS and rootless mode were not run; callouts for them are marked "not verified".

## Lab catalogue

| # | Title | Time | Level | Required setup |
|---|-------|------|-------|----------------|
| 0 | Install Podman | 30 min | Beginner | macOS / Linux |
| 1 | First container | 20 min | Beginner | Podman installed |
| 2 | Container lifecycle | 25 min | Beginner | Podman installed |
| 3 | Working with images | 30 min | Beginner | Podman installed |
| 4 | Containerize a Node.js app | 40 min | Intermediate | `git`, hand-written Dockerfile |
| 5 | Multi-stage builds | 35 min | Intermediate | Lab 4 done |
| 6 | Compose multi-container | 40 min | Intermediate | Compose v2 provider (`docker-compose-v2`) |
| 7 | Bridge networks | 30 min | Intermediate | Linux or macOS machine |
| 8 | Volumes & persistent data | 25 min | Beginner | Podman installed |
| 9 | Build a Swarm + stack (Docker only) | 45 min | Advanced | Play With Docker / Multipass |
| 10 | Overlay networking (Docker only) | 35 min | Advanced | Swarm from Lab 9 |
| 11 | Vulnerability scanning (Docker Scout, or `trivy`) | 25 min | Intermediate | Docker Desktop, or `trivy` |
| 12 | Content Trust + Secrets (Docker only) | 35 min | Advanced | Swarm + Docker Hub |
| 13 | Docker Model Runner (opt, Docker only) | 50 min | Advanced | Docker Desktop 4.41+ |
| 14 | Wasm containers (opt, Docker only) | 35 min | Advanced | Docker Desktop 4.37+ |

> **Labs 9–14 — optional / reference, Docker only.** Podman has no Swarm, Docker Scout, Model Runner or Docker's Wasm integration. These labs keep their `docker` commands and need Docker Desktop or several VMs with Docker. The main path of the course is labs 0–8. For vulnerability scanning without Docker Desktop use `trivy image <image>` (not verified here: `trivy` is not in the Ubuntu 24.04 repositories).

---

# Lab 0 — Install Podman

**Goal.** Get a working Podman environment on your machine and verify that the client and the engine respond.

**Prereqs.** A computer with admin rights (macOS or Linux). On Windows, use a Linux VM or WSL 2 and follow the Linux steps.

## Steps

### 1. Install Podman

- **macOS:** Podman runs containers inside a small Linux VM called a *machine*.

  ```bash
  $ brew install podman
  $ podman machine init --rootful --cpus 4 --memory 8192
  $ podman machine start
  $ podman machine list
  ```

  `--rootful` is needed later for kind (module 2). Lower `--cpus` and `--memory` if your laptop is small.

- **Linux (Ubuntu 24.04):** Podman runs natively, no VM.

  ```bash
  $ sudo apt install podman netavark aardvark-dns docker-compose-v2
  $ echo 'unqualified-search-registries = ["docker.io"]' | sudo tee /etc/containers/registries.conf.d/00-docker-hub.conf
  $ sudo systemctl enable --now podman.socket
  ```

  - `netavark` and `aardvark-dns` give containers working networking and name resolution (Lab 7).
  - The `registries.conf.d` line lets you pull by short name: `podman pull nginx` instead of `podman pull docker.io/library/nginx`. Without it you get `short-name ... did not resolve to an alias`.
  - `docker-compose-v2` is the compose provider used in Lab 6.

> **macOS:** if you see the same short-name error, add the registries line inside the machine (`podman machine ssh`). Not verified.

Optional but handy: make the old muscle memory work.

```bash
$ alias docker=podman                                       # interactive shell only
$ sudo ln -s "$(command -v podman)" /usr/local/bin/docker   # Linux, for scripts that call docker
```

> **Docker:** if you use Docker Desktop (macOS) or Docker Engine (Linux) instead, run the same labs with `docker` in place of `podman`. Differences are listed in [podman-platforms.md](podman-platforms.md).

### 2. Verify the installation

```bash
$ podman version
$ podman info
```

**Success criteria:** `podman version` prints a Client block with the version, and `podman info` runs without errors. On macOS `podman version` also shows a Server block that comes from the machine VM; on Linux there is no Server block because there is no daemon.

> **Docker:** `docker version` always shows two blocks, **Client** and **Server**. The missing Server block is normal for Podman.

### 3. Understand the architecture

Podman has **no long-running daemon**. Each container is a normal child process of your user (or root), supervised by a small helper called `conmon`.

- **Linux:** the Podman CLI starts containers directly on your kernel.
- **macOS:** the CLI is a native macOS binary, but containers need a Linux kernel, so they run inside the *machine* VM. That is why `podman info` reports `os: linux` even on a Mac. All macOS containers are Linux containers in practice.

## Troubleshooting

| Error | Cause / Fix |
|-------|-------------|
| `Cannot connect to Podman` (macOS) | Machine not running: `podman machine start` |
| `short-name ... did not resolve to an alias` (Linux) | Add the `registries.conf.d` line from step 1, or use full names such as `docker.io/library/nginx` |
| `looking up compose provider failed` | No compose provider installed. Linux: `sudo apt install docker-compose-v2` |
| `cannot listen on the TCP port ... address already in use` | Another process or container already publishes that host port. Pick another port or stop the other container |
| Port below 1024 fails when running rootless | Unprivileged users cannot bind low ports. Use ports above 1024 (the labs do), or run rootful |

## Cleanup

Nothing to clean. Podman stays installed for the rest of the course.

## Verification checklist

- [ ] `podman version` prints a Client block
- [ ] `podman info` runs without error
- [ ] You can explain why there is no Server block on Linux, and why `os: linux` appears on a Mac

---

# Lab 1 — Run your first container

**Goal.** Pull an NGINX image from Docker Hub, run it, expose it on a host port, exec a command inside, then delete cleanly.

**Prereqs.** Lab 0 complete.

## Steps

### 1. Pull an image

```bash
$ podman pull nginx:latest
Using default tag: latest
latest: Pulling from library/nginx
ad5932596f78: Pull complete
e4bc5c1a6721: Pull complete
...
Digest: sha256:fb197595ebe76b9c0c14ab68159fd3c08bd067ec62300583543f0ebda353b5be
Status: Downloaded newer image for nginx:latest
docker.io/library/nginx:latest

$ podman images
REPOSITORY      TAG      IMAGE ID       CREATED          SIZE
nginx           latest   fb197595ebe7   10 days ago      280MB
```

The image now exists in your local image cache.

> **Linux:** if the pull fails with `short-name ... did not resolve to an alias`, add the registries line from Lab 0, or pull the full name: `podman pull docker.io/library/nginx:latest`.
>
> **Docker:** Podman prints full image names (`docker.io/library/nginx`) in `podman images`. Docker hides the `docker.io/library/` prefix. The images are identical.

### 2. Run a container from the image

```bash
$ podman run --name test -d -p 5005:80 nginx:latest
e08c3535...30557225
```

Flag breakdown:

| Flag | Meaning |
|------|---------|
| `--name test` | Friendly container name |
| `-d` | Detached (background) |
| `-p 5005:80` | Map host port **5005** to container port **80** |
| `nginx:latest` | Image to base the container on |

The long hex string is the container ID.

### 3. Verify the container is running

```bash
$ podman ps
CONTAINER ID  IMAGE        COMMAND       PORTS                  NAMES
e08c35352ff3  nginx:latest "/docker..."  0.0.0.0:5005->80/tcp   test
```

Open `http://localhost:5005` in your browser. You should see the NGINX welcome page.

### 4. Exec into the container

```bash
$ podman exec -it test bash
root@e08c35352ff3:/#
```

Try a few commands:

```bash
root@e08c35352ff3:/# ls -l
total 64
lrwxrwxrwx 1 root root    7 Jan 2 00:00 bin -> usr/bin
drwxr-xr-x 2 root root 4096 Oct 31 11:04 boot
...

# Slim images strip most tools
root@e08c35352ff3:/# ps
bash: ps: command not found

root@e08c35352ff3:/# exit
```

### 5. Stop and remove the container

```bash
$ podman stop test
test

$ podman ps              # gone from the running list
$ podman ps -a           # still here in 'Exited' state
CONTAINER ID  IMAGE         STATUS                       NAMES
e08c35352ff3  nginx:latest  Exited (0) 5 seconds ago     test

$ podman rm test
test

$ podman ps -a           # truly gone
```

> Exit code: nginx shuts down cleanly on `SIGTERM`, so you see `Exited (0)`. You see `Exited (137)` (killed by `SIGKILL` after the 10 s grace period) for a process that ignores `SIGTERM`, for example `sleep` as PID 1.

## Optional challenge

Re-run the container with the `--rm` flag so Podman deletes it automatically when it stops:

```bash
$ podman run --rm -d --name test2 -p 5005:80 nginx:latest
$ podman stop test2
$ podman ps -a   # test2 is already gone
```

## Cleanup

```bash
$ podman stop test2 2>/dev/null
$ podman rm test2   2>/dev/null
$ podman rmi nginx:latest
```

## Verification checklist

- [ ] You pulled an image and saw it in `podman images`
- [ ] You ran a container and reached it via the browser
- [ ] You exec'd into the container and exited cleanly
- [ ] You understand the difference between `podman ps` and `podman ps -a`

---

# Lab 2 — Container lifecycle deep dive

**Goal.** Observe how PID 1, the writable layer, and restart policies govern a container's life.

**Prereqs.** Labs 0–1.

## Steps

### 1. PID 1 owns the container

Start an interactive Ubuntu container running `bash` as PID 1:

```bash
$ podman run --name ddd-ctr -it ubuntu:24.04 bash
root@d3c892ad0eb3:/#

root@d3c892ad0eb3:/# ps
  PID TTY          TIME CMD
    1 pts/0    00:00:00 bash
    9 pts/0    00:00:00 ps
```

`bash` is PID 1. Now type `exit`:

```bash
root@d3c892ad0eb3:/# exit
exit

$ podman ps -a | grep ddd-ctr
ddd-ctr   ubuntu:24.04   "bash"   Exited (0) 3 secs ago
```

**Rule:** kill PID 1, kill the container. Containers run only while their main process runs.

### 2. Detach without killing — `Ctrl-P Q`

```bash
$ podman restart ddd-ctr
ddd-ctr

$ podman attach ddd-ctr
root@d3c892ad0eb3:/#
```

You're back inside. To leave without killing the bash process, press **Ctrl-P** then **Ctrl-Q** (NOT `exit`):

```bash
root@d3c892ad0eb3:/#          # press Ctrl-P then Ctrl-Q
read escape sequence

$ podman ps
CONTAINER ID    IMAGE          STATUS          NAMES
d3c892ad0eb3    ubuntu:24.04   Up 27 seconds   ddd-ctr
```

Memorize this — you'll need it often.

### 3. The writable layer survives stop/restart but not delete

```bash
$ podman exec -it ddd-ctr bash
root@d3c892ad0eb3:/# echo "hello world" > /tmp/persisted.txt
root@d3c892ad0eb3:/# exit

$ podman stop ddd-ctr
$ podman start ddd-ctr

$ podman exec ddd-ctr cat /tmp/persisted.txt
hello world
```

But when you delete the container, the writable layer is gone forever:

```bash
$ podman rm -f ddd-ctr
$ podman run --name ddd-ctr -it ubuntu:24.04 bash
root@xxx:/# cat /tmp/persisted.txt
cat: /tmp/persisted.txt: No such file or directory
```

**Anti-pattern warning.** Never edit files in a live production container — the changes vanish on `rm`. Build a new image and replace the container.

### 4. Restart policies

```bash
$ podman run --name neversaydie -it --restart always alpine sh
/# exit                # zero exit, but 'always' restarts it

$ podman ps
CONTAINER ID    IMAGE     CREATED          STATUS         NAMES
1933623830bb    alpine    35 seconds ago   Up 2 seconds   neversaydie

$ podman inspect neversaydie | grep RestartCount
        "RestartCount": 1,
```

| Policy | Non-zero exit | Zero exit | `podman stop` | Daemon restart |
|--------|--------------|----------|---------------|----------------|
| `no` (default) | N | N | N | N |
| `on-failure` | Y | N | N | Y |
| `always` | Y | Y | N | Y |
| `unless-stopped` | Y | Y | N | N |

`always` and `unless-stopped` differ only in behaviour after a `podman stop` followed by a daemon restart: `always` restarts the container, `unless-stopped` does not.

> **Podman:** there is no daemon, so "Daemon restart" means "after the machine reboots". Containers with a restart policy come back after a reboot only if `podman-restart.service` is enabled (`sudo systemctl enable podman-restart.service`). Not verified here. **Docker:** the Docker daemon restarts them itself.

## Cleanup

```bash
$ podman rm -f ddd-ctr neversaydie
$ podman rmi ubuntu:24.04 alpine
```

## Verification checklist

- [ ] You proved a container dies when PID 1 dies
- [ ] You used `Ctrl-P Q` to detach without killing
- [ ] You observed writes survive `stop`/`restart` but not `rm`
- [ ] You can name the four restart policies and their semantics

---

# Lab 3 — Working with images

**Goal.** Pull by tag and by digest, inspect layers, see how multi-architecture images work.

**Prereqs.** Labs 0–2.

## Steps

### 1. Pull images and observe layer reuse

```bash
$ podman pull alpine
$ podman pull alpine          # second time: nothing is downloaded again
$ podman pull redis:alpine
$ podman images
REPOSITORY                    TAG     IMAGE ID       CREATED       SIZE
docker.io/library/redis       alpine  ...            2 weeks ago   ...
docker.io/library/alpine      latest  ...            2 weeks ago   ...
```

Layers are stored by content hash. When a layer is already in local storage, Podman skips it during a pull. A repeated pull of the same image downloads nothing. Two **different** images share a layer only if they were built on the exact same base release: compare them with `podman history alpine` and `podman history redis:alpine`.

> The pull progress differs between tools. Podman prints `Copying blob ... skipped: already exists` for reused layers; Docker prints `Already exists`.

### 2. Pull immutably by digest

Tags are mutable. `nginx:latest` today is **not** the same image as `nginx:latest` a year ago. For reproducibility, pull by digest.

```bash
$ podman pull alpine
$ podman image inspect alpine --format '{{.Digest}}'
sha256:c5b1261d...8e1ad6b

$ podman manifest inspect alpine | grep -E 'architecture|digest'
# one entry per platform: amd64, arm64, arm/v6, ...

# Pull by digest
$ podman pull alpine@sha256:c5b1261d...8e1ad6b
```

> **Docker:** `docker buildx imagetools inspect alpine` prints the digest and all platform manifests in one view. Podman has no `buildx`.

A digest is a cryptographic hash of the image's contents. Two different images cannot share a digest, and a single digest always pulls the same bytes.

### 3. Multi-architecture transparency

```bash
$ podman manifest inspect golang | grep 'architecture\|os'
"architecture": "amd64", "os": "linux"
"architecture": "arm", "os": "linux"
"architecture": "arm64", "os": "linux"
"architecture": "ppc64le", "os": "linux"
"architecture": "s390x", "os": "linux"
"architecture": "amd64", "os": "windows"

$ podman run --rm golang go version
go version go1.23.4 linux/arm64    # on Apple Silicon
```

Same command, different hosts, the right image is selected from the manifest list automatically.

### 4. Inspect layers and metadata

```bash
$ podman history redis:latest
IMAGE          CREATED BY                                  SIZE
11c3e418c296   CMD ["redis-server"]                        0B       # metadata
<missing>      EXPOSE map[6379/tcp:{}]                     0B       # metadata
<missing>      ENTRYPOINT ["docker-entrypoint.sh"]         0B       # metadata
<missing>      COPY docker-entrypoint.sh ...               569B     # layer
<missing>      RUN /bin/sh -c set -eux; apk add ...        36.7MB   # layer
<missing>      ENV REDIS_VERSION=7.4.1                     0B       # metadata
```

**Rule of thumb.** Content-adding instructions (`FROM`, `RUN`, `COPY`, `WORKDIR`) create layers. Metadata-only instructions (`ENV`, `EXPOSE`, `CMD`, `ENTRYPOINT`) do not.

## Cleanup

```bash
$ podman rmi redis:alpine alpine:latest golang:latest
```

Pulling by digest leaves an image without a tag. Find it with `podman images` and remove it by its IMAGE ID: `podman rmi <IMAGE ID>`.

## Verification checklist

- [ ] You saw that a repeated pull does not download layers again
- [ ] You pulled an image by digest
- [ ] You inspected a manifest list and saw multiple architectures
- [ ] You can explain why `EXPOSE` is 0 B but `RUN` produces a layer

---

# Lab 4 — Containerize a Node.js app

**Goal.** Take a Node.js app from GitHub, write a Dockerfile, build the image, run it as a container, and optionally push to Docker Hub.

**Prereqs.** Labs 0–3, `git` installed.

## Steps

### 1. Clone the sample app

```bash
$ git clone https://github.com/nigelpoulton/ddd-book.git
$ cd ddd-book/node-app

$ ls -l
-rw-r--r--@  app.js
drwxr-xr-x  node_modules
-rw-r--r--   package-lock.json
-rw-r--r--@  package.json
drwxr-xr-x   views
```

This directory is the **build context** — everything the build can see.

### 2. Write the Dockerfile

> **Docker:** `docker init` (Docker Desktop) asks a few questions and generates `Dockerfile`, `.dockerignore`, `compose.yaml` and `README.Docker.md`. Podman has no equivalent (`podman init` is a different command that starts a created container), so you write the file by hand.

Create `Dockerfile` with this content (it is what `docker init` generates for Node):

```dockerfile
ARG NODE_VERSION=23.3.0
FROM node:${NODE_VERSION}-alpine
ENV NODE_ENV production
WORKDIR /usr/src/app
RUN --mount=type=bind,source=package.json,target=package.json \
    --mount=type=bind,source=package-lock.json,target=package-lock.json \
    --mount=type=cache,target=/root/.npm \
    npm ci --omit=dev
USER node
COPY . .
EXPOSE 8080
CMD node app.js
```

What it does:

- Pins the Node version (no `latest`)
- Uses an Alpine base for size
- Sets `NODE_ENV=production` for performance
- Installs **only prod dependencies** with a cached npm dir
- Runs as a **non-root** user
- Copies app code last (cache-friendly)
- Documents the network port and the start command

### 3. Build the image

```bash
$ podman build -t ddd-book:ch8.node .

[+] Building 16.2s (12/12) FINISHED
 => [stage-0 1/4] FROM node:23.3.0-alpine               3.0s
 => [stage-0 2/4] WORKDIR /usr/src/app                  0.2s
 => [stage-0 3/4] RUN --mount=type=bind,... npm ci ...  1.1s
 => [stage-0 4/4] COPY . .                              0.1s
 => => writing image sha256:24dd040fa06b...

$ podman images ddd-book
REPO        TAG            IMAGE ID       CREATED             SIZE
ddd-book    ch8.node       24dd040fa06b   18 minutes ago      242MB
```

### 4. Run the container

```bash
$ podman run -d --name c1 -p 5005:8080 ddd-book:ch8.node

$ podman ps
ID    IMAGE              COMMAND           PORTS                    NAMES
49..  ddd-book:ch8.node  "node ./app.js"   0.0.0.0:5005->8080/tcp   c1
```

Browse `http://localhost:5005`. Your app is live.

### 5. (Optional) push to Docker Hub

You need a free [Docker Hub](https://hub.docker.com) account.

```bash
$ podman login

$ podman tag ddd-book:ch8.node <your-id>/ddd-book:ch8.node

$ podman push <your-id>/ddd-book:ch8.node
The push refers to repository [docker.io/<your-id>/ddd-book]
e4ef261755c8: Pushed
2a2799ae89a2: Mounted from library/node    # reused base layer
ch8.node: digest: sha256:24dd040f... size: 856
```

Other people can now `podman pull <your-id>/ddd-book:ch8.node` from anywhere.

## Cleanup

```bash
$ podman rm -f c1
$ podman rmi ddd-book:ch8.node <your-id>/ddd-book:ch8.node 2>/dev/null
```

## Verification checklist

- [ ] You wrote a Dockerfile and understand each line
- [ ] You built and ran the image
- [ ] You reached the app in the browser
- [ ] You understand why the image runs as `USER node` (security)

---

# Lab 5 — Multi-stage builds for slim images

**Goal.** Use a multi-stage Dockerfile to produce a tiny production image from a Go client+server project. Then learn how to build a single target image.

**Prereqs.** Labs 0–4.

## Steps

### 1. Clone and inspect

```bash
$ cd ../multi-stage
$ ls -l
-rw-rw-r-- Dockerfile
-rw-rw-r-- Dockerfile-final
drwxrwxr-x cmd
-rw-rw-r-- go.mod
-rw-rw-r-- go.sum
```

The `Dockerfile`:

```dockerfile
FROM golang:1.23.4-alpine AS base
WORKDIR /src
COPY go.mod go.sum .
RUN go mod download
COPY . .

FROM base AS build-client
RUN go build -o /bin/client ./cmd/client

FROM base AS build-server
RUN go build -o /bin/server ./cmd/server

FROM scratch AS prod
COPY --from=build-client /bin/client /bin/
COPY --from=build-server /bin/server /bin/
ENTRYPOINT [ "/bin/server" ]
```

Each `FROM` opens a new stage. The final `prod` stage uses `FROM scratch` (an empty image) and copies only the compiled binaries.

### 2. Build and compare sizes

```bash
$ podman build --jobs 2 -t multi:full .

STEP 1/…: FROM golang:alpine AS build-client
...
Successfully tagged localhost/multi:full

$ podman images multi
REPO   TAG    IMAGE ID       CREATED         SIZE
localhost/multi  full   a7a01440f2b5   5 seconds ago   17.9MB
```

The final image is **under 20 MB** (17.9 MB on the test VM) — compared to ~350 MB for the `golang:alpine` base alone. `--jobs 2` lets Podman build the independent `build-client` and `build-server` stages at the same time; by default Podman builds one stage at a time (`--jobs 1`).

> **Docker:** BuildKit runs independent stages in parallel by default, no flag needed. Exact sizes depend on the Go and Alpine versions.

### 3. Build a single target

Looking at `Dockerfile-final` you'll see two final stages: `prod-client` and `prod-server`. You can target each independently:

```bash
$ podman build -t multi:client --target prod-client -f Dockerfile-final .
$ podman build -t multi:server --target prod-server -f Dockerfile-final .

$ podman images multi
REPOSITORY  TAG       IMAGE ID       SIZE
multi       full      a7a01440f2b5   17.9MB
multi       server    a75778df1b9c   7.9MB
multi       client    02b621e9415f   8.0MB
```

Each single-binary image is ~8 MB — easier to push and pull.

### 4. Inspect the final image

```bash
$ podman history multi:full
IMAGE          CREATED         CREATED BY                          SIZE
a7a01440f2b5   2 minutes ago   ENTRYPOINT ["/bin/server"]          0B
<missing>      2 minutes ago   COPY /bin/server /bin/ # buildkit   8.64MB
<missing>      2 minutes ago   COPY /bin/client /bin/ # buildkit   8.53MB
```

Only two layers. No Go toolchain, no glibc, no shell. Smaller and harder to attack.

## Best practices for slim images

1. Multi-stage for compiled languages (Go, Rust, Java, .NET)
2. Final stage `FROM scratch`, `alpine`, or [distroless](https://github.com/GoogleContainerTools/distroless)
3. Pin versions (`node:23.3.0-alpine`, not `:latest`)
4. Order Dockerfile from **stable** at the top to **frequently-changed** at the bottom — for cache hits
5. Use `.dockerignore` to keep `node_modules`, `.git`, build artefacts out of the build context

## Cleanup

```bash
$ podman rmi multi:full multi:client multi:server
```

## Verification checklist

- [ ] You built with `--jobs 2` and understand why independent stages can run in parallel
- [ ] Final image is significantly smaller than the build stage's base
- [ ] You can build a single named target with `--target`

---

# Lab 6 — Multi-container app with Compose

**Goal.** Deploy a Python web app + Redis backend with Compose. Observe service discovery and volume persistence.

**Prereqs.** Labs 0–5.

> **Podman:** `podman compose` is a thin wrapper that runs an external compose provider. Install one first (Linux: `sudo apt install docker-compose-v2`, and keep `podman.socket` running, see Lab 0). Without it you get `looking up compose provider failed`. The same `compose.yaml` works with `docker compose`.

## Steps

### 1. Clone and inspect

```bash
$ cd ../multi-container
$ ls -l
drwxrwxr-x app/
-rw-rw-r-- Dockerfile
-rw-rw-r-- requirements.txt
-rw-rw-r-- compose.yaml
```

The `compose.yaml`:

```yaml
services:
  web-fe:
    build: .
    command: python app/app.py
    ports:
      - target: 8080
        published: 5001
    networks:
      - counter-net
  redis:
    image: "redis:alpine"
    networks:
      counter-net:
    volumes:
      - type: volume
        source: counter-vol
        target: /data
networks:
  counter-net:
volumes:
  counter-vol:
```

Two services, one user-defined network, one named volume.

### 2. Deploy the stack

```bash
$ podman compose up --detach
[+] Running 5/5
 - web-fe                                Built
 - Network multi-container_counter-net   Created
 - Volume multi-container_counter-vol    Created
 - Container multi-container-redis-1     Started
 - Container multi-container-web-fe-1    Started
```

Compose names resources `<project>_<resource>` where the project is the build-context directory.

```bash
$ podman compose ps
NAME                         SERVICE    STATUS      PORTS
multi-container-redis-1      redis      Up 5 sec    6379/tcp
multi-container-web-fe-1     web-fe     Up 5 sec    0.0.0.0:5001->8080
```

### 3. Exercise the app

Browse `http://localhost:5001` and refresh several times. The counter increments and Redis stores the value.

> The first request right after `up` can come back empty while the app is still starting. Wait a few seconds and retry.
>
> **Podman:** `podman compose ps` shows the container port (`8080/tcp`) without the host mapping. `podman port <container>` shows the real mapping (`0.0.0.0:5001`).

```bash
$ podman compose exec web-fe cat app/app.py | grep -A1 redis
cache = redis.Redis(host='redis', port=6379)
```

The web app uses `host='redis'`, **not** an IP. Compose's user-defined network has built-in DNS — services find each other by **service name**.

### 4. Observe volume persistence

```bash
$ podman compose down
[+] Running 3/3
 - Container multi-container-redis-1     Removed
 - Container multi-container-web-fe-1    Removed
 - Network multi-container_counter-net   Removed

$ podman volume ls
DRIVER  VOLUME NAME
local   multi-container_counter-vol     # ← still here

$ podman compose up --detach
```

Refresh the browser — the counter **continues** from where you left it. Redis read the prior count from the persistent volume.

### 5. Useful sub-commands

```bash
$ podman compose ps                # service + port summary
$ podman compose logs --tail 20    # last 20 log lines from all services
$ podman compose logs -f redis     # follow logs from a single service
$ podman top multi-container-redis-1  # processes inside one container
$ podman compose restart web-fe    # restart a single service
$ podman compose stop              # stop without removing
$ podman compose ls                # high-level status of all stacks
```

## Cleanup

```bash
$ podman compose down --volumes --rmi all
[+] Running 6/6
 - Container multi-container-web-fe-1    Removed
 - Container multi-container-redis-1     Removed
 - Volume multi-container_counter-vol    Removed
 - Image multi-container-web-fe:latest   Removed
 - Image redis:alpine                    Removed
 - Network multi-container_counter-net   Removed
```

## Verification checklist

- [ ] You ran a 2-service Compose stack
- [ ] You can explain why `host='redis'` works (user-defined network DNS)
- [ ] You proved a volume survives `podman compose down`
- [ ] You know the difference between `down`, `down --volumes`, and `down --rmi all`

---

# Lab 7 — Bridge networks deep dive

**Goal.** Create a user-defined bridge network, attach containers, watch them resolve each other by name, then publish a port.

**Prereqs.** Labs 0–6. **Recommended environment:** a Linux host. On macOS the bridges live inside the `podman machine` VM, so host-level commands such as `ip link` must be run inside it (`podman machine ssh`).

## Steps

### 1. Create a user-defined bridge

```bash
$ podman network create -d bridge localnet
f918f1bb0602...

$ podman network ls
NETWORK ID    NAME        DRIVER
2f259bab93aa  podman      bridge      # default network — no DNS
f918f1bb0602  localnet    bridge      # yours — DNS works
```

> **Docker:** the default network is called `bridge`, and `docker network ls` also lists `host` and `none`.

On Linux you can see the corresponding kernel bridge:

```bash
$ ip -br link show type bridge
podman1   UP   ...        # the bridge of your localnet network
```

> Podman names kernel bridges `podman0`, `podman1`, ... (the default network uses `podman0`). Docker uses `docker0` for the default network and `br-<id>` for user-defined ones, and `brctl show` needs the `bridge-utils` package.

### 2. Attach two containers and ping by name

```bash
$ podman run -d --name c1 --network localnet alpine sleep 1d
$ podman run -it --name c2 --network localnet alpine sh

# Inside c2
# ping c1
PING c1 (172.21.0.2): 56 data bytes
64 bytes from 172.21.0.2: seq=0 ttl=64 time=1.564 ms
64 bytes from 172.21.0.2: seq=1 ttl=64 time=0.338 ms
^C
# exit
```

**Key insight.** The network's built-in DNS server (`aardvark-dns` in Podman, an embedded DNS server in Docker) registers container names automatically — but **only on user-defined networks**. The default network does not have DNS resolution.

### 3. Publish a port to the outside world

```bash
$ podman run -d --name web \
    --network localnet \
    --publish 5005:80 \
    nginx

$ podman port web
80/tcp -> 0.0.0.0:5005

# From outside the host:
$ curl http://localhost:5005 | head -5
<!DOCTYPE html>
<html>
<head><title>Welcome to nginx!</title>

# From inside another container on the same network — by name:
$ podman exec c1 wget -qO- http://web:80 | head -5
```

Two ways to reach a service: by **port** from outside, by **name** from inside.

### 4. Inspect the network

```bash
$ podman ps --filter network=localnet --format '{{.Names}}  {{.Networks}}'
web  localnet
c2   localnet
c1   localnet

$ podman network inspect localnet
```

`podman network inspect` shows the subnet, gateway and options of the network. It does not list the attached containers, so use the `ps` filter for that.

> **Docker:** `docker network inspect localnet --format '{{json .Containers}}' | jq` lists the attached containers with their IP addresses. This template does not exist in Podman (`can't evaluate field Containers`).

## Cleanup

```bash
$ podman rm -f c1 c2 web
$ podman network rm localnet
```

## Verification checklist

- [ ] You created a user-defined bridge
- [ ] Two containers pinged each other **by name**
- [ ] You published a port and reached the container from your browser
- [ ] You know why the default `bridge` network is not the one to use

---

# Lab 8 — Volumes & persistent data

**Goal.** Create a volume, write data into it through one container, prove the data survives container deletion, mount it into a fresh container.

**Prereqs.** Labs 0–7.

## Steps

### 1. Auto-create a volume by reference

> **Podman:** `--mount` needs an explicit `type=`. Without it Podman fails with `incorrect mount format: should be --mount type=<bind|glob|tmpfs|volume>`. Docker accepts `--mount source=...,target=...` and defaults to a volume.

```bash
$ podman run -it --name voltainer \
    --mount type=volume,source=bizvol,target=/vol \
    alpine

# Inside the container
# ls /vol
# echo "I promise to write a book review on Amazon" > /vol/file1
# ls /vol
file1
# cat /vol/file1
I promise to write a book review on Amazon
```

Detach with **Ctrl-P Q**.

```bash
$ podman volume ls
DRIVER  VOLUME NAME
local   bizvol         # ← Docker auto-created it
```

If the named volume already exists, Docker reuses it. If not, Docker creates it.

### 2. Volume is protected while in use

```bash
$ podman volume rm bizvol
Error: volume is in use - [b44d3f82...dd2029ca]
```

### 3. Container deletion does NOT remove the volume

```bash
$ podman rm -f voltainer
voltainer

$ podman ps -a              # gone
$ podman volume ls          # bizvol still here

# Mount into a fresh container
$ podman run -it --name newctr \
    --mount type=volume,source=bizvol,target=/vol \
    alpine sh

# cat /vol/file1
I promise to write a book review on Amazon
```

Same volume, new container, same data. **Stateful apps belong in volumes**, not the ephemeral writable layer.

### 4. Volume commands reference

```bash
$ podman volume create myvol           # explicit pre-create
$ podman volume ls                     # list all volumes
$ podman volume inspect bizvol         # driver, mountpoint, scope
$ podman volume prune                  # delete all unused volumes — CAUTION
$ podman volume rm bizvol              # delete a specific unused volume
```

### 5. Locating volume data on disk (Linux only)

> **Docker:** the path is `/var/lib/docker/volumes/<name>/_data`. Rootless Podman keeps volumes under `~/.local/share/containers/storage/volumes/` (not verified here). On macOS the data lives inside the `podman machine` VM.

```bash
$ podman volume inspect bizvol --format '{{ .Mountpoint }}'
/var/lib/containers/storage/volumes/bizvol/_data

$ sudo ls -l /var/lib/containers/storage/volumes/bizvol/_data
-rw-r--r-- 1 root root 50 Jan 12 14:25 file1
```

Don't edit volume contents directly via the host filesystem — only via containers.

## Cleanup

```bash
# Detach (Ctrl-P Q) first, then:
$ podman rm -f newctr
$ podman volume rm bizvol
```

## Verification checklist

- [ ] You wrote a file into a volume and read it back from a new container
- [ ] You confirmed `podman volume rm` refuses while the volume is in use
- [ ] You confirmed the volume survives `podman rm -f`
- [ ] You can name the difference between a volume and the writable layer

---

# Lab 9 — Build a Swarm + deploy a stack

> **Optional / reference:** нужен Swarm из нескольких нод (Play With Docker или несколько VM), Docker Desktop не подходит.

**Goal.** Build a Swarm cluster (2+ nodes), deploy a multi-service stack declaratively, perform a rolling update.

**Prereqs.** Labs 0–8.

**Environment.** [Play With Docker](https://labs.play-with-docker.com/) (free, browser) or two Multipass VMs. **Not** Docker Desktop (single node).

**Required open ports** between nodes:

- `2377/tcp` — management plane
- `7946/tcp` and `7946/udp` — control plane (SWIM gossip)
- `4789/udp` — VXLAN data plane

## Steps

### 1. Initialize the Swarm on the first manager

```bash
# On node1
$ docker swarm init
Swarm initialized: current node (b8slc7l29...) is now a manager.

To add a worker to this swarm, run the following command:
    docker swarm join --token SWMTKN-1-2hl6...-...3lqg 172.31.40.192:2377
```

If the node has multiple IPs, you'll be prompted for `--advertise-addr <ip>`.

### 2. Add workers

On each future worker node, paste the join command from step 1:

```bash
# On node2, node3, ...
$ docker swarm join --token SWMTKN-1-2hl6...-...3lqg 172.31.40.192:2377
This node joined a swarm as a worker.
```

### 3. (Optional) add managers for HA

```bash
# On node1
$ docker swarm join-token manager
To add a manager to this swarm, run the following command:
    docker swarm join --token SWMTKN-1-... <node1-ip>:2377
```

Paste on future managers. **Use 3 or 5 managers in production** (Raft tolerates `(N-1)/2` failures).

### 4. Verify the cluster

```bash
# On any manager
$ docker node ls
ID                      HOSTNAME   STATUS   AVAILABILITY   MANAGER STATUS
b8slc7l29tgdetxgy... *  node1      Ready    Active         Leader
y43jr1d754pbjv3arlh...  node2      Ready    Active         Reachable
k1npnfxr7ykueac4jov...  node3      Ready    Active         Reachable
w3e321uxty2quuqnsk1...  node4      Ready    Active         (worker)
kbodotf68tz8dne2ktk...  node5      Ready    Active         (worker)
```

### 5. Deploy a stack

```bash
$ git clone https://github.com/nigelpoulton/ddd-book.git 2>/dev/null
$ cd ddd-book/swarm-new

$ docker stack deploy -c compose.yaml ddd
Creating network ddd_counter-net
Creating service ddd_web-fe
Creating service ddd_redis
```

Inspect:

```bash
$ docker stack ps ddd
ID            NAME           NODE   DESIRED   CURRENT STATE
pgeupeqyqg5t  ddd_redis.1    wrk2   Running   Running 8 min
qbtkiz1p9v1n  ddd_web-fe.1   wrk1   Running   Running 8 min
wbs2ndy22xhh  ddd_web-fe.2   mgr1   Running   Running 8 min
skqz1mbreluo  ddd_web-fe.3   mgr2   Running   Running 8 min
sg5u9b6t8m44  ddd_web-fe.4   mgr3   Running   Running 8 min

$ docker stack services ddd
NAME         MODE   REPLICAS   IMAGE                              PORTS
ddd_redis    repl   1/1        redis:alpine
ddd_web-fe   repl   4/4        nigelpoulton/ddd-book:swarm-app    *:5001->8080
```

Browse **any node's IP** on port 5001 — Swarm's routing mesh handles the rest, even on nodes without replicas.

### 6. Perform a rolling update — **declaratively**

Edit `compose.yaml`:

```yaml
services:
  web-fe:
    image: nigelpoulton/ddd-book:swarm-appv2   # was swarm-app
    deploy:
      replicas: 10                              # was 4
```

```bash
$ docker stack deploy -c compose.yaml ddd
Updating service ddd_web-fe (id: rsm3x02o9fwc...)
Updating service ddd_redis (id: otr7927s9m1s...)
```

Watch the rollout:

```bash
$ docker stack ps ddd
NAME             IMAGE                              NODE   DESIRED   CURRENT STATE
ddd_web-fe.1     nigelpoulton/...:swarm-app         wrk1   Running   Running 8 min
ddd_web-fe.2     nigelpoulton/...:swarm-appv2       mgr1   Running   Running 13 sec
\_ddd_web-fe.2   nigelpoulton/...:swarm-app         mgr1   Shutdown  Shutdown 26 sec
ddd_web-fe.3     nigelpoulton/...:swarm-app         mgr2   Running   Running 8 min
```

Six new replicas come up immediately. The existing four are replaced **2 at a time with 10 s between batches** — exactly as `update_config` in the Compose file specified.

### Warning — never mix declarative and imperative

Real-world story: a colleague runs `docker service scale reporting=10` during a traffic spike. The Compose file in Git still says `replicas: 1`. Next time anyone runs `docker stack deploy`, Swarm sees observed=10, desired=1 → scales reporting back to 1 → outage.

**Always edit the Compose file, then `docker stack deploy`.** Keep Compose files in version control.

## Cleanup

```bash
$ docker stack rm ddd          # no confirmation prompt!
Removing service ddd_redis
Removing service ddd_web-fe
Removing network ddd_counter-net

$ docker volume rm ddd_counter-vol    # volume isn't auto-deleted

# Tear down the swarm — workers first, leader last
$ docker swarm leave            # on each worker
$ docker swarm leave --force    # on the leader
```

## Verification checklist

- [ ] You built a Swarm with multiple nodes
- [ ] You deployed a stack and reached it via any node's IP
- [ ] You performed a rolling update by editing the Compose file
- [ ] You understand why imperative scaling is dangerous

---

# Lab 10 — Overlay networking across hosts

> **Optional / reference:** требует работающий Swarm из lab 9 (несколько хостов).

**Goal.** Create an encrypted overlay network spanning your Swarm. Two replicas on different hosts ping each other on the same logical L2 subnet.

**Prereqs.** Lab 9 — you need a running Swarm.

## Steps

### 1. Create an encrypted overlay

```bash
# On a manager
$ docker network create -d overlay -o encrypted uber-net
vdu1yly429jvt04hgdm0mjqc6

$ docker network ls
NETWORK ID     NAME              DRIVER    SCOPE
4dsqo7jc36ip   ingress           overlay   swarm
vdu1yly429jv   uber-net          overlay   swarm   # ← yours
```

Without `-o encrypted` the control plane is still encrypted, but the **data plane is plaintext**. Use `-o encrypted` for cross-cloud or untrusted-underlay setups (about 10 % performance penalty).

> Workers only see the overlay **after** Docker schedules a container that needs it (lazy propagation).

### 2. Deploy a service across nodes

```bash
$ docker service create --name test \
    --network uber-net \
    --replicas 2 \
    ubuntu sleep infinity

$ docker service ps test
ID          NAME    NODE    DESIRED STATE   CURRENT STATE
sm1...1nw   test.1  node1   Running         Running
tro...kgk   test.2  node2   Running         Running
```

`docker network ls` on node2 now **does** show `uber-net`.

### 3. Cross-host ping

```bash
# On node1 — find the local replica ID
$ docker ps
CONTAINER ID    NAME
d7766923a5a7    test.1.tro80xq...kgk

$ docker exec -it d7766923a5a7 bash
# apt update && apt install -y iputils-ping traceroute

# ping 10.0.0.4   # IP of test.2 on node2
64 bytes from 10.0.0.4: icmp_seq=1 ttl=64 time=1.06 ms

# Traceroute shows a single hop, regardless of underlay topology
# traceroute 10.0.0.4
 1  test.2.sm180xqwm7k1bsyn3mt1fj1nw.uber-net (10.0.0.4)  1.110 ms
```

The "single hop" is the magic of VXLAN — the overlay collapses arbitrarily complex underlay topology into one logical L2 segment.

### 4. (Optional) layer-3 routing within an overlay

```bash
$ docker network create --subnet=10.1.1.0/24 --subnet=11.1.1.0/24 -d overlay prod-net
```

Docker creates two virtual switches inside the sandbox and routes between them automatically.

## Cleanup

```bash
$ docker service rm test
$ docker network rm uber-net      # may take a few seconds after service deletion
```

## Verification checklist

- [ ] You created an encrypted overlay
- [ ] You pinged across hosts and saw a single traceroute hop
- [ ] You understand VXLAN encapsulation (UDP/4789, VTEPs)
- [ ] You can explain why this works regardless of underlay topology

---

# Lab 11 — Vulnerability scanning with Docker Scout

> **Optional / reference:** команды `docker scout` нужны Docker Desktop. В Podman используйте `trivy image <image>` (https://aquasecurity.github.io/trivy/); для `trivy` выводы и номера версий будут другими.

**Goal.** Scan an image with Docker Scout, read the CVE report, and apply a base-image fix.

**Prereqs.** Docker Desktop (Scout CLI plugin ships pre-installed).

## Steps

### 1. Quick overview

```bash
$ docker scout quickview nigelpoulton/tu-demo:latest

    ✓ Pulled
    ✓ Image stored for indexing
    ✓ Indexed 66 packages

  Target             │   nigelpoulton/tu-demo:latest   │   0C   4H   2M    0L
    digest           │   b4210d0aa52f                  │
  Base image         │   python:3-alpine               │   0C   2H   1M    0L
  Updated base image │   python:3.11-alpine            │   0C   1H   1M    0L
```

The counts are **C**ritical / **H**igh / **M**edium / **L**ow vulnerabilities.

| Letter | Severity | Action |
|--------|----------|--------|
| C | Critical | Patch ASAP, block the release |
| H | High | Patch in next sprint |
| M | Medium | Triage and plan |
| L | Low | Watchlist |

### 2. Full CVE report

```bash
$ docker scout cves nigelpoulton/tu-demo:latest

    Detected 1 vulnerable package with 2 vulnerabilities

## Overview
                    │        Analyzed Image
────────────────────┼────────────────────────────────
  Target            │ nigelpoulton/tu-demo:latest
    platform        │ linux/arm64
    vulnerabilities │    0C     1H     1M     0L

## Packages and Vulnerabilities
   0C     1H     1M     0L expat 2.5.0-r2

    HIGH CVE-2023-52425
      https://scout.docker.com/v/CVE-2023-52425
      Affected range : <2.6.0-r0
      Fixed version : 2.6.0-r0
```

Each finding gives:

- The vulnerable **package + current version**
- The **CVE ID** with Scout report link
- The **affected range** and **fixed version**

### 3. Apply a base-image fix

In your Dockerfile:

```dockerfile
# Before
FROM python:3-alpine

# After (Scout's recommendation)
FROM python:3.11-alpine
```

Rebuild and re-scan:

```bash
$ docker build -t myapp:fixed .
$ docker scout quickview myapp:fixed
```

You should see fewer High/Medium findings.

### 4. Wire Scout into CI

```bash
$ docker scout cves --exit-code --only-severity critical,high myapp:fixed
```

Exit code is non-zero if anything Critical or High is found — fail the build.

## What Scout does NOT do

- Doesn't scan **networks, hosts, or orchestrators** — only images
- Different scanners use different methods: some only match package names; deep binary scanning catches more
- Once you know about a vulnerability, you **own** the responsibility to fix or accept it

## Cleanup

```bash
$ docker rmi nigelpoulton/tu-demo:latest myapp:fixed 2>/dev/null
```

## Verification checklist

- [ ] You ran `docker scout quickview` and `docker scout cves`
- [ ] You can name the four severity levels and CI-blocking strategy
- [ ] You upgraded a base image based on Scout's recommendation

---

# Lab 12 — Content Trust + Swarm Secrets

> **Optional / reference.** Docker Content Trust (Notary v1) выводится из эксплуатации, поэтому часть про подпись образов (шаги 1–5) — только для понимания. Современная подпись образов — cosign: см. [gitlab_ci_lab_cosign.md](../gitlab/gitlab_ci_lab_cosign.md). Шаг 6 (Swarm secrets) требует Swarm из lab 9.

**Goal.** Sign an image with Docker Content Trust, force-verify pulls, then deliver a secret to a Swarm service without it touching disk in plaintext.

**Prereqs.** Lab 9 (Swarm) for secrets, Docker Hub account for DCT.

## Steps

### 1. Generate a key pair

```bash
$ docker trust key generate myname
Generating key for myname...
Enter passphrase for new myname key with ID 1f78609:
Repeat passphrase for new myname key with ID 1f78609:
Successfully generated and loaded private key.
```

You'll see a `myname.pub` file in your current directory.

### 2. Associate the key with a repository

```bash
$ docker trust signer add --key myname.pub myname <your-id>/ddd-trust
Adding signer "myname" to <your-id>/ddd-trust...
Initializing signed repository for <your-id>/ddd-trust...
Successfully added signer: myname to <your-id>/ddd-trust
```

### 3. Sign and push an image

```bash
$ docker tag alpine:latest <your-id>/ddd-trust:signed
$ docker trust sign <your-id>/ddd-trust:signed
Successfully signed docker.io/<your-id>/ddd-trust:signed
```

### 4. Inspect trust data

```bash
$ docker trust inspect <your-id>/ddd-trust:signed --pretty

Signatures for <your-id>/ddd-trust:signed
  SIGNED TAG   DIGEST                               SIGNERS
  signed       30e6d35703c578ee...4fcbbcbb0f281     myname

List of signers and their keys for <your-id>/ddd-trust:signed
  SIGNER    KEYS
  myname    4d6f1bf55702

Administrative keys for <your-id>/ddd-trust:signed
  Repository Key:        5e72e54afafb8444f...
  Root Key:              40418fc47544ca630...
```

### 5. Enforce verification

```bash
$ export DOCKER_CONTENT_TRUST=1

# Unsigned pulls fail
$ docker pull alpine:edge
Error: remote trust data does not exist for docker.io/alpine: notary.docker.io
does not have trust data for docker.io/alpine

# Signed pulls work
$ docker rmi <your-id>/ddd-trust:signed
$ docker pull <your-id>/ddd-trust:signed
Pull (1 of 1): <your-id>/ddd-trust:signed@sha256:30e6...
```

```bash
# Remember to unset for the rest of the labs
$ unset DOCKER_CONTENT_TRUST
```

### 6. Swarm secrets (Swarm required)

```bash
# Create a secret from stdin
$ echo "s3cr3t-db-password" | docker secret create db_pass -
qz9k...

$ docker secret ls
ID          NAME      CREATED
qz9k...     db_pass   3 seconds ago

# Attach to a service
$ docker service create --name api \
    --secret source=db_pass,target=/run/secrets/db_pass \
    nigelpoulton/ddd-book:web0.1
```

The secret is mounted **read-only** at `/run/secrets/db_pass` on every replica, backed by an **in-memory tmpfs**. When the replica stops, the tmpfs is destroyed.

```bash
$ docker exec $(docker ps -q --filter name=api) cat /run/secrets/db_pass
s3cr3t-db-password
```

### What guarantees Swarm secrets provide

1. Encrypted **at rest** in the cluster store
2. Encrypted **in flight** from manager to worker
3. Mounted as **in-memory tmpfs** inside containers — never written to disk in plaintext
4. **Least-privilege** — only services explicitly granted access can read the secret

## Cleanup

```bash
$ docker service rm api
$ docker secret rm db_pass

$ unset DOCKER_CONTENT_TRUST
$ docker trust signer remove myname <your-id>/ddd-trust 2>/dev/null
$ rm -f myname.pub
```

## Verification checklist

- [ ] You signed an image and verified its trust data
- [ ] You enforced signed pulls with `DOCKER_CONTENT_TRUST=1`
- [ ] You delivered a secret to a service via `/run/secrets/`
- [ ] You can name the 4 guarantees Swarm secrets provide

---

# Lab 13 — Docker Model Runner + Compose chatbot (optional)

**Goal.** Enable Docker Model Runner, pull a quantized Gemma3 model, talk to it over the OpenAI-compatible API, then deploy a Compose chatbot stack that uses it.

**Prereqs.** Docker Desktop **4.41+** (ships Compose v2.35). Mac (Apple Silicon) or Windows with NVIDIA GPU recommended; CPU works (slow).

## Steps

### 1. Enable DMR

1. **Docker Desktop → Settings → Features in development**
2. Tick **Enable Docker Model Runner**
3. Tick **Enable host-side TCP support** (default port `12434`)
4. **Apply & restart**

```bash
$ docker model status
Docker Model Runner is running

Status:
llama.cpp: running llama.cpp latest-metal (sha256:ad58...)
```

### 2. Pull and test a model

```bash
$ docker model pull ai/gemma3:4B-Q4_K_M
Model pulled successfully

$ docker model ls
MODEL NAME             PARAMS   QUANTIZATION     ARCHITECTURE   SIZE
ai/gemma3:4B-Q4_K_M    3.88 B   IQ2_XXS/Q4_K_M   gemma3         2.31G

# CLI REPL test (no conversation memory)
$ docker model run ai/gemma3:4B-Q4_K_M
Interactive chat mode started. Type '/bye' to exit.

> How long is a day on Mars?
A "sol" is about 24 hours, 39 minutes, and 35 seconds long...
> /bye
```

For a chat UI **with** history, use the **Models** tab in Docker Desktop.

### 3. Talk to DMR over the OpenAI-compatible API

```bash
$ curl -s http://localhost:12434/engines/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "ai/gemma3:4B-Q4_K_M",
    "messages": [
       { "role": "system", "content": "Keep your responses to one sentence only." },
       { "role": "user",   "content": "How long is a day on Mars?" }
    ],
    "temperature": 0.7,
    "max_tokens": 500
  }' | jq -r '.choices[0].message.content'

A day on Mars, also known as a sol, is approximately 24.6 hours long.
```

Any OpenAI-compatible client SDK works — just point it at `localhost:12434/engines/v1`.

### 4. Deploy the Compose chatbot

The repo includes a 3-service chatbot in `ddd-book/dmr/`:

```yaml
services:
  frontend:
    build: ./frontend
    ports: ["3000:3000"]
    env_file: [.env]
    depends_on: [backend]
  backend:
    build: ./backend
    ports: ["8000:8000"]
    env_file: [.env]
    depends_on: [dmr]
  dmr:
    provider:
      type: model
      options:
        model: ${LLM_MODEL_NAME}
```

`.env`:

```
MODEL_HOST=http://model-runner.docker.internal/engines/v1
LLM_MODEL_NAME=ai/gemma3:4B-Q4_K_M
```

```bash
$ cd ddd-book/dmr
$ docker compose up --build --detach
```

Browse `http://localhost:3000` — chat with your local LLM. The frontend talks to the backend on the project's default network; the backend talks to DMR via `http://model-runner.docker.internal/engines/v1`.

### Where DMR fits

- DMR runs **outside containers**, on the host, so it can access GPU/NPU/TPU hardware
- Models are pulled from Docker Hub's `ai/` namespace as a new OCI artifact type
- Containers on the same host can reach DMR at `model-runner.docker.internal`
- Compose v2.35+ supports `provider: type: model` so DMR is a first-class service

## Cleanup

```bash
$ docker compose down --rmi all --volumes
$ docker model rm ai/gemma3:4B-Q4_K_M
```

(Disable DMR in Settings if you don't want it running.)

## Verification checklist

- [ ] You pulled a model and ran a CLI chat
- [ ] You called DMR over the OpenAI API with `curl`
- [ ] You deployed a Compose chatbot that uses DMR as a service
- [ ] You understand why DMR runs outside containers

---

# Lab 14 — Wasm containers (optional)

**Goal.** Write a Rust HTTP server, compile it to Wasm, package it as a 100 KB Docker image, and run it with the Spin runtime.

**Prereqs.**

- Docker Desktop 4.37+ with **Wasm** enabled in Settings → Features in development
- *Use containerd for pulling and storing images* must also be enabled
- **Rust 1.82+**: `rustup target add wasm32-wasip1`
- **Spin 3.1+**: search "install Fermyon spin"; verify with `spin --version`

> Wasm support in Docker Desktop is in **beta** and doesn't work with Multipass VMs.

## Steps

### 1. Scaffold a Rust HTTP component

```bash
$ spin new hello-world -t http-rust
Description: Wasm app
HTTP path: /hello

$ cd hello-world
$ tree
.
├── Cargo.toml
├── spin.toml
└── src
    └── lib.rs
```

Edit `src/lib.rs` — change the response body:

```rust
.body("Docker loves Wasm")?
```

### 2. Compile to Wasm

```bash
$ spin build
Building component hello-world with `cargo build --target wasm32-wasip1 --release`
Finished building all Spin components

$ ls target/wasm32-wasip1/release/hello_world.wasm
target/wasm32-wasip1/release/hello_world.wasm
```

You can run it locally to verify:

```bash
$ spin up
Serving http://127.0.0.1:3000
Available Routes:
  hello-world: http://127.0.0.1:3000/hello
```

Browse `http://127.0.0.1:3000/hello` — you should see your message. Press **Ctrl-C** to stop.

### 3. Containerize

Update `spin.toml` so the path matches the image layout (the `COPY` lands the binary at `/`):

```toml
[component.hello-world]
source = "hello_world.wasm"
```

Create a `Dockerfile`:

```dockerfile
FROM scratch
COPY /target/wasm32-wasip1/release/hello_world.wasm .
COPY spin.toml .
```

Build the Wasm image:

```bash
$ docker build --platform wasi/wasm --provenance=false \
    -t <your-id>/ddd-book:wasm .

$ docker images
REPOSITORY              TAG    IMAGE ID         SIZE
<your-id>/ddd-book      wasm   7b55889f1006     104kB    # ← yes, kilobytes
```

If your Docker is older, try `docker buildx build` instead of `docker build`.

### 4. Push (optional)

```bash
$ docker push <your-id>/ddd-book:wasm
The push refers to repository [docker.io/<your-id>/ddd-book]
301823195c36: Pushed
8966226af76a: Pushed
wasm: digest: sha256:7b55889f1006...8ae4c size: 695
```

Docker Hub will recognize it as a `wasi/wasm` image. (Image scanners don't yet analyze Wasm images.)

### 5. Run with the Spin Wasm runtime

```bash
$ docker run -d --name wasm-ctr \
    --runtime=io.containerd.spin.v2 \
    --platform=wasi/wasm \
    -p 5556:80 \
    <your-id>/ddd-book:wasm /

$ docker ps
CONTAINER ID    IMAGE                       STATUS
....            <your-id>/ddd-book:wasm     Up 2 seconds
```

Browse `http://localhost:5556/hello` — you should see "Docker loves Wasm".

## Cleanup

```bash
$ docker rm -f wasm-ctr
$ docker rmi <your-id>/ddd-book:wasm
```

## Verification checklist

- [ ] You compiled a Rust app to Wasm with `spin build`
- [ ] You produced a Wasm container image with `FROM scratch`
- [ ] You ran it with `--runtime=io.containerd.spin.v2`
- [ ] You can explain why image size is so small (no Linux OS in the image)

---

# Appendix — Cheat sheet

## Most-used commands

```bash
# Images
docker pull <image>[:tag]              # download
docker images                          # list local
docker build -t name:tag .             # build from Dockerfile in cwd
docker push <repo>:<tag>               # upload to registry
docker rmi <image>                     # delete

# Containers
docker run -d --name n -p H:C <image>  # run detached, name, publish port
docker run -it --rm <image> <cmd>      # run interactive, auto-clean
docker ps                              # running
docker ps -a                           # all (incl. stopped)
docker exec -it <name> sh              # shell into a running container
docker logs -f <name>                  # follow logs
docker stop <name> && docker rm <name> # graceful stop + remove
docker rm -f <name>                    # force remove

# Networks
docker network create -d bridge <name>
docker network ls
docker network inspect <name>
docker network rm <name>

# Volumes
docker volume create <name>
docker volume ls
docker volume inspect <name>
docker volume rm <name>

# Compose
docker compose up --detach
docker compose ps
docker compose logs -f [<service>]
docker compose exec <service> <cmd>
docker compose down [--volumes] [--rmi all]

# Swarm
docker swarm init [--advertise-addr <ip>]
docker swarm join-token (manager|worker)
docker node ls
docker stack deploy -c compose.yaml <stack>
docker stack ps <stack>
docker stack rm <stack>
docker swarm leave [--force]

# Security
docker scout quickview <image>
docker scout cves <image>
docker secret create <name> -    # from stdin
docker trust sign <repo>:<tag>
```

## Keyboard reflexes

| Keys | Effect |
|------|--------|
| **Ctrl-P Q** | Detach from a running container without killing PID 1 |
| **Ctrl-C** | Send SIGINT — kills foreground containers (and `spin up`) |
| **Ctrl-D** | EOF — usually exits a container's shell same as `exit` |

## Common Dockerfile instructions

| Instruction | Creates layer? | Purpose |
|-------------|---------------|---------|
| `FROM` | Yes | Base image (one per stage) |
| `RUN` | Yes | Execute a shell command |
| `COPY` | Yes | Copy files from context |
| `ADD` | Yes | Like COPY plus URL/tar handling |
| `WORKDIR` | Tiny | Default directory for following instructions |
| `ENV` | No | Set env var |
| `ARG` | No | Build-time arg |
| `EXPOSE` | No | Document a network port |
| `USER` | No | Drop root before running |
| `ENTRYPOINT` | No | Default command (overridable with `--entrypoint` in `docker run`) |
| `CMD` | No | Default args (overridable on CLI) |
| `HEALTHCHECK` | No | Container health probe |

## Glossary (most-used terms)

- **Image** — read-only blueprint with app + deps + just-enough OS
- **Container** — running instance of an image with a thin writable layer
- **Layer** — one fragment of an image (read-only)
- **Tag** — mutable human-friendly image name (`nginx:latest`)
- **Digest** — immutable cryptographic hash of image content (`@sha256:...`)
- **Build context** — directory sent to the builder (where your `Dockerfile` lives)
- **Compose** — YAML spec for multi-container apps managed via `docker compose`
- **Service** — Swarm concept: one or more identical containers managed together
- **Stack** — a deployed Compose file in Swarm (collection of services + networks + volumes)
- **Overlay** — multi-host L2 network built on VXLAN
- **Volume** — first-class Docker object for persistent data, decoupled from containers
- **OCI** — Open Container Initiative; the standards body for image/runtime/distribution

---

*Companion to the slide deck `../docker-basics-slides.md`. Source: Nigel Poulton, Docker Deep Dive, May 2025.*
