SELF_DIR := $(dir $(lastword $(MAKEFILE_LIST)))
OS := $(shell uname -s | tr '[:upper:]' '[:lower:]')
ARCH := $(shell uname -m | sed 's/x86_64/amd64/')
K8S_VERSION ?= 1.23.12

KFILT = docker run --rm -i ryane/kfilt

## Tool Binaries
KUSTOMIZE ?= $(LOCALBIN)/kustomize
KIND ?= $(LOCALBIN)/kind


## Tool Versions
KUSTOMIZE_VERSION ?= v4.5.4
KIND_VERSION ?= v0.14.0
NGINX_CONTOLLER_VERSION=controller-v1.2.1

.PHONY: kind
kind: $(KIND) ## Download kind locally if necessary.
$(KIND):
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/kind@$(KIND_VERSION)

ARGOCD_KUBECONFIG ?= $(SELF_DIR)/kubeconfig
export KUBECONFIG=$(ARGOCD_KUBECONFIG)
argocd-start: kind
	$(KIND) create cluster --name argocd --wait 5m --config $(SELF_DIR)kind-with-ingress.yaml --image kindest/node:v${K8S_VERSION}
	kubectl config use-context kind-argocd
# Deploy the ingress-controller so that Ingresses get reconciled AND allow external access from portMappings
	kubectl config set-context --current --namespace=ingress-nginx
	$(KUSTOMIZE) build config/ingress-nginx | kubectl apply -f - 
	kubectl annotate ingressclass nginx "ingressclass.kubernetes.io/is-default-class=true"
	@echo "Waiting for deployments to be ready ..."
	kubectl -n ingress-nginx wait --for=condition=ready pod --selector=app.kubernetes.io/component=controller --timeout=90s
	kubectl config set-context --current --namespace=monitoring
	$(KUSTOMIZE) build config/thanos | kubectl apply -f - 
	$(KUSTOMIZE) build config/kube-prometheus | $(KFILT) -i kind=CustomResourceDefinition | kubectl create -f -
	$(KUSTOMIZE) build config/kube-prometheus | $(KFILT) -x kind=CustomResourceDefinition | kubectl apply -f - 
	@make -s argocd-setup
	@make argocd-start-target-clusters
	@make argocd-register-target-clusters

argocd-start-target-clusters: kind
	$(KIND) create cluster --name argocd-target-cluster-01 --wait 5m --config $(SELF_DIR)kind.yaml --image kindest/node:v${K8S_VERSION}
	kubectl config use-context kind-argocd-target-cluster-01
# Deploy the ingress-controller so that Ingresses get reconciled. However, we don't rely on external access at this time for this cluster
	kubectl config set-context --current --namespace=ingress-nginx
	$(KUSTOMIZE) build config/ingress-nginx | kubectl apply -f - 
	kubectl annotate ingressclass nginx "ingressclass.kubernetes.io/is-default-class=true"
	@echo "Waiting for deployments to be ready ..."
	kubectl -n ingress-nginx wait --for=condition=ready pod --selector=app.kubernetes.io/component=controller --timeout=90s
	kubectl create namespace monitoring
	kubectl config set-context --current --namespace=monitoring
	$(KUSTOMIZE) build config/kube-prometheus | $(KFILT) -i kind=CustomResourceDefinition | kubectl create -f - 
	$(KUSTOMIZE) build config/kube-prometheus | $(KFILT) -x kind=CustomResourceDefinition -x kind=Ingress | kubectl apply -f - 
	kubectl rollout status --watch --timeout=90s deployment/prometheus-operator
	kubectl rollout status --watch --timeout=90s statefulset/prometheus-k8s
	kubectl port-forward svc/prometheus-k8s 9090:9090 > /dev/null  2>&1 &

argocd-register-target-clusters: kustomize
	kind get kubeconfig --internal --name argocd-target-cluster-01 > argocd-target-cluster-01.kubeconfig
	CLUSTER_NAME=argocd-target-cluster-01 \
		CLUSTER_SERVER=https://argocd-target-cluster-01-control-plane:6443 KEYDATA=$(shell cat argocd-target-cluster-01.kubeconfig | yq '.users[0].user.client-key-data') CADATA=$(shell cat argocd-target-cluster-01.kubeconfig | yq '.clusters[0].cluster.certificate-authority-data') CERTDATA=$(shell cat argocd-target-cluster-01.kubeconfig | yq '.users[0].user.client-certificate-data') envsubst < cluster.yaml.template > cluster.yaml
	kubectl --context kind-argocd -n argocd apply -f cluster.yaml
#	$(KUSTOMIZE) build config/argocd-clusters/roles/ | kubectl --context kind-argocd-target-cluster-01 apply -n default -f -
#	kubectl --context kind-argocd-target-cluster-01 get serviceaccount argocd-manager -n default -o=jsonpath='{.secrets[0].name}' | xargs kubectl get secret -n default -o=json > cluster.json

argocd-create-example-applicationset:
	$(KUSTOMIZE) build config/argocd-applications/example | kubectl --context kind-argocd -n argocd apply -f - 

argocd-stop: kind
	$(KIND) delete cluster --name=argocd || true
	$(KIND) delete cluster --name=argocd-target-cluster-01 || true

argocd-clean:
	rm -rf $(SELF_DIR)kubeconfig $(SELF_DIR)bin

ARGOCD_PASSWD = $(shell kubectl --kubeconfig=$(ARGOCD_KUBECONFIG) --context kind-argocd -n argocd get secret argocd-initial-admin-secret -o jsonpath="{.data.password}" | base64 -d)
argocd-password:
	@echo $(ARGOCD_PASSWD)

argocd-login: argocd
	@$(ARGOCD) login argocd.172.18.0.2.nip.io:443 --insecure --username admin --password $(ARGOCD_PASSWD) > /dev/null

argocd-setup: export KUBECONFIG=$(ARGOCD_KUBECONFIG)
argocd-setup: kustomize
	$(KUSTOMIZE) build $(SELF_DIR)config/argocd-install | $(KFILT) -i kind=CustomResourceDefinition | kubectl apply -f -
	$(KUSTOMIZE) build $(SELF_DIR)config/argocd-install | kubectl apply -f -
	kubectl -n argocd wait deployment argocd-server --for condition=Available=True --timeout=90s

	@echo -ne "\n\n\tConnect to ArgoCD UI in https://argocd.172.18.0.2.nip.io\n\n"
	@echo -ne "\t\tUser: admin\n"
	@echo -ne "\t\tPassword: "
	@make -s argocd-password
	@echo

# 	kubectl port-forward svc/argocd-server -n argocd 8444:443 > /dev/null  2>&1 &
argocd-port-forward-stop:
	pkill kubectl

##@ Install argocd and configure the root:test workspace
ARGOCD ?= $(LOCALBIN)/argocd
ARGOCD_VERSION ?= v2.4.12
ARGOCD_DOWNLOAD_URL ?= https://github.com/argoproj/argo-cd/releases/download/v2.4.13/argocd-$(OS)-$(ARCH)
argocd: $(ARGOCD) ## Download argocd CLI locally if necessary
$(ARGOCD):
	curl -sL $(ARGOCD_DOWNLOAD_URL) -o $(ARGOCD)
	chmod +x $(ARGOCD)


# kubectl port-forward svc/prometheus-k8s -n monitoring 9090:9090 > /dev/null  2>&1 &
# kubectl port-forward svc/thanos-query -n monitoring 9091:9090 > /dev/null  2>&1 &