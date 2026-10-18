# Learning Environment Preparation

Follow these steps to set up your Kubernetes learning environment with all required tools.

## 1. Install Podman

- [Podman Desktop Downloads](https://podman-desktop.io/downloads)
- [Podman Installation Guide](https://podman.io/docs/installation)

## 2. Install Kind (Kubernetes IN Docker/Podman)

- [Kind Quick Start Guide](https://kind.sigs.k8s.io/docs/user/quick-start)
- Note: Kind can run with Docker or Podman as the container runtime.

## 3. Install kubectl (Kubernetes CLI)

- [kubectl Installation Instructions](https://kubernetes.io/docs/tasks/tools/install-kubectl-windows/)

## 4. Install Helm (Kubernetes Package Manager)

- [Helm Installation Guide](https://helm.sh/docs/intro/install/)

## 5. Install freelens (Kubernetes IDE)

- [freelens GitHub Repository](https://github.com/freelensapp/freelens?tab=readme-ov-file#windows)

## Prerequisites

- [Install kind](https://kind.sigs.k8s.io/)
- [Ingress documentation for kind](https://kind.sigs.k8s.io/docs/user/ingress/)
- [Install Metrics Server on kind](https://maggnus.com/install-metrics-server-on-the-kind-kubernetes-cluster-12b0a5faf94a)
- [Example Gist: Metrics Server on kind](https://gist.github.com/sanketsudake/a089e691286bf2189bfedf295222bd43)


# Create Cluster

```bash
export KIND_EXPERIMENTAL_PROVIDER=podman
```

Create a kind cluster with extraPortMappings and node-labels.

    extraPortMappings allow the local host to make requests to the Ingress controller over ports 80/443
    node-labels only allow the ingress controller to run on a specific node(s) matching the label selector

```bash
cat <<EOF | kind create cluster --name k8s-playground --config=-
# three node (two workers) cluster config
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  kubeadmConfigPatches:
  - |
    kind: InitConfiguration
    nodeRegistration:
      kubeletExtraArgs:
        node-labels: "ingress-ready=true"
  extraPortMappings:
  - containerPort: 80
    hostPort: 80
    protocol: TCP
  - containerPort: 443
    hostPort: 443
    protocol: TCP
- role: worker
- role: worker
EOF
```

# Create Ingress NGINX

```bash
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/deploy/static/provider/kind/deploy.yaml
```

```bash
kubectl wait --namespace ingress-nginx \
  --for=condition=ready pod \
  --selector=app.kubernetes.io/component=controller \
  --timeout=90s
```

Using Ingress

Note, this example uses an nginx-specific Ingress annotation which may not be supported by all Ingress implementations.

```yaml
kind: Pod
apiVersion: v1
metadata:
  name: foo-app
  labels:
    app: foo
spec:
  containers:
  - command:
    - /agnhost
    - netexec
    - --http-port
    - "8080"
    image: registry.k8s.io/e2e-test-images/agnhost:2.39
    name: foo-app
---
kind: Service
apiVersion: v1
metadata:
  name: foo-service
spec:
  selector:
    app: foo
  ports:
  # Default port used by the image
  - port: 8080
---
kind: Pod
apiVersion: v1
metadata:
  name: bar-app
  labels:
    app: bar
spec:
  containers:
  - command:
    - /agnhost
    - netexec
    - --http-port
    - "8080"
    image: registry.k8s.io/e2e-test-images/agnhost:2.39
    name: bar-app
---
kind: Service
apiVersion: v1
metadata:
  name: bar-service
spec:
  selector:
    app: bar
  ports:
  # Default port used by the image
  - port: 8080
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: example-ingress
  annotations:
    nginx.ingress.kubernetes.io/rewrite-target: /$2
spec:
  rules:
  - http:
      paths:
      - pathType: ImplementationSpecific
        path: /foo(/|$)(.*)
        backend:
          service:
            name: foo-service
            port:
              number: 8080
      - pathType: ImplementationSpecific
        path: /bar(/|$)(.*)
        backend:
          service:
            name: bar-service
            port:
              number: 8080
---
```

```bash
kubectl apply -f https://kind.sigs.k8s.io/examples/ingress/usage.yaml
```
Now verify that the ingress works

```bash
# should output "foo-app"
curl localhost/foo/hostname
# should output "bar-app"
curl localhost/bar/hostname
```


# LoadBalancer Services on kind + podman

Ingress (above) routes HTTP/HTTPS through ports 80/443. If instead you need a
`Service` of `type: LoadBalancer` to get an `EXTERNAL-IP`, kind does **not**
implement that on its own — the Service stays `<pending>`.

The official solution is the standalone host binary
[`cloud-provider-kind`](https://github.com/kubernetes-sigs/cloud-provider-kind):
it watches for `type: LoadBalancer` Services and launches an envoy load-balancer
container per Service.

> **Why the extra flag on macOS/Windows:** kind nodes run inside a VM (the podman
> machine), so the IP assigned to the balancer is **not routable** from your host.
> `--enable-lb-port-mapping` publishes the Service on `127.0.0.1:<port>` instead.

Install:
```bash
brew install cloud-provider-kind                       # macOS
go install sigs.k8s.io/cloud-provider-kind@latest      # any platform with Go
```

Run (keep it in its own terminal):

```bash
# macOS / WSL2 — requires sudo; point DOCKER_HOST at the rootful podman socket
sudo DOCKER_HOST="unix://$(podman machine inspect <MACHINE> --format '{{.ConnectionInfo.PodmanSocket.Path}}')" \
     KUBECONFIG="$HOME/.kube/config" \
     cloud-provider-kind --enable-lb-port-mapping
```

```powershell
# Native Windows — run from a PowerShell opened "as administrator"
$env:DOCKER_HOST = "npipe:////./pipe/<pipe-name>"      # from: podman machine inspect <MACHINE>
$env:KUBECONFIG  = "$HOME\.kube\config"
cloud-provider-kind --enable-lb-port-mapping
```

Gotchas (the ones that actually bite):
- **Root required** — `sudo` on macOS/WSL2, *Run as administrator* on Windows.
  Without it the binary exits with `please run this again with sudo`.
- Pass `DOCKER_HOST` and `KUBECONFIG` explicitly (inside `sudo VAR=val …`), or root
  won't see the podman socket nor your kubeconfig.
- Use a **rootful** podman machine — only then does the provider see the `kind` network.
- **Colima** fails to start the envoy container (`exit status 125`); use **podman**.

Verify:
```bash
kubectl apply -f https://kind.sigs.k8s.io/examples/loadbalancer/usage.yaml
kubectl get svc                                        # EXTERNAL-IP becomes real in ~30s
# find the host port the balancer was published on:
podman --connection <MACHINE>-root ps --filter name=kindccm   # PORTS: 0.0.0.0:NNNNN->...
curl http://127.0.0.1:NNNNN/                           # traffic reaches the Pod
```

> The `EXTERNAL-IP` itself is **not** reachable from the host on macOS/Windows —
> always go through `127.0.0.1:<mapped-port>`.

## Alternative: MetalLB

`cloud-provider-kind` is a *development* LB. On real bare-metal clusters (no cloud),
`type: LoadBalancer` is usually implemented by [**MetalLB**](https://metallb.universe.tf/),
which hands out IPs from a pool and announces them over ARP (L2) or BGP. Useful to
show students the "real" mechanism.

**Which to use:**

| | `cloud-provider-kind` | MetalLB |
|---|---|---|
| Purpose | local dev on kind | bare-metal prod + teaching |
| How IPs are served | envoy container per Service | IP pool + L2/BGP announce |
| Host access (macOS/Windows + podman) | ✅ via `127.0.0.1:<port>` (`--enable-lb-port-mapping`) | ❌ pool IP not routable from host (lives in the VM) |
| Access from inside cluster/node | ✅ | ✅ |
| Needs root | yes (`sudo` / admin) | no |

> **Verdict for macOS/Windows + podman:** MetalLB installs fine and assigns an IP,
> but that IP is **unreachable from the host** (node in a VM; known podman bug —
> `No route to host`). For laptop access use `cloud-provider-kind`. MetalLB here is
> for demonstrating the concept and testing from inside the cluster.

Install MetalLB (verified on kind + podman):
```bash
# 1. find the kind network subnet — the IP pool comes from it
podman --connection <MACHINE>-root network inspect kind \
  --format '{{range .Subnets}}{{.Subnet}} {{end}}'           # e.g. 10.89.0.0/24

# 2. install MetalLB (check github.com/metallb/metallb/releases for the current version)
kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.16.1/config/manifests/metallb-native.yaml
kubectl wait -n metallb-system --for=condition=ready pod --selector=app=metallb --timeout=120s
```

```bash
# 3. address pool + L2 advertisement (a range inside the kind subnet, not clashing with nodes)
cat <<EOF | kubectl apply -f -
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: kind-pool
  namespace: metallb-system
spec:
  addresses:
  - 10.89.0.200-10.89.0.250
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: kind-l2
  namespace: metallb-system
spec:
  ipAddressPools:
  - kind-pool
EOF
```

Verify (works from inside the cluster; not from the host on macOS/Windows + podman):
```bash
kubectl get svc                              # EXTERNAL-IP from the pool, e.g. 10.89.0.200
kubectl run lbtest --rm -i --restart=Never --image=curlimages/curl -- \
  curl -s http://10.89.0.200/                # ✅ response from the Pod
```


# Install Metrics Server

# Install Prometheus

https://medium.com/@giorgiodevops/kind-install-prometheus-operator-and-fix-missing-targets-b4e57bcbcb1f

## Kind configuration
```yaml
# File name cluster.yaml
apiVersion: kind.x-k8s.io/v1alpha4
kind: Cluster
name: prometheus-cluster
kubeadmConfigPatches:
- |-
  kind: ClusterConfiguration
  # configure controller-manager bind address
  controllerManager:
    extraArgs:
      bind-address: 0.0.0.0 #Disable localhost binding
      secure-port: "0"      #Disable the https 
      port: "10257"         #Enable http on port 10257
  # configure etcd metrics listen address
  etcd:
    local:
      extraArgs:
        listen-metrics-urls: http://0.0.0.0:2381
  # configure scheduler bind address
  scheduler:
    extraArgs:
      bind-address: 0.0.0.0  #Disable localhost binding
      secure-port: "0"       #Disable the https
      port: "10259"          #Enable http on port 10259
- |-
  kind: KubeProxyConfiguration
  # configure proxy metrics bind address
  metricsBindAddress: 0.0.0.0
nodes:
- role: control-plane
- role: worker
- role: worker
- role: worker
```
## install Prometheus operator using the helm chart repository https://github.com/prometheus-operator/prometheus-operator 

```bash
helm install --wait --timeout 15m \
   --namespace monitoring --create-namespace \
   --repo https://prometheus-community.github.io/helm-charts \
   kube-prometheus-stack kube-prometheus-stack --values - <<EOF 
 kubeEtcd:
   service:
     targetPort: 2381
 kubeControllerManager:  
   service:
     targetPort: 10257
 kubeScheduler:
   service:
     targetPort: 10259
EOF
```

## When the operator has been deployed just proxy the port `9090`
```bash
$ kubectl port-forward -n monitoring svc/kube-prometheus-stack-prometheus 9090:9090
```
and check the url http://localhost:9090/target