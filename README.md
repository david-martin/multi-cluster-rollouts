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

Re-register the target clusters (Issue with x509 cert error unless we do this again)
```
make argocd-register-target-clusters
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

You can see the Applications being created/synced/deleted from the ArgoCD UI at https://argocd.172.18.0.2.nip.io
The admin password can be retrieved with `make argocd-password`

You can send traffic to either instance of the example application with curl:

Cluster 1 (in-cluster)
```
while true;do curl --resolve argocd-test-multi-cluster-ingress.dev.hcpapps.net:80:172.18.0.2 http://argocd-test-multi-cluster-ingress.dev.hcpapps.net && sleep 1;done
```

Cluster 2 (argocd-target-cluster-01)
```
while true;do curl --resolve argocd-test-multi-cluster-ingress.dev.hcpapps.net:80:172.18.0.3 http://argocd-test-multi-cluster-ingress.dev.hcpapps.net && sleep 1;done
```