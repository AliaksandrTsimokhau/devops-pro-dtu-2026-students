# Lab 19 — Secrets: create, mount, decode

**Цель:** создать Secret, замонтировать в Pod как volume, проверить что base64 ≠ encryption.

**Время:** 15 минут
**Prerequisites:** Lab 18.

---

## Шаг 1. Imperative — Secret из литералов

```bash
kubectl create secret generic db-creds \
  --from-literal=username=admin \
  --from-literal=password='S3cr3t!'

kubectl get secret db-creds -o yaml
```

Видим:
```yaml
data:
  password: UzNjcjN0IQ==
  username: YWRtaW4=
```

**Это base64, НЕ encryption!**
```bash
echo "UzNjcjN0IQ==" | base64 -d
# S3cr3t!
```

---

## Шаг 2. Declarative — через stringData

`secret.yaml`:
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: tkb-secret
type: Opaque
stringData:                           # plain text — K8s сам base64-encodes
  username: admin
  password: superSecret123
  api-key: aBcDeFgHiJ
```

```bash
kubectl apply -f secret.yaml
kubectl get secret tkb-secret -o yaml
# теперь data/ — base64
```

---

## Шаг 3. Mount Secret как volume

`pod-secret.yaml`:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: secret-app
spec:
  containers:
  - name: app
    image: nginx:1.27-alpine
    volumeMounts:
    - name: secrets
      mountPath: /etc/secrets
      readOnly: true
    env:
    - name: DB_PASSWORD
      valueFrom:
        secretKeyRef:
          name: tkb-secret
          key: password
  volumes:
  - name: secrets
    secret:
      secretName: tkb-secret
```

```bash
kubectl apply -f pod-secret.yaml
kubectl get pods
```

---

## Шаг 4. Внутри Pod-а Secret в plain text

```bash
kubectl exec secret-app -- ls /etc/secrets
# api-key  password  username

kubectl exec secret-app -- cat /etc/secrets/password
# superSecret123

# Env var тоже plain text
kubectl exec secret-app -- env | grep DB_PASSWORD
# DB_PASSWORD=superSecret123
```

> Внутри контейнера Secret — это plain text. Это специально: app должен прочитать.

---

## Шаг 5. Secret монтируется как tmpfs (память, не диск)

```bash
kubectl exec secret-app -- mount | grep secret
# tmpfs on /etc/secrets type tmpfs (ro,relatime,size=...)
```

> `tmpfs` = в памяти. Не пишется на диск ноды.

---

## Шаг 6. Specific Secret types

### TLS Secret
```bash
# Создание из cert+key
kubectl create secret tls my-tls \
  --cert=path/to/cert.pem \
  --key=path/to/key.pem

# Манифест
cat <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: my-tls
type: kubernetes.io/tls
data:
  tls.crt: |
    LS0tLS1CRUdJTi...
  tls.key: |
    LS0tLS1CRUdJTi...
EOF
```

Используется в Ingress:
```yaml
spec:
  tls:
  - hosts: [example.com]
    secretName: my-tls
```

### Docker registry secret
```bash
kubectl create secret docker-registry regcred \
  --docker-server=ghcr.io \
  --docker-username=USER \
  --docker-password=TOKEN \
  --docker-email=me@example.com

# Используется в Pod через imagePullSecrets
```

---

## Шаг 7. RBAC — кто может читать Secret?

```bash
# Проверка прав текущего пользователя
kubectl auth can-i get secrets
kubectl auth can-i get secrets/tkb-secret
```

> В production: давайте RBAC на конкретные Secret-ы (`resourceNames`), не на все.

---

## Cleanup

```bash
kubectl delete -f pod-secret.yaml -f secret.yaml
kubectl delete secret db-creds
```

---

## Что вы узнали

- Secret = ConfigMap с base64-encoded values, mounted as tmpfs
- **base64 ≠ encryption**. Не коммитьте Secret YAML в Git напрямую
- Внутри Pod-а Secret — plain text (для использования app-ом)
- Specific Secret types: `Opaque`, `kubernetes.io/tls`, `kubernetes.io/dockerconfigjson`, `kubernetes.io/ssh-auth`
- Production: Sealed Secrets, External Secrets Operator + Vault / AWS SM / GCP SM
- RBAC на Secret должен быть очень узким (`resourceNames` рекомендуется)
