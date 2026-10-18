# Learning Environment Preparation

Follow these steps to set up your Kubernetes learning environment with all required tools.

## 1. Install Docker

- [Docker Desktop for Windows](https://docs.docker.com/desktop/setup/install/windows-install/)
- [Docker Desktop for Linux](https://docs.docker.com/desktop/setup/install/linux/)

## 2. Install Podman

- [Podman Desktop Downloads](https://podman-desktop.io/downloads)
- [Podman Installation Guide](https://podman.io/docs/installation)

## 3. Install Minikube

- [Minikube Installation Guide](https://minikube.sigs.k8s.io/docs/start/)

## 4. Install kubectl (Kubernetes CLI)

- [kubectl Installation Guide](https://kubernetes.io/docs/tasks/tools/install-kubectl-windows/)

## 5. Install Helm (Kubernetes package manager)

- [Helm Installation Guide](https://helm.sh/docs/intro/install/)

## 6. Install Freelens (Kubernetes IDE)

- [Freelens for Windows](https://github.com/freelensapp/freelens?tab=readme-ov-file#windows)

> **Tip:**  
> After installation, verify each tool by running its version command, e.g., `kubectl version --client`, `minikube version`, etc.





# Minikube + Podman Setup Guide
[Minikube Official Docs](https://minikube.sigs.k8s.io/docs/start/)

## Installation

```sh
brew install podman
brew install minikube
```

## Initialize and Start Podman Machine

```sh
podman machine init --cpus 2 --memory 2048 --rootful
podman machine start
```

## Start Minikube Cluster

```sh
minikube start --nodes 2 -p multinode-demo --driver=podman
```

## Common Minikube Commands

- **Pause Kubernetes:**  
    ```sh
    minikube pause
    ```

- **Unpause Minikube:**  
    ```sh
    minikube unpause
    ```

- **Stop the cluster:**  
    ```sh
    minikube stop
    ```

- **Set memory limit (requires restart):**  
    ```sh
    minikube config set memory 9001
    ```

- **List available addons:**  
    ```sh
    minikube addons list
    ```

- **Create a cluster with a specific Kubernetes version:**  
    ```sh
    minikube start -p aged --kubernetes-version=v1.16.1
    ```

- **Delete all clusters:**  
    ```sh
    minikube delete --all
    ```

- **Access the dashboard:**  
    ```sh
    minikube dashboard
    minikube dashboard --url
    ```

## Enable Metrics Server

```sh
minikube -p multinode-demo addons enable metrics-server
```

## Rootless Podman Configuration

```sh
minikube config set rootless true
minikube config set driver podman
minikube start --driver=podman --container-runtime=containerd
```

## Enable Ingress Addon

```sh
minikube addons enable ingress
```

> **Note:**  
> On macOS and Windows, the Docker driver requires `minikube tunnel` for ingress to work properly.  
> Run `minikube tunnel` in a separate terminal window (it may appear to hang, but that's expected), then enable ingress again.

```sh
minikube tunnel
```

---

## Example: Enabling Ingress and Testing

1. **Start Minikube:**
        ```sh
        minikube start
        ```

2. **Enable Ingress Addons:**
        ```sh
        minikube addons enable ingress
        minikube addons enable ingress-dns
        ```

3. **Wait for the ingress controller to be ready:**
        ```sh
        kubectl get pods -n ingress-nginx
        ```
        Wait until `ingress-nginx-controller-XXXX` is running.

4. **Create an Ingress resource using a Kubernetes YAML file.**

5. **Update the service section** to point to the NodePort Service you created.

6. **Edit `/etc/hosts` on macOS:**
        ```
        127.0.0.1 hello-world.info
        ```
        > **Note:** Do **not** use the Minikube IP.

7. **Run Minikube Tunnel:**
        ```sh
        minikube tunnel
        ```
        Keep this terminal open. After entering your password, there will be no further messages.

8. **Test in your browser:**  
     Visit `http://hello-world.info` (or the host you configured) to verify it works.



