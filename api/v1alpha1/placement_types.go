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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// PlacementSpec defines the desired state of Placement
type PlacementSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Static list of ArgoCD clusters
	Clusters []string `json:"clusters,omitempty"`

	// TODO: reference to object intead of object name?
	// Name of AnalysisTemplate for analysing if the placement of an Application is ready
	RemoveAnalysis string `json:"removeAnalysis,omitempty"`

	// Name of AnalysisTemplate for analysing if the placement of an Application is ready
	ReadyAnalysis string `json:"readyAnalysis,omitempty"`
}

// PlacementStatus defines the observed state of Placement
type PlacementStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Decisions is a slice of decisions according to a placement
	// +kubebuilder:validation:Required
	// +required
	Decisions []ClusterDecision `json:"decisions"`
}

// ClusterDecision represents a decision from a placement
// An empty ClusterDecision indicates it is not scheduled yet.
type ClusterDecision struct {
	// ClusterName is the name of the ArgoCD cluster.
	// +kubebuilder:validation:Required
	// +required
	ClusterName string `json:"clusterName"`

	// Name of AnalysisRun that should be successful
	// before removing this cluster decision
	PendingRemoval string `json:"pendingRemoval,omitempty"`

	// Name of AnalysisRun that should be successful
	// before this cluster decision is deemed 'ready'
	PendingReady string `json:"pendingReady,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Placement is the Schema for the placements API
type Placement struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PlacementSpec   `json:"spec,omitempty"`
	Status PlacementStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// PlacementList contains a list of Placement
type PlacementList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Placement `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Placement{}, &PlacementList{})
}
