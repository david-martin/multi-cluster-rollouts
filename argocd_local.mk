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

.PHONY: kind
kind: $(KIND) ## Download kind locally if necessary.
$(KIND):
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/kind@$(KIND_VERSION)

ARGOCD_KUBECONFIG ?= $(SELF_DIR)/kubeconfig
argocd-start: kind
	KUBECONFIG=$(ARGOCD_KUBECONFIG) $(KIND) create cluster --name argocd --wait 5m --config $(SELF_DIR)kind.yaml --image kindest/node:v${K8S_VERSION}
	@make -s argocd-setup

argocd-stop:
	$(KIND) delete cluster --name=argocd || true

argocd-clean:
	rm -rf $(SELF_DIR)kubeconfig $(SELF_DIR)bin

ARGOCD_PASSWD = $(shell kubectl --kubeconfig=$(ARGOCD_KUBECONFIG) -n argocd get secret argocd-initial-admin-secret -o jsonpath="{.data.password}" | base64 -d)
argocd-password:
	@echo $(ARGOCD_PASSWD)

argocd-login: argocd
	@$(ARGOCD) login localhost:8443 --insecure --username admin --password $(ARGOCD_PASSWD) > /dev/null

argocd-setup: export KUBECONFIG=$(ARGOCD_KUBECONFIG)
argocd-setup: kustomize
	$(KUSTOMIZE) build $(SELF_DIR)config/argocd-install | $(KFILT) -i kind=CustomResourceDefinition | kubectl apply -f -
	$(KUSTOMIZE) build $(SELF_DIR)config/argocd-install | kubectl apply -f -
	kubectl -n argocd wait deployment argocd-server --for condition=Available=True --timeout=90s
	kubectl port-forward svc/argocd-server -n argocd 8443:443 > /dev/null  2>&1 &
	@echo -ne "\n\n\tConnect to ArgoCD UI in https://localhost:8443\n\n"
	@echo -ne "\t\tUser: admin\n"
	@echo -ne "\t\tPassword: "
	@make -s argocd-password
	@echo

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
