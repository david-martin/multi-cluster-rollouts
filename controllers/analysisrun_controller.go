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
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	// prometheusapi "github.com/prometheus/client_golang/api"
	// prometheusv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	// prometheusmodel "github.com/prometheus/common/model"
	"github.com/antonmedv/expr"
	"github.com/antonmedv/expr/file"
	prometheusapi "github.com/prometheus/client_golang/api"
	prometheusv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	prometheusmodel "github.com/prometheus/common/model"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	rolloutsv1alpha1 "github.com/david-martin/multi-cluster-rollouts/api/v1alpha1"
)

// AnalysisRunReconciler reconciles a AnalysisRun object
type AnalysisRunReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=rollouts.example.com,resources=analysisruns,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rollouts.example.com,resources=analysisruns/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=rollouts.example.com,resources=analysisruns/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the AnalysisRun object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.1/pkg/reconcile
func (r *AnalysisRunReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the AnalysisRun
	var analysisRun rolloutsv1alpha1.AnalysisRun
	if err := r.Get(ctx, req.NamespacedName, &analysisRun); err != nil {
		log.Error(err, "unable to fetch AnalysisRun")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	// log.Info("Reconcile AnalysisRun", "analysisRun", analysisRun)
	log.Info("Reconcile AnalysisRun", "analysisRun", analysisRun.ObjectMeta.Name)

	if analysisRun.Status.Phase.Completed() {
		log.Info("AnalysisRun already complete. Ignoring", "analysisRun", analysisRun.ObjectMeta.Name)
		return ctrl.Result{}, nil
	}

	// Reached hardcoded limit of 5 attempts
	if analysisRun.Status.MetricResults != nil && len(analysisRun.Status.MetricResults) > 0 && analysisRun.Status.MetricResults[0].Count >= 5 {
		log.Info("AnalysisRun reached count limit. Ignoring", "analysisRun", analysisRun.ObjectMeta.Name, "count", analysisRun.Status.MetricResults[0].Count)
		return ctrl.Result{}, nil
	}

	if len(analysisRun.Status.MetricResults) > 0 {
		// check if we should defer measurement as it's too soon since previous
		previousMeasurement := analysisRun.Status.MetricResults[0].Measurements[len(analysisRun.Status.MetricResults[0].Measurements)-1]
		waitTime := previousMeasurement.FinishedAt.Add(5 * time.Second)
		currentTime := time.Now()
		if currentTime.Before(waitTime) {
			requeueAfter := waitTime.Sub(currentTime)
			log.Info("Not enough time elapsed since previous AnalysisRun measurement taken. Requeueing", "analysisRun", analysisRun.ObjectMeta.Name, "requeueAfter", requeueAfter)
			return ctrl.Result{RequeueAfter: requeueAfter}, nil
		}
	}

	// Execute query and evalaate success condition here
	status, err := r.ExecuteAndEvaluate(ctx, analysisRun.Spec.Metric, analysisRun.Status)
	if err != nil {
		log.Error(err, "Error executing and evaluating")
		return ctrl.Result{}, err
	}
	analysisRun.Status = status

	if err := r.Status().Update(ctx, &analysisRun); err != nil {
		log.Error(err, "unable to update AnalysisRun status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *AnalysisRunReconciler) ExecuteAndEvaluate(ctx context.Context, metric rolloutsv1alpha1.Metric, existingStatus rolloutsv1alpha1.AnalysisRunStatus) (rolloutsv1alpha1.AnalysisRunStatus, error) {
	startedAt := existingStatus.StartedAt
	if startedAt == nil {
		startedAt = &metav1.Time{Time: time.Now()}
	}
	status := rolloutsv1alpha1.AnalysisRunStatus{
		StartedAt: startedAt,
	}

	// Take a new measurement
	measurement := rolloutsv1alpha1.Measurement{
		StartedAt: &metav1.Time{Time: time.Now()},
	}

	// execute query
	client, err := prometheusapi.NewClient(prometheusapi.Config{
		Address: metric.Provider.Prometheus.Address,
	})
	if err != nil {
		return rolloutsv1alpha1.AnalysisRunStatus{}, err
	}
	api := prometheusv1.NewAPI(client)
	response, _, err := api.Query(ctx, metric.Provider.Prometheus.Query, time.Now())
	if err != nil {
		return rolloutsv1alpha1.AnalysisRunStatus{}, err
	}
	newValue, newStatus, err := r.processResponse(metric, response)
	log.Log.Info("Prometheus response", "newValue", newValue, "newStatus", newStatus)
	if err != nil {
		log.Log.Error(err, "Error processing Prometheus response")
	}
	measurement.Value = newValue

	measurement.FinishedAt = &metav1.Time{Time: time.Now()}
	measurement.Phase = newStatus
	// TODO: Refactor existingStatus.MetricResults checks duplication
	var measurements []rolloutsv1alpha1.Measurement
	if len(existingStatus.MetricResults) > 0 {
		measurements = existingStatus.MetricResults[0].Measurements
	} else {
		measurements = []rolloutsv1alpha1.Measurement{}
	}
	measurements = append(measurements, measurement)
	var existingCount int32
	if len(existingStatus.MetricResults) > 0 {
		existingCount = existingStatus.MetricResults[0].Count
	} else {
		existingCount = 0
	}
	successful := int32(0)
	if newStatus == rolloutsv1alpha1.AnalysisPhaseSuccessful {
		// TODO: Figure out in what scenarios this would be incremented
		successful = 1
	}
	metricResult := rolloutsv1alpha1.MetricResult{
		Phase:        newStatus,
		Measurements: measurements,
		Successful:   successful,
		Count:        existingCount + 1,
	}

	// Only work with 1 MetricResult (with potentially mulitple measurements) at this time
	status.MetricResults = []rolloutsv1alpha1.MetricResult{
		metricResult,
	}

	// Simplistic approach to overall status check
	if newStatus == rolloutsv1alpha1.AnalysisPhaseSuccessful {
		status.Phase = newStatus
	} else {
		// TODO: Update overall phase to failed if max count is reached
		status.Phase = rolloutsv1alpha1.AnalysisPhasePending
	}

	return status, nil
}

func (p *AnalysisRunReconciler) processResponse(metric rolloutsv1alpha1.Metric, response prometheusmodel.Value) (string, rolloutsv1alpha1.AnalysisPhase, error) {
	switch value := response.(type) {
	case *prometheusmodel.Scalar:
		valueStr := value.Value.String()
		result := float64(value.Value)
		newStatus, err := evaluateResult(result, metric)
		return valueStr, newStatus, err
	case prometheusmodel.Vector:
		results := make([]float64, 0, len(value))
		valueStr := "["
		for _, s := range value {
			if s != nil {
				valueStr = valueStr + s.Value.String() + ","
				results = append(results, float64(s.Value))
			}
		}
		// if we appended to the string, we should remove the last comma on the string
		if len(valueStr) > 1 {
			valueStr = valueStr[:len(valueStr)-1]
		}
		valueStr = valueStr + "]"
		newStatus, err := evaluateResult(results, metric)
		return valueStr, newStatus, err
	default:
		return "", rolloutsv1alpha1.AnalysisPhaseError, fmt.Errorf("Prometheus metric type not supported")
	}
}

func evaluateResult(result interface{}, metric rolloutsv1alpha1.Metric) (rolloutsv1alpha1.AnalysisPhase, error) {
	successCondition := false
	failCondition := false
	var err error

	if metric.SuccessCondition != "" {
		successCondition, err = EvalCondition(result, metric.SuccessCondition)
		if err != nil {
			return rolloutsv1alpha1.AnalysisPhaseError, err
		}
	}
	// if metric.FailureCondition != "" {
	// 	failCondition, err = EvalCondition(result, metric.FailureCondition)
	// 	if err != nil {
	// 		return rolloutsv1alpha1.AnalysisPhaseError, err
	// 	}
	// }

	switch {
	case metric.SuccessCondition == "": // && metric.FailureCondition == "":
		//Always return success unless there is an error
		return rolloutsv1alpha1.AnalysisPhaseSuccessful, nil
	case metric.SuccessCondition != "": // && metric.FailureCondition == "":
		// Without a failure condition, a measurement is considered a failure if the measurement's success condition is not true
		failCondition = !successCondition
	case metric.SuccessCondition == "": // && metric.FailureCondition != "":
		// Without a success condition, a measurement is considered a successful if the measurement's failure condition is not true
		successCondition = !failCondition
	}

	if failCondition {
		return rolloutsv1alpha1.AnalysisPhaseFailed, nil
	}

	if !failCondition && !successCondition {
		return rolloutsv1alpha1.AnalysisPhaseInconclusive, nil
	}

	// If we reach this code path, failCondition is false and successCondition is true
	return rolloutsv1alpha1.AnalysisPhaseSuccessful, nil
}

// EvalCondition evaluates the condition with the resultValue as an input
func EvalCondition(resultValue interface{}, condition string) (bool, error) {
	var err error

	env := map[string]interface{}{
		"result":  valueFromPointer(resultValue),
		"asInt":   asInt,
		"asFloat": asFloat,
		"isNaN":   math.IsNaN,
		"isInf":   isInf,
		"isNil":   isNilFunc(resultValue),
		"default": defaultFunc(resultValue),
	}

	unwrapFileErr := func(e error) error {
		if fileErr, ok := err.(*file.Error); ok {
			e = errors.New(fileErr.Message)
		}
		return e
	}

	program, err := expr.Compile(condition, expr.Env(env))
	if err != nil {
		return false, unwrapFileErr(err)
	}

	output, err := expr.Run(program, env)
	if err != nil {
		return false, unwrapFileErr(err)
	}

	switch val := output.(type) {
	case bool:
		return val, nil
	default:
		return false, fmt.Errorf("expected bool, but got %T", val)
	}
}

func isInf(f float64) bool {
	return math.IsInf(f, 0)
}

func asInt(in interface{}) int64 {
	switch i := in.(type) {
	case float64:
		return int64(i)
	case float32:
		return int64(i)
	case int64:
		return i
	case int32:
		return int64(i)
	case int16:
		return int64(i)
	case int8:
		return int64(i)
	case int:
		return int64(i)
	case uint64:
		return int64(i)
	case uint32:
		return int64(i)
	case uint16:
		return int64(i)
	case uint8:
		return int64(i)
	case uint:
		return int64(i)
	case string:
		inAsInt, err := strconv.ParseInt(i, 10, 64)
		if err == nil {
			return inAsInt
		}
		panic(err)
	}
	panic(fmt.Sprintf("asInt() not supported on %v %v", reflect.TypeOf(in), in))
}

func asFloat(in interface{}) float64 {
	switch i := in.(type) {
	case float64:
		return i
	case float32:
		return float64(i)
	case int64:
		return float64(i)
	case int32:
		return float64(i)
	case int16:
		return float64(i)
	case int8:
		return float64(i)
	case int:
		return float64(i)
	case uint64:
		return float64(i)
	case uint32:
		return float64(i)
	case uint16:
		return float64(i)
	case uint8:
		return float64(i)
	case uint:
		return float64(i)
	case string:
		inAsFloat, err := strconv.ParseFloat(i, 64)
		if err == nil {
			return inAsFloat
		}
		panic(err)
	}
	panic(fmt.Sprintf("asFloat() not supported on %v %v", reflect.TypeOf(in), in))
}

// Check whether two slices of type string are equal or not.
// func Equal(a, b []string) bool {
// 	if len(a) != len(b) {
// 		return false
// 	}
// 	left := make(map[string]bool)
// 	for _, x := range a {
// 		left[x] = true
// 	}
// 	for _, x := range b {
// 		if !left[x] {
// 			return false
// 		}
// 	}
// 	return true
// }

func defaultFunc(resultValue interface{}) func(interface{}, interface{}) interface{} {
	return func(_ interface{}, defaultValue interface{}) interface{} {
		if isNil(resultValue) {
			return defaultValue
		}
		return valueFromPointer(resultValue)
	}
}

func isNilFunc(resultValue interface{}) func(interface{}) bool {
	return func(_ interface{}) bool {
		return isNil(resultValue)
	}
}

// isNil is courtesy of: https://gist.github.com/mangatmodi/06946f937cbff24788fa1d9f94b6b138
func isNil(in interface{}) (out bool) {
	if in == nil {
		out = true
		return
	}

	switch reflect.TypeOf(in).Kind() {
	case reflect.Ptr, reflect.Map, reflect.Array, reflect.Chan, reflect.Slice:
		out = reflect.ValueOf(in).IsNil()
	}

	return
}

// valueFromPointer allows pointers to be passed in from the provider, but then extracts the value from
// the pointer if the pointer is not nil, else returns nil
func valueFromPointer(in interface{}) (out interface{}) {
	if isNil(in) {
		return
	}

	if reflect.TypeOf(in).Kind() != reflect.Ptr {
		out = in
		return
	}

	out = reflect.ValueOf(in).Elem().Interface()
	return
}

// SetupWithManager sets up the controller with the Manager.
func (r *AnalysisRunReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&rolloutsv1alpha1.AnalysisRun{}).
		Complete(r)
}
