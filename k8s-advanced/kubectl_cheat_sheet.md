# Kubectl Cheat Sheet

## Basic Commands – Getting Information

| Command                       | Description                |
|-------------------------------|----------------------------|
| `kubectl cluster-info`        | View Cluster Information   |
| `kubectl get nodes`           | View All Nodes             |
| `kubectl get pods`            | View All Pods              |
| `kubectl get services`        | View All Services          |
| `kubectl get deployments`     | View All Deployments       |

## Pod Management

| Command                                              | Description                        |
|------------------------------------------------------|------------------------------------|
| `kubectl run <pod-name> --image=<image-name>`        | Create a Pod                       |
| `kubectl delete pod <pod-name>`                      | Delete a Pod                       |
| `kubectl describe pod <pod-name>`                    | Describe a Pod                     |
| `kubectl logs <pod-name>`                            | Log output from a Pod              |
| `kubectl exec -it <pod-name> -- /bin/sh`             | Start an interactive shell in a pod|

## Deployment Management

| Command                                                                 | Description          |
|-------------------------------------------------------------------------|----------------------|
| `kubectl create deployment <deployment-name> --image=<image-name>`      | Create a Deployment  |
| `kubectl scale deployment <deployment-name> --replicas=<number>`        | Scale a Deployment   |
| `kubectl set image deployment/<deployment-name> <container>=<image>`    | Update a Deployment  |
| `kubectl delete deployment <deployment-name>`                           | Delete a Deployment  |

## Service Management

| Command                                                                 | Description                  |
|-------------------------------------------------------------------------|------------------------------|
| `kubectl expose deployment <deployment-name> --type=<type> --port=<port>`| Create a Service             |
| `kubectl describe service <service-name>`                               | View Details of a Service    |
| `kubectl delete service <service-name>`                                 | Delete a Service             |

## Namespaces

| Command                                                        | Description                       |
|----------------------------------------------------------------|-----------------------------------|
| `kubectl get namespaces`                                       | List all Namespaces               |
| `kubectl create namespace <namespace-name>`                    | Create a Namespace                |
| `kubectl delete namespace <namespace-name>`                    | Delete a Namespace                |
| `kubectl config set-context --current --namespace=<namespace>` | Switch context to a Namespace     |

## Configuration and Management

| Command                                   | Description                                 |
|--------------------------------------------|---------------------------------------------|
| `kubectl apply -f <file>.yaml`             | Apply a configuration from the file         |
| `kubectl delete -f <file>.yaml`            | Delete Resources from a Configuration File  |
| `kubectl get all`                          | View Current Configurations                 |
| `kubectl get <resource> <name> -o yaml`    | View Configuration of a Resource            |

## Troubleshooting Commands

| Command                                 | Description                                 |
|------------------------------------------|---------------------------------------------|
| `kubectl get events`                     | View Events                                 |
| `kubectl describe pod <pod-name>`        | View Pod Status                             |
| `kubectl exec -it <pod-name> -- <cmd>`   | Execute a Command in a Pod                  |
| `kubectl logs -f <pod-name>`             | Follow the logs of a pod in real-time       |
| `kubectl top pod`                        | Display resource usage for pods             |
| `kubectl top node`                       | Display resource usage for nodes            |

## Node Management

| Command                          | Description                        |
|-----------------------------------|------------------------------------|
| `kubectl get nodes`               | View All Nodes                     |
| `kubectl cordon <node-name>`      | Mark a node as unschedulable       |
| `kubectl drain <node-name>`       | Evict all pods from a node         |
| `kubectl uncordon <node-name>`    | Mark a node as schedulable         |

## Krew Plugins

| Command                                                      | Description                                                        |
|--------------------------------------------------------------|--------------------------------------------------------------------|
| `kubectl krew install neat`                                  | Format kubectl get output for better readability                   |
| `kubectl krew install tree`                                  | Visualize resource hierarchies and relationships                   |
| `kubectl tree deployment <deployment-name>`                  | Display a tree view of a deployment and its related objects        |
| `kubectl krew install ctx`                                   | Manage and switch between multiple Kubernetes contexts             |
| `kubectl krew install resources`                             | View resource requests and limits for pods and containers          |

## Output Formats

| Формат вывода                        | Описание                                                                 |
|---------------------------------------|--------------------------------------------------------------------------|
| `-o=custom-columns=<spec>`            | Вывод таблицы из списка пользовательских столбцов через запятую          |
| `-o=custom-columns-file=<filename>`   | Вывод таблицы из списка пользовательских столбцов, определённых в файле  |
| `-o=json`                            | Вывод API-объекта в формате JSON                                         |
| `-o=jsonpath=<template>`              | Вывод полей, определённых в выражении jsonpath                           |
| `-o=jsonpath-file=<filename>`         | Вывод полей, определённых в выражении jsonpath из файла                  |
| `-o=name`                            | Вывод только имена ресурсов                                              |
| `-o=wide`                            | Вывод дополнительной информации, например, имя узла для подов            |
| `-o=yaml`                            | Вывод API-объекта в формате YAML                                         |
