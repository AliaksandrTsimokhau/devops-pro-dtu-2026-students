# Appendix — Доступ к UI и наблюдение за прогрессом

**Зачем:** многие лабы поднимают веб-дашборды (Argo CD, Kubernetes Dashboard,
Goldilocks, Grafana) или меняют состояние кластера в реальном времени (HPA scale,
rollout, scheduling, drain). Этот аппендикс — общий справочник: **как зайти в UI**
(port-forward / Ingress / NodePort) и **как смотреть прогресс** из терминала.

> Лабы ссылаются сюда вместо дублирования. В **kind** по умолчанию используйте
> **port-forward** — он работает всегда и ничего не требует от кластера.

---

## 1. Доступ к UI

### Вариант A — port-forward (по умолчанию, работает везде)

```bash
kubectl -n <namespace> port-forward svc/<service> <локальный>:<порт-сервиса>
# пример (Argo CD, сервис слушает HTTPS на 443):
kubectl -n argocd port-forward svc/argocd-server 8080:443
# → откройте https://localhost:8080
```

Путь трафика:

```
браузер :8080  ──▶  kubectl port-forward  ──▶  Service  ──▶  Pod :8443
   (ваш ноут)          (туннель к API)         (ClusterIP)   (контейнер)
```

- **TLS-нюанс:** форвардьте на тот порт, который реально слушает сервис.
  Argo CD и Kubernetes Dashboard — это **443 (HTTPS)** → `https://localhost:...`
  (браузер ругнётся на самоподписанный серт — это норма для лабы).
- port-forward **держит сессию**, пока команда запущена. Запуск в фоне:
  ```bash
  kubectl -n argocd port-forward svc/argocd-server 8080:443 &
  # остановить: kill %1   (или fg → Ctrl+C)
  ```
- Падает при разрыве/таймауте — просто перезапустите.

### Вариант B — Ingress (ближе к production)

Нужен установленный ingress-controller (см. Lab 11) и маппинг хоста локально.

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: argocd
  annotations:
    nginx.ingress.kubernetes.io/backend-protocol: "HTTPS"   # бэкенд на HTTPS
spec:
  ingressClassName: nginx
  rules:
  - host: argocd.local
    http:
      paths:
      - path: /
        pathType: Prefix
        backend: {service: {name: argocd-server, port: {number: 443}}}
```

```bash
echo "127.0.0.1 argocd.local" | sudo tee -a /etc/hosts   # резолв хоста локально
# → http://argocd.local  (через ingress-controller)
```

> ⚠️ В **kind** Ingress требует, чтобы кластер был создан с пробросом портов
> 80/443 (`extraPortMappings`) и стоял ingress-controller. Если этого нет —
> используйте port-forward (Вариант A). Ingress показываем «как в проде».

### Вариант C — NodePort (быстрый доступ без ingress)

```bash
kubectl -n <ns> patch svc <service> -p '{"spec":{"type":"NodePort"}}'
kubectl -n <ns> get svc <service>     # смотрим назначенный 3xxxx порт
```

> NodePort открывает порт на ноде. В kind к нему ещё нужно пробросить порт
> наружу — поэтому для локальной лабы port-forward обычно проще.

### Что выбрать

| Сценарий | Выбор |
|---|---|
| Локальная лаба в kind | **port-forward** |
| Показать «как в проде» / постоянный доступ | Ingress |
| Быстро, без ingress-controller | NodePort |

---

## 2. Наблюдение за прогрессом из терминала

Для динамических лаб (HPA, scheduling, rollout, drain, VPA, операторы, Argo) —
смотрите изменения «вживую», а не одноразовым `get`:

| Команда | Когда |
|---|---|
| `kubectl get <res> -w` | следить за одним ресурсом (статус меняется построчно) |
| `watch -n2 'kubectl get pods -o wide'` | обновлять целую картину каждые 2 сек |
| `kubectl rollout status deploy/<name>` | блокироваться до конца раскатки |
| `kubectl events --watch` | поток событий (preemption, evict, probe failed, scale) |
| `kubectl get hpa,pods -w` | HPA: видеть рост реплик в реальном времени |

```bash
# пример: смотреть, как topology spread раскидывает Pod-ы по нодам (Lab 27)
watch -n2 'kubectl get pods -l app=spread -o wide --no-headers | awk "{print \$7}" | sort | uniq -c'
```

---

## 3. k9s — терминальный дашборд (рекомендуется)

Лёгкая TUI для наблюдения за кластером без браузера — Pod-ы, логи, события,
ресурсы в реальном времени.

```bash
brew install k9s            # macOS  (Linux: см. https://k9scli.io)
k9s                         # запуск в текущем контексте
# :pods  :deploy  :events  — переключение ресурсов;  l — логи;  d — describe
```

> k9s особенно удобен в лабах HPA/scheduling/drain: одно окно — и видно, как
> Pod-ы появляются, переезжают и умирают.

---

## 4. Kubernetes Dashboard — общий веб-UI (опционально)

Официальный веб-UI кластера. Ставится один раз, переиспользуется во многих лабах.

```bash
# установка (manifest-вариант, стабилен для лаб)
kubectl apply -f https://raw.githubusercontent.com/kubernetes/dashboard/v2.7.0/aio/deploy/recommended.yaml
kubectl -n kubernetes-dashboard rollout status deploy/kubernetes-dashboard --timeout=120s

# админ-доступ: ServiceAccount + токен
kubectl apply -f - <<'EOF'
apiVersion: v1
kind: ServiceAccount
metadata: {name: dashboard-admin, namespace: kubernetes-dashboard}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata: {name: dashboard-admin}
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: cluster-admin}
subjects: [{kind: ServiceAccount, name: dashboard-admin, namespace: kubernetes-dashboard}]
EOF

# токен для входа
kubectl -n kubernetes-dashboard create token dashboard-admin

# доступ (сервис на HTTPS 443)
kubectl -n kubernetes-dashboard port-forward svc/kubernetes-dashboard 8443:443
# → https://localhost:8443  → вставьте токен
```

> ⚠️ `cluster-admin` для дашборда — только для **учебного** кластера. В проде
> давайте дашборду минимальные права (read-only ClusterRole).

Удалить:
```bash
kubectl delete -f https://raw.githubusercontent.com/kubernetes/dashboard/v2.7.0/aio/deploy/recommended.yaml
kubectl delete clusterrolebinding dashboard-admin
```

---

## Шпаргалка

```bash
# UI (kind → port-forward):
kubectl -n <ns> port-forward svc/<svc> <local>:<svcport>   # HTTPS → https://localhost:<local>

# прогресс:
kubectl get <res> -w
watch -n2 'kubectl get pods -o wide'
kubectl rollout status deploy/<name>
kubectl events --watch
k9s
```
