# Lab 20 — Kustomize: base + overlays без шаблонов

**Цель:** управлять манифестами для разных окружений (dev/prod) через
**Kustomize** — встроенный в `kubectl` (`apply -k`), без шаблонизаторов: общий
`base` + накладываемые `overlays`.

**Время:** 25 минут
**Prerequisites:** module 03, Lab 08 — rolling update, module 03, Lab 21 — ConfigMap в env.

> Kustomize встроен в `kubectl` — отдельно ничего ставить не нужно. Helm
> (шаблоны/values) разбирается в отдельном модуле; Kustomize — это
> **patch-based** альтернатива «без логики в YAML».

---

## BLUF — Kustomize vs Helm

| | Kustomize | Helm |
|---|---|---|
| Подход | patch/overlay поверх чистого YAML | шаблоны Go + `values.yaml` |
| Синтаксис | обычный валидный YAML (без `{{ }}`) | шаблонизатор |
| Встроен в kubectl | **да** (`kubectl apply -k`) | нет (отдельный CLI) |
| Когда удобнее | свои манифесты, разные окружения | упаковка/дистрибуция чужих приложений |

Идея: один **base** (общие манифесты) + **overlays** на каждое окружение,
которые **патчат** base (имя, реплики, образ, лейблы, конфиги).

---

## Шаг 1. Структура

```
kustomize-demo/
├── base/
│   ├── deployment.yaml
│   └── kustomization.yaml
└── overlays/
    ├── dev/
    │   └── kustomization.yaml
    └── prod/
        └── kustomization.yaml
```

`base/deployment.yaml`:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata: {name: web}
spec:
  replicas: 1
  selector: {matchLabels: {app: web}}
  template:
    metadata: {labels: {app: web}}
    spec:
      containers:
      - name: web
        image: nginx:1.27-alpine
        envFrom:
        - configMapRef: {name: app-config}   # ConfigMap генерируется в base, см. ниже
```

`base/kustomization.yaml`:
```yaml
resources:
- deployment.yaml
configMapGenerator:                # сгенерировать ConfigMap (+ хеш в имени)
- name: app-config
  literals: [LOG_LEVEL=info]
```

> ConfigMap лежит в `base`, поэтому его видят все overlays, а Deployment на него ссылается через `envFrom`.

```bash
# base сам по себе уже применим:
kubectl kustomize kustomize-demo/base        # отрендерить в stdout
```

---

## Шаг 2. Prod overlay — патчим base

`overlays/prod/kustomization.yaml`:
```yaml
namePrefix: prod-                  # web → prod-web
labels:                            # современный синтаксис (не commonLabels!)
- pairs: {env: prod}
  includeSelectors: true
resources:
- ../../base                       # наследуем base
images:
- name: nginx                      # переопределить тег образа
  newTag: "1.26-alpine"
replicas:
- name: web                        # переопределить число реплик
  count: 3
patches:                           # точечный JSON6902-патч
- target: {kind: Deployment, name: web}
  patch: |-
    - op: add
      path: /spec/template/spec/containers/0/resources
      value: {requests: {cpu: 50m, memory: 64Mi}}
```

> ⚠️ Используйте `labels:` (с `pairs`/`includeSelectors`), а не устаревший
> `commonLabels` — последний печатает deprecation warning.

---

## Шаг 3. Рендер и apply

```bash
# 3.1 Отрендерить (НИЧЕГО не применяя) — лучшая привычка перед apply
kubectl kustomize kustomize-demo/overlays/prod
```
Результат: base + все патчи слиты воедино:
```yaml
# ConfigMap   prod-app-config-hf678c7m2b   (имя + хеш содержимого)
# Deployment  prod-web   env: prod   replicas: 3   image: nginx:1.26-alpine
#             + resources.requests {cpu: 50m, memory: 64Mi}
```

```bash
# 3.2 Применить в кластер
kubectl apply -k kustomize-demo/overlays/prod
# configmap/prod-app-config-hf678c7m2b created
# deployment.apps/prod-web created

kubectl get deploy prod-web                 # 3 реплики, образ 1.26-alpine
kubectl get cm | grep prod-app-config       # ConfigMap с хеш-суффиксом
```

> 🔑 `configMapGenerator` добавляет **хеш содержимого** в имя ConfigMap. Изменили
> литерал → новое имя → Deployment видит новый ConfigMap → автоматический
> rolling update. Это решает классическую проблему «обновил ConfigMap, а Pod-ы
> не перезапустились».

---

## Шаг 4. Второй overlay (dev) — та же идея

```yaml
# overlays/dev/kustomization.yaml
namePrefix: dev-
labels:
- pairs: {env: dev}
  includeSelectors: true
resources:
- ../../base
replicas:
- {name: web, count: 1}
# образ оставляем как в base (1.27-alpine) — dev на «стабильном» теге
```

```bash
kubectl apply -k kustomize-demo/overlays/dev
kubectl get deploy dev-web                  # 1 реплика, образ 1.27-alpine
kubectl get cm | grep dev-app-config        # ConfigMap с хеш-суффиксом
```

> Один `base` — N окружений. Меняется только overlay. Никакого копипаста
> манифестов и никаких `{{ if eq .Values.env "prod" }}`.

---

## Шаг 5. Частые трансформеры Kustomize

| Поле | Что делает |
|---|---|
| `namePrefix` / `nameSuffix` | префикс/суффикс к именам |
| `namespace` | задать namespace всем ресурсам |
| `labels` / `annotations` | добавить лейблы/аннотации (и в селекторы) |
| `images` | переопределить `newName` / `newTag` образов |
| `replicas` | переопределить число реплик |
| `configMapGenerator` / `secretGenerator` | сгенерировать CM/Secret (+хеш) |
| `patches` | strategic-merge или JSON6902 патчи |

---

## Cleanup

```bash
kubectl delete -k kustomize-demo/overlays/dev
kubectl delete -k kustomize-demo/overlays/prod
# rm -rf kustomize-demo
```

---

## Что вы узнали

- **Kustomize** встроен в kubectl (`apply -k`, `kubectl kustomize`) — управление
  окружениями **без шаблонов**, чистым YAML.
- **base + overlays**: общий base, overlays его патчат (имя, лейблы, образ,
  реплики, конфиги).
- Трансформеры: `namePrefix`, `images`, `replicas`, `labels`, `patches`,
  `configMapGenerator`.
- `configMapGenerator` добавляет **хеш** в имя → авто-rollout при изменении
  конфига.
- Используйте современный `labels:`, а не устаревший `commonLabels`.
- Привычка: `kubectl kustomize <dir>` (рендер) **перед** `apply -k`.
- Kustomize vs Helm: patch-based для своих манифестов vs шаблоны для упаковки.
