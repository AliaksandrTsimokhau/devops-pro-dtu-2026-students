# How to Use Prometheus Operator

This guide walks you through installing and using the Prometheus Operator in a real Kubernetes cluster. You’ll install the Operator, access the Prometheus and Grafana dashboards, and use some available CRDs to configure monitoring and alerting.

> **Prerequisites:**  
> - [kubectl](https://kubernetes.io/docs/tasks/tools/) and [Helm](https://helm.sh/docs/intro/install/) installed  
> - Active connection to a Kubernetes cluster

---

## Installation

### 1. Add the Helm Chart Repository

Register the Prometheus community Helm chart repository:

```sh
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
```

Update your repositories:

```sh
helm repo update
```

---

### 2. Deploy the Chart

Install the `kube-prometheus-stack` chart into its own namespace:

```sh
helm install kube-prometheus-stack \
    --create-namespace \
    --namespace kube-prometheus-stack \
    prometheus-community/kube-prometheus-stack
```

You should see output similar to:

```
NAME: kube-prometheus-stack
LAST DEPLOYED: Wed Sep 20 09:25:30 2023
NAMESPACE: kube-prometheus-stack
STATUS: deployed
REVISION: 1
NOTES:
kube-prometheus-stack has been installed. Check its status by running:
    kubectl --namespace kube-prometheus-stack get pods -l "release=kube-prometheus-stack"
```

---

### 3. Check Pod Status

Wait for all pods to be in the `Running` state:

```sh
kubectl get pods -n kube-prometheus-stack
```

Example output:

```
NAME                                                        READY   STATUS    RESTARTS   AGE
alertmanager-kube-prometheus-stack-alertmanager-0           2/2     Running   0          79s
kube-prometheus-stack-grafana-56b76d89cb-44z55              3/3     Running   0          93s
kube-prometheus-stack-kube-state-metrics-599db949d7-stlbh   1/1     Running   0          93s
kube-prometheus-stack-operator-66b485d695-r22pv             1/1     Running   0          93s
kube-prometheus-stack-prometheus-node-exporter-pkp55        1/1     Running   0          93s
prometheus-kube-prometheus-stack-prometheus-0               2/2     Running   0          79s
```

---

## Access Prometheus Dashboard

Prometheus is not exposed by default. To access the web UI, use port forwarding:

```sh
kubectl port-forward -n kube-prometheus-stack svc/kube-prometheus-stack-prometheus 9090:9090
```

This forwards your local port `9090` to the Prometheus service in your cluster.

Now, open [http://localhost:9090](http://localhost:9090) in your browser to access the Prometheus UI.



## Log into Grafana

Deploying the Prometheus Operator via the `kube-prometheus-stack` Helm chart also installs a Grafana instance for visualizing your metrics.

To access Grafana, start a port-forwarding session to the Grafana service:

```sh
kubectl port-forward -n kube-prometheus-stack svc/kube-prometheus-stack-grafana 8080:80
```

This will forward your local port `8080` to the Grafana service running in your cluster.

Now, open [http://localhost:8080](http://localhost:8080) in your browser.  
Log in with the following default credentials:

- **Username:** `admin`
- **Password:** `prom-operator`

Once logged in, explore the preconfigured dashboards by clicking the menu button in the top-left and selecting **Dashboards**.  
For example, the **General > Kubernetes / Compute Resources / Cluster** dashboard provides an overview of your cluster’s physical resource utilization.


## Exposing Prometheus Metrics from a Sample Application

To expose Prometheus metrics in a Kubernetes application, follow these steps:

### 1. Application with `/metrics` Endpoint

Your application must expose a `/metrics` endpoint that returns metrics in the [Prometheus exposition format](https://prometheus.io/docs/instrumenting/exposition_formats/).


---


---

### 2. Kubernetes Deployment and Service

- **Deployment:** Runs both an NGINX container and an NGINX Prometheus Exporter sidecar.
- **Service:** Exposes the exporter endpoint within the cluster.
- **Label:** Add a label (e.g., `monitoring: enabled`) to the Service for Prometheus discovery.

**Example YAML:**

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
    name: nginx-with-exporter
spec:
    replicas: 1
    selector:
        matchLabels:
            app: nginx-with-exporter
    template:
        metadata:
            labels:
                app: nginx-with-exporter
        spec:
            containers:
              - name: nginx
                image: nginx:stable
                ports:
                    - containerPort: 80
                volumeMounts:
                  - name: nginx-conf
                    mountPath: /etc/nginx/conf.d/default.conf
                    subPath: default.conf
              - name: nginx-exporter
                image: nginx/nginx-prometheus-exporter:latest
                args:
                - --nginx.scrape-uri=http://localhost:80/nginx_status
                ports:
                - containerPort: 9113
            volumes:
              - name: nginx-conf
                configMap:
                    name: nginx-stub-status-conf

---
apiVersion: v1
kind: ConfigMap
metadata:
    name: nginx-stub-status-conf
data:
    default.conf: |
        server {
            listen 80;
            server_name localhost;
            location /nginx_status {
                stub_status;
            }
        }
---
apiVersion: v1
kind: Service
metadata:
    name: nginx-exporter-service
    labels:
        monitoring: enabled
spec:
    selector:
        app: nginx-with-exporter
    ports:
      - name: metrics
        protocol: TCP
        port: 9113
        targetPort: 9113
```

**Note:**  
Make sure the NGINX container is configured to expose the `/stub_status` endpoint, which the exporter scrapes for metrics.

---

### Port-Forward to the Pod and Test Metrics

To verify that metrics are being exposed and collected:

1. **Port-forward to the NGINX pod's port 80** (for `/nginx_status`):

    ```sh
    kubectl port-forward deploy/nginx-with-exporter 8081:80
    ```

    Now, in another terminal, make a few requests to the NGINX status endpoint:

    ```sh
    curl http://localhost:8081/nginx_status
    ```

    Repeat this command several times to generate traffic.

2. **Port-forward to the NGINX Exporter on port 9113** (for `/metrics`):

    ```sh
    kubectl port-forward deploy/nginx-with-exporter 9113:9113
    ```

    In a new terminal, fetch the Prometheus metrics:

    ```sh
    curl http://localhost:9113/metrics
    ```

    Look for the `nginx_http_requests_total` metric in the output. Its value should increase as you make more requests to `/nginx_status`.

List of all the metrics available is here https://github.com/nginx/nginx-prometheus-exporter

---

### 3. Prometheus Configuration with ServiceMonitor

A `ServiceMonitor` resource tells Prometheus which services to scrape.

**Example YAML:**

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
    name: nginx-exporter-monitor
    labels:
        team: frontend
spec:
    selector:
        matchLabels:
            monitoring: enabled
    endpoints:
    - port: metrics
        interval: 30s
```

This configuration instructs Prometheus to scrape metrics from any service with the label `monitoring: enabled` on the port named `metrics` every 30 seconds.

---

### 4. Query your metric in the Prometheus UI
Now that you’ve created your ServiceMonitor, you can return to the Prometheus UI to query your app’s metrics.

Use the Expression input at the top of the screen to search for your `nginx_http_requests_total` metric.


## Key Points

- **Prometheus Operator** automates the deployment and configuration of Prometheus in Kubernetes clusters.
- It enables you to use Kubernetes **Custom Resource Definitions (CRDs)** to define:
    - Prometheus metrics monitors
    - Alerting rules
    - Alertmanager configurations
- Simplifies monitoring setup and management by leveraging Kubernetes-native resources.
- Facilitates scalable and consistent monitoring across your cluster.
