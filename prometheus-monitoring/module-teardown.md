# Module 07 — full teardown

Run this section once, when you are finished with the whole module. Each lab has its own `### Cleanup (только если вы останавливаетесь здесь)` section. Those remove only that lab's state, and later labs and the capstone depend on earlier state (the `monitoring` stack, `demo-monitoring`, `alert-sink`, Loki), so do not run them in the middle of the module.

Order matters:

1. **VM objects first, while the VictoriaMetrics operator still runs.** The operator reconciles the `VMServiceScrape`, `VMRule` and `VMPodScrape` objects created by Lab 07. Delete them before you remove the operator.
2. **Loki and Alloy** (Lab 08), together with the `demo-logs` workload.
3. **The stacks**: the VictoriaMetrics release (Lab 07), then the `kube-prometheus-stack` release (Lab 01).
4. **Namespaces and CRDs** last.

Имя кластера берём из текущего контекста: курсовой кластер вы создали сами (в модуле 02 это, например, `book`, а не `dtu`). Проверьте, что контекст указывает на ваш kind-кластер, а не на чужой:

```bash
kubectl config current-context        # должно быть kind-<имя вашего кластера>
export KIND_CLUSTER=$(kubectl config current-context | sed 's/^kind-//')
echo "$KIND_CLUSTER"; kind get clusters
```

## 1. Delete the VM objects (operator still running)

Only if you did Lab 07. If Lab 07 was skipped, the resource type does not exist and `kubectl` fails with `the server doesn't have a resource type "vmservicescrape"`, so the block below runs only when the type is present. It removes every object of these kinds in every namespace, so check the list first:

```bash
if kubectl api-resources | grep -q vmservicescrape; then
  kubectl get vmservicescrape,vmrule,vmpodscrape -A
  kubectl delete vmservicescrape,vmrule,vmpodscrape -A --all
fi
```

## 2. Remove Loki and Alloy (Lab 08)

Each `helm uninstall` runs only if the release still exists, so the step can be repeated after the Lab 08 cleanup (repeated `helm uninstall` fails with `release: not found`):

```bash
pkill -f "port-forward svc/loki" 2>/dev/null
helm status alloy -n logging >/dev/null 2>&1 && helm uninstall alloy -n logging
helm status loki -n logging >/dev/null 2>&1 && helm uninstall loki -n logging
kubectl delete namespace demo-logs logging --ignore-not-found
```

The Loki datasource in Grafana is part of the `monitoring` release. It disappears when you uninstall that release in step 3; re-applying the values without it does not remove it (проверено batchC). If you want to keep the stack, see the note in the Cleanup section of [lab-08-loki-logs.md](lab-08-loki-logs.md).

## 3. Remove the stacks

```bash
helm status vmks -n vm >/dev/null 2>&1 && helm uninstall vmks -n vm   # only if you did Lab 07
helm status monitoring -n monitoring >/dev/null 2>&1 && helm uninstall monitoring -n monitoring
```

If you want to keep the stack and only undo Lab 04, do not uninstall it. Remove the `alertmanagerConfigMatcherStrategy` block from `kps-values.yaml`, then run `helm upgrade --install monitoring prometheus-community/kube-prometheus-stack --namespace monitoring --version 87.2.0 -f kps-values.yaml` again.

## 4. Remove namespaces and CRDs

```bash
kubectl delete namespace demo-monitoring vm monitoring --ignore-not-found
```

The `alert-sink` deployment from Lab 04 is in `monitoring` and goes away with it. The optional Jaeger lab (`jaeger.md`) runs in its own namespace, so delete it separately if you did it: `kubectl delete namespace observability --ignore-not-found`.

Helm does not remove CRDs on uninstall. List the remaining ones:

```bash
kubectl get crd | grep -E 'monitoring.coreos.com|victoriametrics'
```

Delete them only if nothing else in the cluster uses them. Deleting a CRD removes every custom resource of that kind cluster-wide. `проверь: какие CRD ставит ваша версия chart (helm show crds prometheus-community/kube-prometheus-stack | grep 'name: '), прежде чем удалять их вручную`.

To delete exactly the CRDs from that list, without pasting names by hand:

```bash
kubectl get crd -o name | grep -E 'monitoring.coreos.com|victoriametrics' | xargs -r kubectl delete
```

## 5. GitLab and agent resources (capstone)

Only if you did the capstone or the GitLab Agent lab. The cluster side comes first, because the agent token is still needed to understand what to remove. The release and namespace names below are the ones [gitlab_agent_k8s_lab.md](../gitlab/gitlab_agent_k8s_lab.md) creates (`проверь:` если вы меняли имена):

```bash
helm status dtu-agent -n gitlab-agent >/dev/null 2>&1 && helm uninstall dtu-agent -n gitlab-agent
helm status app-staging -n staging >/dev/null 2>&1 && helm uninstall app-staging -n staging
helm status app-prod -n prod >/dev/null 2>&1 && helm uninstall app-prod -n prod
kubectl delete namespace gitlab-agent staging prod --ignore-not-found   # только если namespace создала лаба
```

The GitLab side is manual, in the project you used:

- ☐ Revoke the agent token (Operate → Kubernetes clusters) and delete the agent.
- ☐ Delete the `KUBE_CONFIG` CI/CD variable if you created it in the Helm lab.
- ☐ Delete the charts and images you pushed (Deploy → Package registry / Container registry).
- ☐ Stop or delete the environments (Operate → Environments).
- ☐ Revoke personal access tokens created for the labs, then delete the sandbox project or group.

На self-hosted GitLab в kind полная очистка — `kind delete cluster --name <имя GitLab-кластера>` (это другой кластер, не курсовой): сначала `kind get clusters`, чтобы не удалить чужой.

## 6. Local files (optional)

```bash
rm -f kps-values.yaml loki-values.yaml alloy-values.yaml grafana-loki-ds.yaml log-gen.yaml log-burst.yaml jaeger-ds.yaml
```

To remove the kind cluster as well: `kind delete cluster --name $KIND_CLUSTER`.

## Done when

- ☐ `kubectl get ns monitoring logging vm demo-monitoring demo-logs` returns `NotFound` for each
- ☐ `kubectl get crd | grep -E 'monitoring.coreos.com|victoriametrics'` shows nothing, or only CRDs you chose to keep
- ☐ `helm list -A` shows no `dtu-agent`, `app-staging` or `app-prod` (if you did the capstone), and the GitLab checklist in step 5 is done
