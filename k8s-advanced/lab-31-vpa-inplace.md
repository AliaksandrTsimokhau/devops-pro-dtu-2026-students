# Lab 31 — VPA: вертикальное масштабирование **без пересоздания Pod-а** (Extra / Bonus)

**Цель:** установить Vertical Pod Autoscaler и применить рекомендации по
ресурсам к работающим Pod-ам **in-place** (`updateMode: InPlaceOrRecreate`) —
без eviction и пересоздания, через `resize`-сабресурс Kubernetes.

**Время:** 30–40 минут
**Prerequisites:** Lab 30 (нужен **metrics-server**), кластер из Lab 01.

> 🧩 Это **бонусная** лаба — расширение к Lab 30 (HPA). HPA меняет **число**
> Pod-ов (horizontal), VPA меняет **ресурсы** Pod-а (vertical). Раньше VPA умел
> применять рекомендации только через пересоздание Pod-а (`Recreate`) — это
> рестарт и downtime. Здесь мы используем **новый** механизм: in-place resize.

---

## BLUF — что нового

- **Раньше:** VPA `updateMode: Recreate` → updater **выселял** Pod, Deployment
  создавал новый с новыми requests. Рестарт = разрыв соединений.
- **Сейчас (K8s 1.33+ beta «InPlacePodVerticalScaling» + VPA 1.4+):**
  `updateMode: InPlaceOrRecreate` → updater патчит ресурсы **работающего**
  контейнера через `pods/resize`. Тот же Pod, тот же UID, без рестарта
  (если `resizePolicy: NotRequired`). На eviction откатывается только если
  in-place невозможен.

---

## Шаг 0. Проверьте, что кластер умеет in-place resize

`InPlacePodVerticalScaling` — **beta с Kubernetes 1.33** (включено по умолчанию).
Проверим на «голом» Pod-е, до всякого VPA:

```bash
kubectl version | grep -i server      # нужно v1.33+ (тут v1.35)

# Pod с requests/limits
kubectl run resize-test --image=nginx:1.27-alpine --restart=Never \
  --overrides='{"spec":{"containers":[{"name":"resize-test","image":"nginx:1.27-alpine","resources":{"requests":{"cpu":"100m","memory":"64Mi"},"limits":{"cpu":"200m","memory":"128Mi"}}}]}}'
kubectl wait --for=condition=ready pod/resize-test --timeout=60s

UID_BEFORE=$(kubectl get pod resize-test -o jsonpath='{.metadata.uid}')

# in-place resize через resize-сабресурс
kubectl patch pod resize-test --subresource resize --patch \
  '{"spec":{"containers":[{"name":"resize-test","resources":{"requests":{"cpu":"250m","memory":"128Mi"},"limits":{"cpu":"500m","memory":"256Mi"}}}]}}'

echo "UID before: $UID_BEFORE"
echo "UID after:  $(kubectl get pod resize-test -o jsonpath='{.metadata.uid}')"
kubectl get pod resize-test -o jsonpath='requests={.spec.containers[0].resources.requests} restarts={.status.containerStatuses[0].restartCount}{"\n"}'
```

Ожидаем: **UID не изменился**, `requests` стали `250m/128Mi`, `restarts=0`.
Это и есть «не пересоздавая Pod».

```bash
kubectl delete pod resize-test --now
```

> Если `kubectl patch --subresource resize` ругается `unknown flag` — у вас
> старый kubectl/кластер (<1.32). Обновите kubectl и кластер до 1.33+.

---

## Шаг 1. Установка VPA (с feature-gate `InPlace`)

VPA не входит в кластер по умолчанию — ставим из репозитория autoscaler.
`updateMode: InPlaceOrRecreate` в VPA 1.7 спрятан за **alpha feature-gate
`InPlace`** — его нужно явно включить через `FEATURE_GATES`.

```bash
git clone --depth 1 --branch vertical-pod-autoscaler-1.7.0 \
  https://github.com/kubernetes/autoscaler.git
cd autoscaler/vertical-pod-autoscaler

# FEATURE_GATES прокинется в args всех компонентов VPA
FEATURE_GATES="InPlace=true" ./hack/vpa-up.sh
```

Скрипт ставит **три** компонента в `kube-system` + CRD + генерирует TLS-серты
для admission-webhook:

| Компонент | Роль |
|---|---|
| **recommender** | смотрит метрики (metrics-server), считает рекомендацию по CPU/mem |
| **updater** | применяет рекомендацию к работающим Pod-ам (in-place или eviction) |
| **admission-controller** | мутатор: проставляет ресурсы **новым** Pod-ам при создании |

```bash
kubectl -n kube-system get pods | grep vpa
# vpa-admission-controller-...   1/1   Running
# vpa-recommender-...            1/1   Running
# vpa-updater-...                1/1   Running
```

> ⚠️ Нужны **все три** компонента. `updater` на каждой итерации проверяет lease
> `vpa-admission-controller` и **молча пропускает весь цикл**, если
> admission-controller не поднят:
> `"Error getting Admission Controller status. Skipping update loop"`.
> Если ресайза «не происходит» — первым делом проверьте, что
> admission-controller `Running`, а его lease существует:
> `kubectl -n kube-system get lease vpa-admission-controller`.

---

## Шаг 2. Workload с `resizePolicy: NotRequired`

Ключ к «без рестарта» — `resizePolicy` в спеке контейнера. По умолчанию resize
памяти требует рестарта контейнера; `NotRequired` говорит kubelet применять
изменения **на лету**.

`vpa-demo.yaml`:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: vpa-demo
spec:
  replicas: 2
  selector:
    matchLabels: {app: vpa-demo}
  template:
    metadata:
      labels: {app: vpa-demo}
    spec:
      containers:
      - name: app
        image: registry.k8s.io/hpa-example     # CPU-нагружаемый
        resizePolicy:                            # ← без рестарта при resize
        - resourceName: cpu
          restartPolicy: NotRequired
        - resourceName: memory
          restartPolicy: NotRequired
        resources:
          requests: {cpu: 50m,  memory: 64Mi}   # намеренно занижено
          limits:   {cpu: 500m, memory: 256Mi}
        ports:
        - containerPort: 80
---
apiVersion: v1
kind: Service
metadata: {name: vpa-demo}
spec:
  selector: {app: vpa-demo}
  ports: [{port: 80, targetPort: 80}]
```

```bash
kubectl apply -f vpa-demo.yaml
kubectl rollout status deploy/vpa-demo --timeout=90s

# Запомним стартовые requests + UID-ы
kubectl get pods -l app=vpa-demo -o custom-columns=\
'NAME:.metadata.name,UID:.metadata.uid,CPU:.spec.containers[0].resources.requests.cpu,MEM:.spec.containers[0].resources.requests.memory,RESTARTS:.status.containerStatuses[0].restartCount'
# CPU=50m  MEM=64Mi  RESTARTS=0
```

---

## Шаг 3. VPA с `updateMode: InPlaceOrRecreate`

`vpa.yaml`:
```yaml
apiVersion: autoscaling.k8s.io/v1
kind: VerticalPodAutoscaler
metadata:
  name: vpa-demo
spec:
  targetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: vpa-demo
  updatePolicy:
    updateMode: "InPlaceOrRecreate"      # ← новый режим (нужен gate InPlace)
  resourcePolicy:
    containerPolicies:
    - containerName: app
      minAllowed: {cpu: 50m,  memory: 64Mi}
      maxAllowed: {cpu: "1",  memory: 512Mi}
      controlledResources: ["cpu", "memory"]
```

```bash
kubectl apply -f vpa.yaml
kubectl get vpa vpa-demo
# через 1-2 минуты в .status появится рекомендация
```

> `updateMode`-режимы: `Off` (только считать), `Initial` (только при создании),
> `Recreate` (старый способ — через eviction), **`InPlaceOrRecreate`**
> (in-place, с откатом на recreate), `Auto`.

---

## Шаг 4. Нагрузка → рекомендация

```bash
# В отдельном окне — генератор нагрузки на CPU
kubectl run vpa-load --image=busybox:1.37 --restart=Never -- /bin/sh -c \
  "while true; do wget -q -O- http://vpa-demo.default.svc.cluster.local; done"
```

Следим за рекомендацией (recommender-у нужно ~1-3 мин истории метрик):
```bash
kubectl get vpa vpa-demo \
  -o jsonpath='{.status.recommendation.containerRecommendations[0].target}{"\n"}'
# {"cpu":"224m","memory":"250Mi"}   ← намного больше стартовых 50m/64Mi
```

> `<unknown>`/пусто первые минуты — норма (нет истории метрик). Проверьте, что
> `kubectl top pods -l app=vpa-demo` отдаёт данные (Lab 30, metrics-server).

---

## Шаг 5. Updater применяет рекомендацию **in-place**

Updater запускается раз в минуту. Наблюдаем:
```bash
watch kubectl get pods -l app=vpa-demo -o custom-columns=\
'NAME:.metadata.name,UID:.metadata.uid,CPU:.spec.containers[0].resources.requests.cpu,MEM:.spec.containers[0].resources.requests.memory,RESTARTS:.status.containerStatuses[0].restartCount'
```

Через 1-2 цикла:
```
NAME                        UID           CPU    MEM     RESTARTS
vpa-demo-79654b5749-p5l5c   374d2af0-...  296m   250Mi   0      ← resized
vpa-demo-79654b5749-qjkvs   e7e76e5a-...  50m    64Mi    0      ← следующий цикл
```

**UID тот же, RESTARTS=0** → ресурсы подняты на работающем контейнере. Сравните
с UID-ами из Шага 2 — они не поменялись.

Доказательства:
```bash
# Событие
kubectl get events --field-selector reason=InPlaceResizedByVPA
# ... reason=InPlaceResizedByVPA  "Pod was resized in place by VPA Updater."

# Аннотация, которую ставит updater
kubectl get pod -l app=vpa-demo \
  -o jsonpath='{range .items[*]}{.metadata.name}: {.metadata.annotations.vpaInPlaceUpdated}{"\n"}{end}'
# vpa-demo-...-p5l5c: true

# Лог updater-а
kubectl -n kube-system logs deploy/vpa-updater | grep -i 'In-place patched pod'
# "In-place patched pod /resize subresource using patches" pod="default/vpa-demo-..."
```

> 🧠 Updater обновляет Pod-ы **по одному за цикл**, остальные откладывает
> (`"In-place update deferred"`) — это защита доступности (аналог
> disruption budget). Eviction **не** происходит, пока in-place возможен.

---

## Шаг 6. Когда происходит fallback на Recreate

`InPlaceOrRecreate` ⇒ updater пытается in-place, но **откатывается на
пересоздание**, если resize на лету невозможен, например:

- контейнер без `resizePolicy: NotRequired` для memory → kubelet требует рестарт;
- новый размер не влезает на ноду (нужен reschedule на другую ноду);
- runtime/нода не поддерживают in-place resize памяти.

Это «безопасный» режим: либо мягко (in-place), либо как раньше (recreate), но
рекомендация будет применена в любом случае. Чистый `InPlace` (без fallback)
тоже есть, но он оставит Pod недонастроенным, если resize не прошёл.

---

## Шаг 7. VPA в production — что реально важно

**VPA работает в prod сам по себе — никакого доп. тулинга не требуется.** Главные
ограничения — про сам VPA, а не про обвязку:

- **Нужен metrics-server** (или Prometheus adapter) — без источника метрик
  recommender ничего не посчитает.
- **HPA и VPA нельзя вешать на одну метрику.** Оба по CPU = они конфликтуют
  (VPA меняет requests → меняется % утилизации → HPA дёргает реплики). Рабочая
  комбинация: VPA по CPU/mem **+** HPA по custom/external-метрике (RPS, длина
  очереди).
- **Поведение при изменении.** `Recreate`/`Auto` выселяют Pod (рестарт, разрыв
  соединений) — исторически главный стоппер для prod. `InPlaceOrRecreate` (эта
  лаба) снимает проблему для stateful/долгоживущих воркложов.
- **Приложения, читающие cgroup-лимиты только на старте** (некоторые JVM-сетапы)
  могут не подхватить in-place изменение без рестарта — проверяйте по воркложу.
- **Нужно время и история.** Recommender использует затухающую гистограмму +
  checkpoints; первые минуты доверять числам нельзя.

> **А Goldilocks?** [Goldilocks](https://goldilocks.docs.fairwinds.com/) (Fairwinds) —
> **опциональный** дашборд: для размеченных namespace он сам создаёт VPA в
> `updateMode: Off` и показывает рекомендованные requests/limits. Он **только
> советует** — ничего не применяет и не добавляет VPA новых возможностей. Полезен
> на этапе **right-sizing** (обзор по всему кластеру без ручного VPA на каждый
> Deployment). Если VPA уже работает в активном режиме (`InPlaceOrRecreate` и т.п.)
> — Goldilocks не нужен; те же данные читаются через
> `kubectl get vpa -o jsonpath='{.status.recommendation}'`.

---

## 👀 Наблюдение и диагностика

**Смотреть in-place resize вживую** (во время Шага 5):
```bash
watch -n5 'kubectl get pods -l app=vpa-demo -o custom-columns=\
NAME:.metadata.name,UID:.metadata.uid,CPU:.spec.containers[0].resources.requests.cpu,RESTARTS:.status.containerStatuses[0].restartCount'
# requests меняются (50m → ~296m), а UID и RESTARTS НЕ меняются = ресайз без пересоздания
```
Рекомендацию VPA удобно смотреть так:
```bash
watch -n5 'kubectl get vpa vpa-demo -o jsonpath="{.status.recommendation.containerRecommendations[0].target}{\"\n\"}"'
```

✅ **Checkpoint:** в `.status` VPA появилась рекомендация; updater поднял
`requests` у работающего Pod-а — **тот же UID, `RESTARTS=0`**, событие
`InPlaceResizedByVPA`.

🔧 **Troubleshooting:**
- ресайз не происходит → updater пропускает цикл без admission-controller
  (Шаг 1): `kubectl -n kube-system get lease vpa-admission-controller`.
- рекомендация пустая → нет metrics-server, либо ещё мало истории — подождите
  пару минут (`kubectl top pods -l app=vpa-demo`).
- Pod **рестартует** при ресайзе → в контейнере нет `resizePolicy: NotRequired`
  (Шаг 2).

### (Опционально) Дашборд Goldilocks — рекомендации «как на ладони»

Goldilocks показывает рекомендации VPA таблицей в браузере (вместо `jsonpath`):
```bash
# ТРЕБУЕТ установленного VPA из Шага 1 (Goldilocks использует его CRD/recommender,
# свой VPA он НЕ ставит). metrics-server тоже должен работать (Lab 30).
helm repo add fairwinds-stable https://charts.fairwinds.com/stable && helm repo update
helm install goldilocks fairwinds-stable/goldilocks --namespace goldilocks --create-namespace
kubectl label ns default goldilocks.fairwinds.com/enabled=true   # анализировать namespace default
kubectl -n goldilocks port-forward svc/goldilocks-dashboard 8080:80
# → http://localhost:8080  (HTTP, порт сервиса 80; см. appendix-accessing-uis.md)
```
Goldilocks сам заведёт VPA-объекты (режим `Off`) для Deployment-ов в помеченном
namespace и покажет для каждого рекомендованные requests/limits.
> Goldilocks — **только рекомендации** (VPA в режиме `Off`), ничего не применяет.
> Удобен на этапе right-sizing. Подробно — в Шаге 7 (VPA в production).

См. также [appendix-accessing-uis.md](appendix-accessing-uis.md) (port-forward, k9s, Dashboard).

---

## Cleanup

```bash
kubectl delete pod vpa-load --now --ignore-not-found
kubectl delete -f vpa.yaml --ignore-not-found
kubectl delete -f vpa-demo.yaml --ignore-not-found

# Удалить VPA целиком (из папки репозитория):
cd autoscaler/vertical-pod-autoscaler
./hack/vpa-down.sh
# либо вручную: kubectl delete -n kube-system deploy vpa-recommender vpa-updater vpa-admission-controller
#               kubectl delete mutatingwebhookconfiguration vpa-webhook-config
#               kubectl delete -f deploy/vpa-rbac.yaml ; kubectl delete -f deploy/vpa-v1-crd-gen.yaml
```

> Webhook VPA имеет `failurePolicy: Ignore` — даже если admission-controller
> упадёт, создание обычных Pod-ов в кластере не сломается. Но для чистоты
> учебного кластера VPA лучше снести после лабы.

---

## Что вы узнали

- **VPA** масштабирует Pod **вертикально** (CPU/mem requests), HPA —
  **горизонтально** (число реплик). Это разные оси.
- `InPlacePodVerticalScaling` (K8s 1.33+ beta) + `pods/resize`-сабресурс →
  ресурсы меняются **на работающем** контейнере, без пересоздания.
- VPA `updateMode: InPlaceOrRecreate` (VPA 1.4+, gate `InPlace`) применяет
  рекомендации in-place, откатываясь на eviction только при необходимости.
- `resizePolicy: NotRequired` — условие resize **без рестарта** контейнера.
- VPA = 3 компонента (recommender / updater / admission-controller); updater
  не работает без admission-controller.
- Updater обновляет Pod-ы постепенно (по одному), защищая доступность.
- ⚠️ HPA и VPA на **один и тот же ресурс** (например оба по CPU) конфликтуют —
  не вешайте их на одну метрику. VPA по CPU/mem + HPA по custom-метрике — ок.
