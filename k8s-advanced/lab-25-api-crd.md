# Lab 25 — Explore K8s API + define a CRD

**Цель:** прямой доступ к Kubernetes REST API через kubectl proxy и создание собственного CRD.

**Время:** 25 минут
**Prerequisites:** Lab 02.

---

## Часть A — explore API через kubectl proxy

### Шаг 1. Запустить proxy

```bash
kubectl proxy --port=8001 &
# Starting to serve on 127.0.0.1:8001

# ВАЖНО: убедитесь, что отвечает именно proxy, прежде чем слать curl.
# healthz должен вернуть РОВНО "ok":
curl -s http://localhost:8001/healthz
# ok

# Если позже захотите остановить:
# ps | grep "kubectl proxy"
# kill <pid>
```

> **Почему без токена?** `kubectl proxy` слушает на `localhost:8001` и сам
> подставляет ваши credentials из kubeconfig к каждому запросу. Поэтому curl
> ходит на proxy **без** `Authorization: Bearer ...`, а proxy уже
> аутентифицируется в API server за вас.
>
> **Проверка `healthz` обязательна.** `curl -s` возвращает «успех» (exit 0),
> даже когда отвечает НЕ тот сервер, поэтому единственный надёжный признак —
> тело ответа должно быть ровно `ok`. Если пришёл HTML (`<!doctype html>...`)
> или чужой JSON — значит curl ходит не туда. Две частые причины ниже.

⚠️ **Не запускайте эту лабу из встроенного терминала Freelens / Lens.**
Freelens подменяет `$KUBECONFIG` на свой внутренний прокси с путём в URL
(`server: http://127.0.0.1:<port>/383453...`). При старте `kubectl proxy`
выведет предупреждение:

```
W ... Your kube context contains a server path /383453...,
use --append-server-path to automatically append the path to each request
```

`kubectl proxy` этот путь **не** пробрасывает → `/api` уходит в корень сервера
→ в ответ прилетает веб-интерфейс Freelens (тот самый HTML). Решения:

```bash
# Вариант 1 (рекомендуется): обычный терминал + чистый контекст кластера
kubectl config use-context kind-<your-cluster>      # ваш контекст лабы
kubectl config view --minify -o jsonpath='{..server}'; echo
#   server должен быть вида https://127.0.0.1:49515  (БЕЗ пути в конце)
kubectl proxy --port=8001 &

# Вариант 2: остаться в Freelens-контексте, но попросить proxy дописать путь
kubectl proxy --port=8001 --append-server-path &
```

> **Если порт занят** (`address already in use`, или в ответ HTML от другого
> `kubectl proxy` / дашборда): возьмите свободный порт и используйте его во
> всех командах ниже:
> ```bash
> kubectl proxy --port=8081 &              # любой свободный порт
> curl -s http://localhost:8081/healthz    # снова ждём ровно "ok"
> ```
> Кто держит порт: `lsof -nP -iTCP:8001 -sTCP:LISTEN`

### Шаг 2. List API groups

```bash
# Core group
curl http://localhost:8001/api
# {"kind":"APIVersions","versions":["v1"],
#  "serverAddressByClientCIDRs":[{"clientCIDR":"0.0.0.0/0","serverAddress":"..."}]}

# Named groups
curl http://localhost:8001/apis | python3 -m json.tool | head -30
```

### Шаг 3. List resources

```bash
# Namespaces
curl http://localhost:8001/api/v1/namespaces | python3 -m json.tool | head

# Pods in default namespace
curl http://localhost:8001/api/v1/namespaces/default/pods | python3 -m json.tool | head

# Deployments
curl http://localhost:8001/apis/apps/v1/namespaces/default/deployments
```

### Шаг 4. Create через POST

`ns.json`:
```json
{
  "kind": "Namespace",
  "apiVersion": "v1",
  "metadata": {
    "name": "api-test"
  }
}
```

```bash
curl -X POST \
  -H "Content-Type: application/json" \
  --data-binary @ns.json \
  http://localhost:8001/api/v1/namespaces

# Verify
kubectl get ns api-test
```

### Шаг 5. Delete через DELETE

```bash
curl -X DELETE http://localhost:8001/api/v1/namespaces/api-test
```

---

## Часть B — Custom Resource Definition (CRD)

### Шаг 6. Создаём CRD

`crd-book.yaml`:
```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: books.dtu.local
spec:
  group: dtu.local
  scope: Namespaced
  names:
    plural: books
    singular: book
    kind: Book
    shortNames: [bk]
  versions:
  - name: v1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        type: object
        properties:
          spec:
            type: object
            properties:
              title: {type: string}
              author: {type: string}
              edition: {type: integer}
              topic: {type: string}
            required: [title, author]
    additionalPrinterColumns:
    - name: Title
      type: string
      jsonPath: .spec.title
    - name: Author
      type: string
      jsonPath: .spec.author
```

```bash
kubectl apply -f crd-book.yaml
kubectl get crd books.dtu.local
kubectl api-resources | grep books
# books   bk   dtu.local/v1   true   Book
```

### Шаг 7. Создаём Custom Object

`book1.yaml`:
```yaml
apiVersion: dtu.local/v1
kind: Book
metadata:
  name: k8s-book
spec:
  title: "The Kubernetes Book"
  author: "Nigel Poulton"
  edition: 2024
  topic: "Kubernetes"
```

```bash
kubectl apply -f book1.yaml

# Используем shortname
kubectl get bk
# NAME       TITLE                 AUTHOR
# k8s-book   The Kubernetes Book   Nigel Poulton

# Полные данные
kubectl get book k8s-book -o yaml
```

### Шаг 8. Query через proxy

```bash
curl http://localhost:8001/apis/dtu.local/v1/namespaces/default/books | python3 -m json.tool
```

### Шаг 9. Validation работает

```yaml
# Bad book (нет required field 'author'):
cat <<EOF | kubectl apply -f -
apiVersion: dtu.local/v1
kind: Book
metadata:
  name: incomplete
spec:
  title: "Only Title"
EOF
# Error from server: spec.author is required
```

---

## Шаг 10. Понятие Operator

CRD сам по себе ничего не делает. Чтобы Book "что-то делал" — нужен Controller (Operator), который смотрит на эти объекты.

Примеры real-world Operators:
- **cert-manager** — Certificate, Issuer CRDs → выпускает сертификаты
- **Prometheus Operator** — ServiceMonitor, PrometheusRule CRDs
- **CloudNativePG** — Cluster CRD → создаёт Postgres cluster
- **ArgoCD** — Application CRD → синкает Git в кластер

Operator framework: **Kubebuilder** (Go) или **Operator SDK** (Go / Ansible / Helm).

---

## Cleanup

```bash
# Остановить proxy
kill $(ps | grep "kubectl proxy" | grep -v grep | awk '{print $1}')

kubectl delete -f book1.yaml
kubectl delete -f crd-book.yaml
```

---

## Что вы узнали

- Kubernetes API — RESTful, доступен через `kubectl proxy`
- Все ресурсы лежат под `/api/v1/...` (core) или `/apis/<group>/<version>/...`
- CRUD через стандартные HTTP методы: POST / GET / PUT / PATCH / DELETE
- CRD = добавляем свой тип ресурса в API
- CRD + Controller = Operator pattern
- OpenAPI schema в CRD валидирует данные
- `additionalPrinterColumns` — кастомные колонки в `kubectl get`
