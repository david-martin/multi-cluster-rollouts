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

// AnalysisPhase is the overall phase of an AnalysisRun, MetricResult, or Measurement
type AnalysisPhase string

// Possible AnalysisPhase values
const (
	AnalysisPhasePending      AnalysisPhase = "Pending"
	AnalysisPhaseRunning      AnalysisPhase = "Running"
	AnalysisPhaseSuccessful   AnalysisPhase = "Successful"
	AnalysisPhaseFailed       AnalysisPhase = "Failed"
	AnalysisPhaseError        AnalysisPhase = "Error"
	AnalysisPhaseInconclusive AnalysisPhase = "Inconclusive"
)

// Completed returns whether or not the analysis status is considered completed
func (as AnalysisPhase) Completed() bool {
	switch as {
	case AnalysisPhaseSuccessful, AnalysisPhaseFailed, AnalysisPhaseError, AnalysisPhaseInconclusive:
		return true
	}
	return false
}

// AnalysisRunSpec defines the desired state of AnalysisRun
type AnalysisRunSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Definition of a metric to analyse
	Metric Metric `json:"metric,omitempty"`
}

// AnalysisRunStatus is the status for a AnalysisRun resource
type AnalysisRunStatus struct {
	// Phase is the status of the analysis run
	Phase AnalysisPhase `json:"phase"`
	// Message is a message explaining current status
	Message string `json:"message,omitempty"`
	// MetricResults contains the metrics collected during the run
	MetricResults []MetricResult `json:"metricResults,omitempty"`
	// StartedAt indicates when the analysisRun first started
	StartedAt *metav1.Time `json:"startedAt,omitempty"`
	// RunSummary contains the final results from the metric executions
	RunSummary RunSummary `json:"runSummary,omitempty"`
	// DryRunSummary contains the final results from the metric executions in the dry-run mode
	DryRunSummary *RunSummary `json:"dryRunSummary,omitempty"`
}

// RunSummary contains the final results from the metric executions
type RunSummary struct {
	// This is equal to the sum of Successful, Failed, Inconclusive
	Count int32 `json:"count,omitempty" protobuf:"varint,1,opt,name=count"`
	// Successful is the number of times the metric was measured Successful
	Successful int32 `json:"successful,omitempty" protobuf:"varint,2,opt,name=successful"`
	// Failed is the number of times the metric was measured Failed
	Failed int32 `json:"failed,omitempty" protobuf:"varint,3,opt,name=failed"`
	// Inconclusive is the number of times the metric was measured Inconclusive
	Inconclusive int32 `json:"inconclusive,omitempty" protobuf:"varint,4,opt,name=inconclusive"`
	// Error is the number of times an error was encountered during measurement
	Error int32 `json:"error,omitempty" protobuf:"varint,5,opt,name=error"`
}

// MetricResult contain a list of the most recent measurements for a single metric along with
// counters on how often the measurement
type MetricResult struct {
	// Name is the name of the metric
	Name string `json:"name" protobuf:"bytes,1,opt,name=name"`
	// Phase is the overall aggregate status of the metric
	Phase AnalysisPhase `json:"phase" protobuf:"bytes,2,opt,name=phase,casttype=AnalysisPhase"`
	// Measurements holds the most recent measurements collected for the metric
	Measurements []Measurement `json:"measurements,omitempty" protobuf:"bytes,3,rep,name=measurements"`
	// Message contains a message describing current condition (e.g. error messages)
	Message string `json:"message,omitempty" protobuf:"bytes,4,opt,name=message"`
	// Count is the number of times the metric was measured without Error
	// This is equal to the sum of Successful, Failed, Inconclusive
	Count int32 `json:"count,omitempty" protobuf:"varint,5,opt,name=count"`
	// Successful is the number of times the metric was measured Successful
	Successful int32 `json:"successful,omitempty" protobuf:"varint,6,opt,name=successful"`
	// Failed is the number of times the metric was measured Failed
	Failed int32 `json:"failed,omitempty" protobuf:"varint,7,opt,name=failed"`
	// Inconclusive is the number of times the metric was measured Inconclusive
	Inconclusive int32 `json:"inconclusive,omitempty" protobuf:"varint,8,opt,name=inconclusive"`
	// Error is the number of times an error was encountered during measurement
	Error int32 `json:"error,omitempty" protobuf:"varint,9,opt,name=error"`
	// ConsecutiveError is the number of times an error was encountered during measurement in succession
	// Resets to zero when non-errors are encountered
	ConsecutiveError int32 `json:"consecutiveError,omitempty" protobuf:"varint,10,opt,name=consecutiveError"`
	// DryRun indicates whether this metric is running in a dry-run mode or not
	DryRun bool `json:"dryRun,omitempty" protobuf:"varint,11,opt,name=dryRun"`
	// Metadata stores additional metadata about this metric. It is used by different providers to store
	// the final state which gets used while taking measurements. For example, Prometheus uses this field
	// to store the final resolved query after substituting the template arguments.
	Metadata map[string]string `json:"metadata,omitempty" protobuf:"bytes,12,rep,name=metadata"`
}

// Measurement is a point in time result value of a single metric, and the time it was measured
type Measurement struct {
	// Phase is the status of this single measurement
	Phase AnalysisPhase `json:"phase" protobuf:"bytes,1,opt,name=phase,casttype=AnalysisPhase"`
	// Message contains a message describing current condition (e.g. error messages)
	Message string `json:"message,omitempty" protobuf:"bytes,2,opt,name=message"`
	// StartedAt is the timestamp in which this measurement started to be measured
	StartedAt *metav1.Time `json:"startedAt,omitempty" protobuf:"bytes,3,opt,name=startedAt"`
	// FinishedAt is the timestamp in which this measurement completed and value was collected
	FinishedAt *metav1.Time `json:"finishedAt,omitempty" protobuf:"bytes,4,opt,name=finishedAt"`
	// Value is the measured value of the metric
	Value string `json:"value,omitempty" protobuf:"bytes,5,opt,name=value"`
	// Metadata stores additional metadata about this metric result, used by the different providers
	// (e.g. kayenta run ID, job name)
	Metadata map[string]string `json:"metadata,omitempty" protobuf:"bytes,6,rep,name=metadata"`
	// ResumeAt is the  timestamp when the analysisRun should try to resume the measurement
	ResumeAt *metav1.Time `json:"resumeAt,omitempty" protobuf:"bytes,7,opt,name=resumeAt"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// AnalysisRun is the Schema for the analysisruns API
type AnalysisRun struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AnalysisRunSpec   `json:"spec,omitempty"`
	Status AnalysisRunStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// AnalysisRunList contains a list of AnalysisRun
type AnalysisRunList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AnalysisRun `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AnalysisRun{}, &AnalysisRunList{})
}
