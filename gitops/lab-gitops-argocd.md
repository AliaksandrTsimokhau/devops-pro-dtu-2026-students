# Lab: GitOps с Argo CD

**Цель:** поставить Argo CD, завести `Application` на Git-репозиторий и на живом
кластере увидеть GitOps-цикл: **sync → drift → self-heal → rollback**.

**Время:** 40 минут
**Prerequisites:** [lab-22-rbac.md](../k8s-advanced/lab-22-rbac.md) (RBAC), [lab-20-kustomize.md](../k8s-advanced/lab-20-kustomize.md) (Kustomize — полезно). Эти лабы идут в модуле 6; если вы проходите курс по порядку, сделайте их до этой.

> 📚 Концепция GitOps подробно разобрана в слайд-модуле
> `gitops-argocd-slides.md`. Эта лаба — хендс-он к нему.

---

## BLUF — что проверяем руками

GitOps = **Git — источник истины**, а агент в кластере непрерывно приводит
кластер к Git. В этой лабе:
1. ставим Argo CD (агент);
2. указываем ему Git-репозиторий через `Application`;
3. Argo сам синхронизирует кластер с Git;
4. ломаем кластер руками → Argo **сам чинит** (self-heal).

---

## Шаг 1. Установка Argo CD

```bash
kubectl create namespace argocd
kubectl apply -n argocd --server-side \
  -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml

# дождаться ключевых компонентов (образы крупные — может занять пару минут)
kubectl -n argocd rollout status deploy/argocd-server --timeout=300s
kubectl -n argocd get pods
# argocd-server / argocd-repo-server / argocd-application-controller /
# argocd-redis / argocd-applicationset-controller / dex / notifications
```

> ⚠️ Используйте `--server-side`: CRD `applicationsets.argoproj.io` большой и при
> обычном (client-side) apply падает с
> `metadata.annotations: Too long: may not be more than 262144 bytes`.

---

## Шаг 2. Доступ к UI и CLI

### 2.1 Узнать пароль admin

```bash
# Пароль admin генерируется при установке и лежит в секрете
kubectl -n argocd get secret argocd-initial-admin-secret \
  -o jsonpath='{.data.password}' | base64 -d; echo
```

### 2.2 Открыть UI через port-forward

```bash
# в ОТДЕЛЬНОМ окне (держит туннель, пока запущено)
kubectl -n argocd port-forward svc/argocd-server 8080:443
```

Путь трафика:
```
браузер https://localhost:8080  ──▶  port-forward  ──▶  svc/argocd-server :443  ──▶  Pod
```

- Откройте **https://localhost:8080** (именно `https` — сервис на 443).
- Браузер предупредит о самоподписанном серте → «Advanced → Proceed» (норма для лабы).
- Логин: **admin**, пароль — из шага 2.1.

> 📘 Альтернативы (Ingress / NodePort) и общий разбор доступа к UI —
> в [appendix-accessing-uis.md](../k8s-advanced/appendix-accessing-uis.md).
> В kind проще всего port-forward.

### 2.3 CLI (опционально)

```bash
# brew install argocd   (или скачать релиз с github.com/argoproj/argo-cd)
argocd login localhost:8080 --username admin --password <пароль> --insecure
argocd app list
```

### 2.4 Что смотреть в UI (мини-тур)

После создания приложения (Шаг 3) UI станет «живой картой» деплоя:

```
┌──────────────────────────────────────────────────────────┐
│  Applications                                              │
│  ┌──────────────────────────────────────────────┐         │
│  │  guestbook        ♥ Healthy     ⟳ Synced       │  ← плитка приложения
│  └──────────────────────────────────────────────┘         │
└──────────────────────────────────────────────────────────┘
   клик по плитке ▼
┌──────────────────────────────────────────────────────────┐
│  [SYNC] [REFRESH] [HISTORY AND ROLLBACK]      APP DIFF     │  ← верхняя панель
│                                                            │
│  guestbook ─▶ Deployment ─▶ ReplicaSet ─▶ Pod   ♥          │  ← дерево ресурсов
│            └▶ Service ─────────────────────────  ♥          │
└──────────────────────────────────────────────────────────┘
```

| Элемент UI | Что показывает |
|---|---|
| **Плитка приложения** | общий **Health** (♥ зелёный) и **Sync** (⟳) статус |
| **Дерево ресурсов** | все объекты, созданные из Git, и их связи (Deploy→RS→Pod) |
| **Цвет иконок** | зелёный = Healthy, жёлтый = Progressing/OutOfSync, красный = Degraded |
| **APP DIFF** | построчный diff Git ↔ кластер (виден дрейф) |
| **SYNC / REFRESH** | ручной sync и форс-проверка Git |
| **HISTORY AND ROLLBACK** | список синхронизаций → откат в один клик |

> UI показывает дерево ресурсов, diff Git↔кластер и статусы Sync/Health
> **в реальном времени** — нагляднее, чем `kubectl`. Держите вкладку открытой
> в следующих шагах и наблюдайте, как меняются цвета.

---

## Шаг 3. Application — указываем Argo на Git

`application.yaml`:
```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: guestbook
  namespace: argocd
spec:
  project: default
  source:                                   # ОТКУДА
    repoURL: https://github.com/argoproj/argocd-example-apps.git
    targetRevision: HEAD
    path: guestbook
  destination:                              # КУДА
    server: https://kubernetes.default.svc
    namespace: guestbook
  syncPolicy:
    automated: {prune: true, selfHeal: true}   # авто-sync + self-heal + prune
```

```bash
kubectl create namespace guestbook
kubectl apply -f application.yaml
```

> `automated.selfHeal: true` — Argo откатывает ручной дрейф; `prune: true` —
> удаляет объекты, исчезнувшие из Git. (Подробно — в слайдах, Part 3.)

---

## Шаг 4. Sync — Argo приводит кластер к Git

```bash
# Наблюдаем статус
kubectl -n argocd get app guestbook \
  -o custom-columns='NAME:.metadata.name,SYNC:.status.sync.status,HEALTH:.status.health.status'
# guestbook   Synced   Progressing   ← применено, Pod-ы поднимаются
# ... спустя время:
# guestbook   Synced   Healthy

# Ресурсы, которые Argo создал ИЗ Git (мы их вручную не делали):
kubectl -n guestbook get deploy,svc
# deployment.apps/guestbook-ui   1/1 ...
# service/guestbook-ui           ClusterIP ...
```

> Два независимых статуса: **Sync** (совпадает ли с Git) и **Health** (работает
> ли в кластере). `Synced/Progressing` = манифесты применены, но Pod-ы ещё
> стартуют (тянут образ).

> 👀 **В UI:** плитка `guestbook` сначала жёлтая (Progressing), затем зеленеет
> (Healthy). Кликните в неё → увидите, как в дереве ресурсов появляются
> `Deployment → ReplicaSet → Pod` и `Service`. Это те же объекты, что и в
> `kubectl get`, только вживую.

✅ **Checkpoint:** `kubectl -n guestbook get deploy guestbook-ui` показывает
`1/1`, а статус приложения — `Synced / Healthy`. Дальше — самое интересное.

---

## Шаг 5. Drift + Self-heal (главная демонстрация)

> 👀 **Держите вкладку UI открытой** на плитке `guestbook` и смотрите, как Argo
> реагирует на «поломку» в реальном времени.

```bash
# 5.1 DRIFT: ломаем кластер руками
kubectl -n guestbook delete deploy guestbook-ui --now

# 5.2 SELF-HEAL: Argo замечает расхождение и ВОЗВРАЩАЕТ ресурс из Git
#     (в течение секунд, без нашего участия)
until kubectl -n guestbook get deploy guestbook-ui >/dev/null 2>&1; do sleep 3; done
kubectl -n guestbook get deploy guestbook-ui
# guestbook-ui   1/1   ← воссоздан автоматически

kubectl -n argocd get app guestbook -o jsonpath='sync={.status.sync.status} health={.status.health.status}{"\n"}'
# sync=Synced health=Healthy
```

Попробуйте и дрейф через изменение:
```bash
kubectl -n guestbook scale deploy guestbook-ui --replicas=5
# с selfHeal Argo вернёт к 1 (как в Git) за пару циклов reconcile
```

> ✅ Это суть GitOps: кластер всегда подтягивается к Git. Чтобы изменение
> «прижилось» — меняйте **Git**, а не кластер.

> 👀 **В UI это видно нагляднее всего:** после `delete` ресурс на секунду
> становится серым/исчезает, плитка краснеет (OutOfSync/Missing), затем Argo
> сам пересоздаёт объект и всё снова зеленеет. Тот же эффект при `scale` —
> в `APP DIFF` видно `replicas: 5` (live) против `1` (Git), после чего Argo
> откатывает.

🔧 **Troubleshooting:**
- UI отдаёт пустую страницу / connection refused → форвардите на `:443`, а не
  `:80`, и открывайте `https://` (не `http://`).
- self-heal не срабатывает → проверьте, что в `Application` есть
  `syncPolicy.automated.selfHeal: true` (`kubectl -n argocd get app guestbook -o yaml | grep -A3 syncPolicy`).
- забыли пароль admin → секрет `argocd-initial-admin-secret` (Шаг 2.1); если
  удалён — сбросьте через `argocd admin initial-password` или пересоздайте.

---

## Шаг 6. Изменение и откат через Git

Правильный GitOps-workflow (если бы это был ваш config-repo):
```
1. правите манифест в Git (напр. image tag) → commit/PR → merge
2. Argo видит новый commit (poll ~3 мин или webhook) → синхронизирует
3. откат:  git revert <commit> && git push  → Argo катит обратно
```

Аварийная кнопка отката через Argo (история синхронизаций):
```bash
argocd app history guestbook
argocd app rollback guestbook <REVISION_ID>
```

> «Канонический» откат — `git revert`, чтобы Git оставался источником истины.
> `argocd app rollback` — быстрый аварийный вариант.

---

## Cleanup

```bash
# удаление Application уберёт и его ресурсы (prune)
kubectl -n argocd delete app guestbook
kubectl delete namespace guestbook
# снести сам Argo CD:
kubectl delete -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml --ignore-not-found
kubectl delete namespace argocd
```

---

## Что вы узнали

- **Argo CD** = pull-агент GitOps в кластере: тянет манифесты из Git и применяет.
- **`Application`** = source (откуда) + destination (куда) + syncPolicy (как).
- Статусы **Sync** (== Git?) и **Health** (работает?) — независимы.
- `automated: {selfHeal, prune}` — авто-sync, откат дрейфа, удаление лишнего.
- **Self-heal**: удалённый/изменённый вручную ресурс возвращается к состоянию Git.
- Откат = **`git revert`** (или `argocd app rollback` как аварийный).
- Установка Argo CD требует `kubectl apply --server-side` (большой CRD).
- Глубже — слайд-модуль `gitops-argocd-slides.md` (архитектура, app-of-apps,
  ApplicationSets, безопасность, Argo vs Flux).
