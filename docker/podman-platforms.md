# Podman: macOS, Linux и Docker

Основной инструмент всех лабораторных курса: **Podman**. Эта страница собирает, чем отличается работа на macOS и на Linux, и чем Podman отличается от Docker. В лабах такие места помечены врезками `> **macOS:**`, `> **Linux:**`, `> **Docker:**`.

> Что проверено: Ubuntu 24.04, `amd64`, rootful Podman 4.9.3, `netavark` и `aardvark-dns`. Не проверялись: macOS, Apple Silicon, rootless-режим. Такие места ниже помечены "не проверено".

## Установка

### Linux (Ubuntu 24.04)

```bash
sudo apt install podman netavark aardvark-dns docker-compose-v2
echo 'unqualified-search-registries = ["docker.io"]' | sudo tee /etc/containers/registries.conf.d/00-docker-hub.conf
sudo systemctl enable --now podman.socket
```

- `netavark` и `aardvark-dns`: сеть контейнеров и имена в user-defined сетях. Без них `kind create cluster` зависает на `Joining worker nodes`.
- Строка в `registries.conf.d`: без неё `podman pull nginx` падает с `short-name ... did not resolve to an alias`.
- `docker-compose-v2`: провайдер для `podman compose`.
- Для скриптов, которые вызывают `docker`: `sudo ln -s "$(command -v podman)" /usr/local/bin/docker`. В интерактивной оболочке хватает `alias docker=podman` (в скриптах алиас не работает).

### macOS

```bash
brew install podman
podman machine init --rootful --cpus 4 --memory 8192
podman machine start
```

Контейнерам нужно ядро Linux, поэтому Podman запускает небольшую VM (`podman machine`). `--rootful` понадобится для kind в модуле 2. Команды уровня хоста (`ip link`, пути к томам) выполняйте внутри машины: `podman machine ssh`.

### rootful и rootless

Лабы проверены под root (rootful). Rootless не проверялся. Известно из документации Podman: rootless-пользователь не может занимать порты ниже 1024, а kind в модулях 2-8 требует rootful.

## Что отличается от Docker

| Тема | Podman | Docker |
|---|---|---|
| Демон | нет, контейнеры запускает сам процесс `podman` | `dockerd` |
| `version` | только блок Client на Linux; Client и Server на macOS | всегда Client и Server |
| Короткие имена образов | нужна строка в `registries.conf.d`, иначе ошибка `short-name` | работают сразу (Docker Hub) |
| Имена образов в выводе | `docker.io/library/nginx`, `localhost/lab:1` | `nginx`, `lab:1` |
| `--mount` | нужен явный `type=`: `--mount type=volume,source=v,target=/v` | по умолчанию том |
| Дефолтная сеть | `podman` (без DNS) | `bridge` (без DNS) |
| Имя bridge в ядре | `podman0`, `podman1`, ... | `docker0`, `br-<id>` |
| Каталог томов | `/var/lib/containers/storage/volumes/` | `/var/lib/docker/volumes/` |
| `inspect` сети | `.Containers` нет, используйте `podman ps --filter network=NAME` | `.Containers` есть |
| `inspect` образа | `podman image inspect IMG --format '{{json .HealthCheck}}'` | `docker inspect IMG --format '{{.Config.Healthcheck.Test}}'` |
| Формат образа по умолчанию | OCI: `HEALTHCHECK` теряется, нужен `--format docker` | образ Docker, `HEALTHCHECK` сохраняется |
| Параллельные стадии сборки | по умолчанию одна (`--jobs 1`), включается `--jobs N` | BuildKit параллелит сам |
| `docker init` | нет (`podman init` это другая команда) | генерирует Dockerfile и compose.yaml |
| `buildx`, мультиархитектурная сборка | `podman build --platform ... --manifest`, нужен QEMU (`qemu-user-static`) и реестр | `docker buildx build --platform ... --push` |
| Дайджест образа | `podman image inspect IMG --format '{{.Digest}}'`, `podman manifest inspect IMG` | `docker buildx imagetools inspect IMG` |
| Compose | `podman compose` вызывает внешний провайдер (`docker-compose-v2`); `compose top` падает, используйте `podman top CONTAINER` | `docker compose` встроен |
| Автозапуск после перезагрузки | политика `--restart always` срабатывает, если включён `podman-restart.service` (не проверено) | демон сам |
| Swarm, Scout, Model Runner, Wasm | нет | есть (Docker Desktop или Engine) |
| macOS | контейнеры внутри `podman machine`, публикуемые порты пробрасываются на localhost | внутри Docker Desktop VM |

## Осторожно: `system prune`

`podman system prune` и `docker system prune` удаляют **все** неиспользуемые образы и остановленные контейнеры на машине, а не только ваши. На общей машине (например, на учебной VM, где работает кластер kind) это ломает чужую работу. Удаляйте только свои ресурсы по имени: `podman rm NAME`, `podman rmi IMAGE`, `podman volume rm NAME`.

## Чего нет в Podman

Лабы 9-14 из `docker-labs.md` (Swarm, overlay-сети, Docker Scout, Content Trust, Model Runner, Wasm) оставлены на Docker: у Podman нет Swarm и Docker-специфичных функций. Они нужны только если вы хотите разобрать именно эти темы.
