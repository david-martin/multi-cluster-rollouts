/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/strings/slices"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	rolloutsv1alpha1 "github.com/david-martin/multi-cluster-rollouts/api/v1alpha1"
)

// PlacementReconciler reconciles a Placement object
type PlacementReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=rollouts.example.com,resources=placements,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rollouts.example.com,resources=placements/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=rollouts.example.com,resources=placements/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Placement object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.1/pkg/reconcile
func (r *PlacementReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the Placement
	var placement rolloutsv1alpha1.Placement
	if err := r.Get(ctx, req.NamespacedName, &placement); err != nil {
		log.Error(err, "unable to fetch Placement")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Update status with decisions
	existingClusters := placement.Spec.Clusters
	existingClusterDecisions := []string{}
	clusterDecisions := []rolloutsv1alpha1.ClusterDecision{}

	for _, ecd := range placement.Status.Decisions {
		if ecd.PendingRemoval {
			log.Info("Cluster pending removal from decisions", "cluster", ecd.ClusterName)
			clusterDecisions = append(clusterDecisions, ecd)
		} else if !slices.Contains(existingClusters, ecd.ClusterName) {
			log.Info("Cluster will be removed from decisions", "cluster", ecd.ClusterName)
			if placement.Spec.RemoveAnalysis != "" {
				log.Info("Creating AnalysisRun", "cluster", ecd.ClusterName, "analysisTemplate", placement.Spec.RemoveAnalysis)
				analysisTemplateNamespacedName := types.NamespacedName{
					Namespace: placement.Namespace,
					Name:      placement.Spec.RemoveAnalysis,
				}
				err := r.CreateAnalysisRun(ctx, analysisTemplateNamespacedName)
				if err != nil {
					log.Error(err, "unable to create AnalysisRun")
					return ctrl.Result{}, err
				}
			}

			// Leave cluster in decisions until AnalysisRun is successful
			ecd.PendingRemoval = true
			// TODO: Remove clusterDecision when AnalysisRun is successful
			clusterDecisions = append(clusterDecisions, ecd)
		}
		existingClusterDecisions = append(existingClusterDecisions, ecd.ClusterName)
	}

	for _, cluster := range existingClusters {
		clusterDecision := rolloutsv1alpha1.ClusterDecision{
			ClusterName:    cluster,
			PendingRemoval: false,
		}
		if !slices.Contains(existingClusterDecisions, cluster) {
			log.Info("Cluster will be added to decisions", "cluster", cluster)
			if placement.Spec.ReadyAnalysis != "" {
				// TODO: Allow for a cluster going from pendingRemoval to being added back
				log.Info("Creating AnalysisRun", "cluster", cluster, "analysisTemplate", placement.Spec.ReadyAnalysis)
				analysisTemplateNamespacedName := types.NamespacedName{
					Namespace: placement.Namespace,
					Name:      placement.Spec.ReadyAnalysis,
				}
				err := r.CreateAnalysisRun(ctx, analysisTemplateNamespacedName)
				if err != nil {
					log.Error(err, "unable to create AnalysisRun")
					return ctrl.Result{}, err
				}
			}
		}
		// Update existing cluster decision for this cluster if there is one e.g. previously pending removal
		updatedExistingDecision := false
		for i, decision := range clusterDecisions {
			if decision.ClusterName == cluster {
				clusterDecisions[i] = clusterDecision
				updatedExistingDecision = true
				break
			}
		}
		// Otherwise, add the decision
		if !updatedExistingDecision {
			clusterDecisions = append(clusterDecisions, clusterDecision)
		}
	}

	placement.Status = rolloutsv1alpha1.PlacementStatus{
		Decisions: clusterDecisions,
	}

	log.Info("Updating Placement status", "decisions", placement.Status.Decisions)
	if err := r.Status().Update(ctx, &placement); err != nil {
		log.Error(err, "unable to update Placement status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *PlacementReconciler) CreateAnalysisRun(ctx context.Context, analysisTemplateNamespacedName types.NamespacedName) error {
	var analysisTemplate rolloutsv1alpha1.AnalysisTemplate
	if err := r.Get(ctx, analysisTemplateNamespacedName, &analysisTemplate); err != nil {
		return err
	}

	analysisRun := rolloutsv1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", analysisTemplate.ObjectMeta.Name, time.Now().Unix()),
			Namespace: analysisTemplate.ObjectMeta.Namespace,
		},
		Spec: rolloutsv1alpha1.AnalysisRunSpec{
			Metric: analysisTemplate.Spec.Metric,
		},
	}

	if err := r.Client.Create(ctx, &analysisRun); err != nil {
		return err
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PlacementReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&rolloutsv1alpha1.Placement{}).
		Complete(r)
}
