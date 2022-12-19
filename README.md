# multi-cluster-rollouts
An experiment at providing rollout like features in a multi-cluster environment. Leverages ArogCD ApplicationSets

## Local setup

Create clusters & deploy ArgoCD
```
make argocd-start
```

Install CRD(s)
```
make install
```

Create the example ApplicationSet
```
make argocd-create-example-applicationset
```

Start the controller locally
```
make run
```

Modify the Placement spec.clusters field to specify which cluster to deploy the ApplicationSet on.
Options are `in-cluster` and `argocd-target-cluster-01`
```
export KUBECONFIG=$(pwd)/kubeconfig
kubectl config use-context kind-argocd
kubectl config set-context --current --namespace=argocd

kubectl edit placement example
```

You can see the Applications being created/synced/deleted from the ArgoCD UI at https://localhost:8443
The admin password can be retrieved with `make argocd-password`