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
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/strings/slices"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

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

	specClusters := placement.Spec.Clusters
	var statusDecisions []string
	for _, decision := range placement.Status.Decisions {
		statusDecisions = append(statusDecisions, decision.ClusterName)
	}

	// Mark these for removal in decisions
	clustersToRemove := []string{}
	for _, cluster := range statusDecisions {
		if !slices.Contains(specClusters, cluster) {
			clustersToRemove = append(clustersToRemove, cluster)
		}
	}

	updatedDecisions := []rolloutsv1alpha1.ClusterDecision{}
	for _, decision := range placement.Status.Decisions {
		if !slices.Contains(clustersToRemove, decision.ClusterName) {
			// keep existing decisions that are not being removed
			if decision.PendingRemoval != "" {
				// but unset pendingRemoval in case it was transitioned
				// back before finishing being removed
				decision.PendingRemoval = ""
				// and create a new pendingReady AnalysisRun
				if placement.Spec.ReadyAnalysis != "" {
					log.Info("Creating ready AnalysisRun", "cluster", decision.ClusterName, "analysisTemplate", placement.Spec.ReadyAnalysis)
					analysisTemplateNamespacedName := types.NamespacedName{
						Namespace: placement.Namespace,
						Name:      placement.Spec.ReadyAnalysis,
					}
					analysisRunName, err := r.CreateAnalysisRun(ctx, analysisTemplateNamespacedName, placement.Name)
					if err != nil {
						log.Error(err, "unable to create AnalysisRun")
						return ctrl.Result{}, err
					}
					decision.PendingReady = analysisRunName
				}
			}
			updatedDecisions = append(updatedDecisions, decision)
		} else {
			// mark decision for removal
			removalDecision := rolloutsv1alpha1.ClusterDecision{
				ClusterName: decision.ClusterName,
			}
			// if there's already a pendingRemoval, check if the analysisrun is successful
			if decision.PendingRemoval != "" {
				var pendingRemovalAnalysisRun rolloutsv1alpha1.AnalysisRun
				analysisRunNamespacedName := types.NamespacedName{
					Name:      decision.PendingRemoval,
					Namespace: placement.Namespace,
				}
				if err := r.Get(ctx, analysisRunNamespacedName, &pendingRemovalAnalysisRun); err != nil {
					log.Error(err, "unable to fetch pendingRemoval AnalysisRun. Removing reference from Placement", "analysisRunNamespacedName", analysisRunNamespacedName, "placement", placement.Name)
				} else {
					// TODO: Check for 'Completed' sufficient here i.e. if it failed the analysis run after count max reach, OK to delete?
					//       For now, only remove the reference if the analysis run was successful
					if pendingRemovalAnalysisRun.Status.Phase == rolloutsv1alpha1.AnalysisPhaseSuccessful {
						log.Info("PendingRemoval AnalysisRun successful. Removing decision", "cluster", decision.ClusterName)
						continue
					} else {
						// AnalysisRun is pending/not successful, keep the reference for now
						removalDecision.PendingRemoval = decision.PendingRemoval
					}
				}
			} else if placement.Spec.RemoveAnalysis != "" {
				log.Info("Creating removal AnalysisRun", "cluster", decision.ClusterName, "analysisTemplate", placement.Spec.RemoveAnalysis)
				analysisTemplateNamespacedName := types.NamespacedName{
					Namespace: placement.Namespace,
					Name:      placement.Spec.RemoveAnalysis,
				}
				analysisRunName, err := r.CreateAnalysisRun(ctx, analysisTemplateNamespacedName, placement.Name)
				if err != nil {
					log.Error(err, "unable to create AnalysisRun")
					return ctrl.Result{}, err
				}
				removalDecision.PendingRemoval = analysisRunName
			}
			updatedDecisions = append(updatedDecisions, removalDecision)
		}
	}

	// Add new decisions
	for _, cluster := range specClusters {
		if !slices.Contains(statusDecisions, cluster) {
			newClusterDecision := rolloutsv1alpha1.ClusterDecision{
				ClusterName: cluster,
			}
			if placement.Spec.ReadyAnalysis != "" {
				log.Info("Creating ready AnalysisRun", "cluster", cluster, "analysisTemplate", placement.Spec.ReadyAnalysis)
				analysisTemplateNamespacedName := types.NamespacedName{
					Namespace: placement.Namespace,
					Name:      placement.Spec.ReadyAnalysis,
				}
				analysisRunName, err := r.CreateAnalysisRun(ctx, analysisTemplateNamespacedName, placement.Name)
				if err != nil {
					log.Error(err, "unable to create AnalysisRun")
					return ctrl.Result{}, err
				}
				newClusterDecision.PendingReady = analysisRunName
			}
			updatedDecisions = append(updatedDecisions, newClusterDecision)
		}
	}

	placement.Status = rolloutsv1alpha1.PlacementStatus{
		Decisions: updatedDecisions,
	}

	log.Info("Updating Placement status", "decisions", placement.Status.Decisions)
	if err := r.Status().Update(ctx, &placement); err != nil {
		log.Error(err, "unable to update Placement status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *PlacementReconciler) CreateAnalysisRun(ctx context.Context, analysisTemplateNamespacedName types.NamespacedName, placementName string) (string, error) {
	var analysisTemplate rolloutsv1alpha1.AnalysisTemplate
	if err := r.Get(ctx, analysisTemplateNamespacedName, &analysisTemplate); err != nil {
		return "", err
	}

	analysisRun := rolloutsv1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", analysisTemplate.Name, time.Now().Unix()),
			Namespace: analysisTemplate.Namespace,
			Annotations: map[string]string{
				"rollouts.example.com/placement": placementName,
			},
		},
		Spec: rolloutsv1alpha1.AnalysisRunSpec{
			Metric: analysisTemplate.Spec.Metric,
		},
	}

	if err := r.Client.Create(ctx, &analysisRun); err != nil {
		return "", err
	}
	return analysisRun.Name, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PlacementReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&rolloutsv1alpha1.Placement{}).
		Watches(
			&source.Kind{Type: &rolloutsv1alpha1.AnalysisRun{}},
			handler.Funcs{
				UpdateFunc: func(ue event.UpdateEvent, rli workqueue.RateLimitingInterface) {
					analysisRun := ue.ObjectNew.(*rolloutsv1alpha1.AnalysisRun)
					log.Log.Info("placement_controller watch update of AnalysisRun", "analysisRun", analysisRun.Name)
					if analysisRun.Status.Phase.Completed() {
						log.Log.Info("placement_controller watch update of AnalysisRun with phase Completed. Enqueueing Placement", "analysisRun", analysisRun.Name, "phase", analysisRun.Status.Phase, "placement", analysisRun.Annotations["rollouts.example.com/placement"])
						rli.Add(reconcile.Request{
							NamespacedName: types.NamespacedName{
								Name:      analysisRun.Annotations["rollouts.example.com/placement"],
								Namespace: analysisRun.Namespace,
							},
						})
					}
				},
			}).
		Complete(r)
}
