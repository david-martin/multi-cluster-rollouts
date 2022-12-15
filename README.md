# multi-cluster-rollouts
An experiment at providing rollout like features in a multi-cluster environment. Leverages ArogCD ApplicationSets

## Local setup

```
make argocd-setup
make argocd-start-target-clusters
make argocd-register-target-clusters
make argocd-create-example-applicationset
```