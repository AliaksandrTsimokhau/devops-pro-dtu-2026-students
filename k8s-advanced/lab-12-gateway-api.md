# Lab 12 — Gateway API: преемник Ingress

**Цель:** развернуть Gateway API (GA) — современную замену Ingress: установить
CRD + контроллер (NGINX Gateway Fabric), создать `GatewayClass` → `Gateway` →
`HTTPRoute` и настроить path-роутинг.

**Время:** 35 минут
**Prerequisites:** Lab 11 (Ingress), Lab 10 (Services).

> «Новый» способ K8s (как in-place resize в Lab 31, нативные sidecar-ы в Lab 05).
> Gateway API — отдельные CRD + контроллер; ставится поверх кластера.

---

## BLUF — зачем Gateway API вместо Ingress

Ingress (Lab 11) застрял: один объект, расширения только через
вендор-специфичные **аннотации** (`nginx.ingress.kubernetes.io/...`), нет
разделения ролей. Gateway API это переосмысливает:

| | Ingress | Gateway API |
|---|---|---|
| Объектов | один (`Ingress`) | `GatewayClass` / `Gateway` / `*Route` |
| Расширяемость | аннотации (вендорные) | типизированные поля в спеке |
| Роли | всё в одном | **инфра-команда** владеет Gateway, **разработчик** — Route |
| Протоколы | HTTP(S) | HTTP, gRPC, TCP, TLS, UDP |
| Статус | GA, но «заморожен» | GA, активно развивается |

**Модель ролей** — ключевая идея:
```
GatewayClass  ← задаёт контроллер (как StorageClass для томов); кластер-админ
   Gateway    ← слушатели/порты/TLS; команда платформы/инфры
   HTTPRoute  ← правила роутинга на сервисы; команда разработки
```

---

## Шаг 1. Установка CRD Gateway API

CRD Gateway API **не входят** в кластер — ставим отдельно (standard channel):
```bash
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.5.1/standard-install.yaml

kubectl get crd | grep gateway.networking.k8s.io
# gatewayclasses / gateways / httproutes / grpcroutes / referencegrants ...
```

> CRD — это только **API**. Чтобы он что-то делал, нужен **контроллер**
> (реализация). Их много: NGINX Gateway Fabric, Envoy Gateway, Istio, Cilium,
> Kong. Ставим один.

---

## Шаг 2. Установка контроллера — NGINX Gateway Fabric

```bash
# CRD контроллера — через server-side apply (один CRD большой, иначе ошибка
# "metadata.annotations: Too long" при client-side apply)
kubectl apply --server-side -f https://raw.githubusercontent.com/nginx/nginx-gateway-fabric/v2.6.5/deploy/crds.yaml

# сам контроллер
kubectl apply -f https://raw.githubusercontent.com/nginx/nginx-gateway-fabric/v2.6.5/deploy/default/deploy.yaml

kubectl -n nginx-gateway rollout status deploy/nginx-gateway --timeout=120s

# контроллер регистрирует GatewayClass "nginx"
kubectl get gatewayclass
# NAME    CONTROLLER                                   ACCEPTED
# nginx   gateway.nginx.org/nginx-gateway-controller   True
```

> ⚠️ Если `deploy.yaml` падает с `no matches for kind "NginxProxy"` или
> `annotations: Too long` — это из-за client-side apply большого CRD. Поставьте
> CRD через `--server-side` (как выше) и повторите `deploy.yaml`.

---

## Шаг 3. Backend-приложения

`apps.yaml` — два echo-сервиса:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata: {name: app-a}
spec:
  replicas: 1
  selector: {matchLabels: {app: app-a}}
  template:
    metadata: {labels: {app: app-a}}
    spec:
      containers:
      - name: c
        image: hashicorp/http-echo
        args: ["-text=hello from APP-A", "-listen=:5678"]
---
apiVersion: v1
kind: Service
metadata: {name: app-a}
spec:
  selector: {app: app-a}
  ports: [{port: 80, targetPort: 5678}]
---
# app-b — то же самое с text=APP-B (см. полный манифест в репозитории)
```

```bash
kubectl apply -f apps.yaml
kubectl rollout status deploy/app-a deploy/app-b --timeout=90s
```

---

## Шаг 4. Gateway (слушатель) + HTTPRoute (правила)

`gateway.yaml`:
```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata: {name: demo-gw}
spec:
  gatewayClassName: nginx              # ← какой контроллер обслуживает
  listeners:
  - name: http
    protocol: HTTP
    port: 80
    hostname: "demo.example.com"
    allowedRoutes: {namespaces: {from: Same}}
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata: {name: demo-route}
spec:
  parentRefs: [{name: demo-gw}]        # к какому Gateway цепляемся
  hostnames: ["demo.example.com"]
  rules:
  - matches: [{path: {type: PathPrefix, value: /a}}]
    backendRefs: [{name: app-a, port: 80}]
  - matches: [{path: {type: PathPrefix, value: /b}}]
    backendRefs: [{name: app-b, port: 80}]
```

```bash
kubectl apply -f gateway.yaml

# Gateway запрограммирован контроллером?
kubectl get gateway demo-gw -o jsonpath='{range .status.conditions[*]}{.type}={.status} {end}{"\n"}'
# Accepted=True Programmed=True

# HTTPRoute принят и бэкенды зарезолвлены?
kubectl get httproute demo-route -o jsonpath='{range .status.parents[0].conditions[*]}{.type}={.status} {end}{"\n"}'
# Accepted=True ResolvedRefs=True
```

> `Programmed=True` означает: контроллер развернул дата-плейн (nginx) под этот
> Gateway. NGF создаёт отдельный Deployment+Service nginx **в namespace
> Gateway-я** — `demo-gw-nginx`.

---

## Шаг 5. Проверка роутинга

```bash
# Дата-плейн сервис этого Gateway
kubectl get svc demo-gw-nginx
# TYPE LoadBalancer ... PORT 80:NNNNN/TCP   (в kind EXTERNAL-IP может быть pending)

# Проще всего проверить через port-forward
kubectl port-forward svc/demo-gw-nginx 8080:80 &

curl -s -H "Host: demo.example.com" http://localhost:8080/a
# hello from APP-A
curl -s -H "Host: demo.example.com" http://localhost:8080/b
# hello from APP-B
curl -s -o /dev/null -w "HTTP %{http_code}\n" -H "Host: demo.example.com" http://localhost:8080/
# HTTP 404      ← нет правила для "/", роутинг строгий

kill %1   # остановить port-forward
```

✅ Один Gateway (порт 80, хост `demo.example.com`) → HTTPRoute раздаёт `/a` и
`/b` на разные сервисы. Host-header обязателен — слушатель привязан к hostname.

---

## Шаг 6. Что Gateway API умеет сверх Ingress (без аннотаций)

Всё это — **типизированные поля**, а не вендорные аннотации:
```yaml
rules:
- matches:
  - headers: [{name: x-version, value: canary}]   # роутинг по заголовку
  backendRefs:
  - {name: app-a, port: 80, weight: 90}            # weighted / canary
  - {name: app-b, port: 80, weight: 10}
  filters:
  - type: RequestHeaderModifier                     # модификация заголовков
    requestHeaderModifier: {add: [{name: x-env, value: prod}]}
```

| Возможность | Ingress | Gateway API |
|---|---|---|
| Роутинг по header/method/query | аннотации (если есть) | нативно (`matches`) |
| Weighted / canary | вендорно | `backendRefs[].weight` |
| Header rewrite/redirect | аннотации | `filters` |
| gRPC / TCP / TLS | нет | `GRPCRoute`/`TCPRoute`/`TLSRoute` |
| Cross-namespace routes | нет | `ReferenceGrant` |

---

## Cleanup

```bash
kubectl delete -f gateway.yaml --ignore-not-found
kubectl delete -f apps.yaml --ignore-not-found
# контроллер и CRD (если больше не нужны):
kubectl delete -f https://raw.githubusercontent.com/nginx/nginx-gateway-fabric/v2.6.5/deploy/default/deploy.yaml --ignore-not-found
kubectl delete -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.5.1/standard-install.yaml --ignore-not-found
```

---

## Что вы узнали

- **Gateway API** — GA-преемник Ingress: типизированные поля вместо вендорных
  аннотаций, роль-ориентированная модель.
- Триада: **GatewayClass** (контроллер, админ) → **Gateway** (слушатели, инфра) →
  **HTTPRoute** (правила, разработчик).
- CRD ≠ контроллер: ставим CRD Gateway API **и** реализацию (NGF/Envoy/Istio…).
- Статусы: `Gateway Programmed=True`, `HTTPRoute Accepted/ResolvedRefs=True`.
- Из коробки: роутинг по path/header/method, weighted/canary, header-фильтры,
  gRPC/TCP/TLS-маршруты, cross-namespace через `ReferenceGrant`.
- Большой CRD контроллера ставится через `kubectl apply --server-side`.
