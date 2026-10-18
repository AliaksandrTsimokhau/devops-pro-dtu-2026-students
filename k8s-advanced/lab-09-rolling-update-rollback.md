# Lab 09 — Rolling Update & Rollback

**Цель:** обновить image в Deployment без простоя, наблюдать процесс, откатиться назад.

**Время:** 20–30 минут
**Prerequisites:** Lab 08.

---

## Шаг 1. Поднимем v1.0 Deployment

Используйте `deploy.yaml` из Lab 08 (10 реплик, image `nigelpoulton/k8sbook:1.0`):

```bash
kubectl apply -f deploy.yaml
kubectl apply -f lb.yaml                  # LoadBalancer из Lab 08
kubectl get deploy hello-deploy
```

Откройте http://localhost:8080 — должна быть страница v1.

---

## Шаг 2. Update — поменяем image на v2

В `deploy.yaml` измените:
```yaml
        image: nigelpoulton/k8sbook:1.0
```
на:
```yaml
        image: nigelpoulton/k8sbook:2.0
```

Применяем:
```bash
kubectl apply -f deploy.yaml
```

Наблюдаем в отдельных терминалах:
```bash
# Terminal 1:
kubectl rollout status deployment hello-deploy
# Waiting for deployment "hello-deploy" rollout to finish: 4 out of 10...

# Terminal 2:
watch kubectl get pods -l app=hello-world

# Terminal 3:
kubectl get rs
# Теперь видим ДВА ReplicaSet:
# hello-deploy-<old-hash>  -- старый, replicas постепенно ↓
# hello-deploy-<new-hash>  -- новый, replicas постепенно ↑
```

Обновите страницу http://localhost:8080 — теперь будет v2 ("WebAssembly is coming!").

---

## Шаг 3. Pause & resume rollout

В отдельной сессии начните update v3:

```bash
# Подмените image на nigelpoulton/k8sbook:3.0 (или любой ваш) и apply
sed -i.bak 's|k8sbook:2.0|k8sbook:1.0|' deploy.yaml
kubectl apply -f deploy.yaml

# Сразу — pause
kubectl rollout pause deploy hello-deploy

# Посмотрите статус
kubectl describe deploy hello-deploy
# Progressing  Unknown  DeploymentPaused
# OldReplicaSets: hello-deploy-<v2-hash> (X/X)
# NewReplicaSet:  hello-deploy-<v1-hash> (Y/Y created)

# Resume
kubectl rollout resume deploy hello-deploy
kubectl rollout status deployment hello-deploy
```

---

## Шаг 4. Rollback

### Посмотрите историю
```bash
kubectl rollout history deployment hello-deploy
# REVISION   CHANGE-CAUSE
# 1          <none>
# 2          <none>
# 3          <none>
```

### Откат к конкретной ревизии
```bash
kubectl rollout undo deployment hello-deploy --to-revision=1
kubectl rollout status deployment hello-deploy
```

### Список всех ReplicaSet
```bash
kubectl get rs
# Старые RS остались — у них replicas=0, но они помнят свою конфигурацию.
```

> ⚠️ После rollback — обновите `deploy.yaml` в Git! Иначе следующий `apply` снова применит ту версию которая в файле.

---

## Шаг 5. Recreate strategy (для сравнения)

Создайте отдельный `deploy-recreate.yaml`:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: hello-recreate
spec:
  replicas: 5
  strategy:
    type: Recreate
  # ... остальное идентично hello-deploy
```

```bash
kubectl apply -f deploy-recreate.yaml
# Обновите image и снова apply.
# Все Pod-ы будут убиты сразу, потом новые подняты — будет простой.
```

> Recreate — для stateful apps или когда нельзя иметь две версии параллельно.

---

## Cleanup

```bash
kubectl delete -f lb.yaml
kubectl delete -f deploy.yaml
kubectl delete -f deploy-recreate.yaml 2>/dev/null || true
```

---

## Что вы узнали

- Rolling update = постепенная замена Pod-ов через **два ReplicaSet-а**
- `maxSurge` + `maxUnavailable` определяют скорость и risk-tolerance
- `kubectl rollout pause/resume` — для контроля
- `kubectl rollout undo` — мгновенный откат
- Старые ReplicaSet-ы сохраняются (limit: `revisionHistoryLimit`)
- После rollback **обязательно синхронизируйте YAML в Git**
