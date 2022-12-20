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
	log.Info("Reconcile AnalysisRun", "analysisRun", analysisRun)

	if analysisRun.Status.Phase.Completed() {
		return ctrl.Result{}, nil
	}
	// There is no pending or running phase at this time.
	// Execute query and evalaate success condition here
	status, err := r.ExecuteAndEvaluate(ctx, analysisRun.Spec.Metric)
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

func (r *AnalysisRunReconciler) ExecuteAndEvaluate(ctx context.Context, metric rolloutsv1alpha1.Metric) (rolloutsv1alpha1.AnalysisRunStatus, error) {
	status := rolloutsv1alpha1.AnalysisRunStatus{
		StartedAt: &metav1.Time{Time: time.Now()},
	}

	// Only try once, for now
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
	measurement.Value = newValue

	measurement.FinishedAt = &metav1.Time{Time: time.Now()}
	measurement.Phase = newStatus
	measurements := []rolloutsv1alpha1.Measurement{
		measurement,
	}
	metricResult := rolloutsv1alpha1.MetricResult{
		Phase:        newStatus,
		Count:        1,
		Measurements: measurements,
		Successful:   1,
	}

	/*
		status:
		  metricResults:
		  - count: 1
		    measurements:
		    - finishedAt: "2021-09-08T19:15:49Z"
		      phase: Successful
		      startedAt: "2021-09-08T19:15:49Z"
		      value: '[]'
		    name: success-rate
		    phase: Successful
		    successful: 1
		  phase: Successful
		  startedAt:  "2021-09-08T19:15:49Z"
	*/
	status.MetricResults = []rolloutsv1alpha1.MetricResult{
		metricResult,
	}
	status.Phase = rolloutsv1alpha1.AnalysisPhaseSuccessful
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
