package prometheus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/prometheus/common/model"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
)

// ObservabilityWindow defines the query range parameters start, end, step and optionally PromQL rate durations.
type ObservabilityWindow struct {
	// Start parameter passed to the promtool query range execution
	Start time.Time
	// End parameter passed to the promtool query range execution
	End time.Time
}

// duration returns the window size as a Prometheus duration string (e.g. "900s").
func (w ObservabilityWindow) duration() string {
	return fmt.Sprintf("%ds", int(w.End.Sub(w.Start).Seconds()))
}

// NamedQuery is a PromQL query with a display name and description.
// The Query string may contain the placeholders $duration and $container which are replaced with values derived from the ObservabilityWindow passed to Query.
type NamedQuery struct {
	// Name is used to identify a query.
	Name string
	// Description can be used in result reporting and generally describes the purpose of a query.
	Description string
	// Query is the PromQL query string.
	Query string
}

// QueryResult returnts the query name with its result.
type QueryResult struct {
	// Name from the NamedQuery to map a result back to a query.
	Name string
	// Result of the promtool range query execution.
	Result model.Matrix
}

// String returns a single-line summary for simplified reporting. It only shows the first value of the first stream.
// For more detailed reporting of multi-value query results, access the Result directly.
func (r QueryResult) String() string {
	var value string
	if r.Result.Len() > 0 && len(r.Result[0].Values) > 0 {
		value = r.Result[0].Values[0].Value.String()
	}
	return fmt.Sprintf("%s: %s", r.Name, value)
}

// Predefined queries. $duration, $container and $controller are replaced in Query with values from ObservabilityWindow.
var (
	// opencontrolplane-runtime based metrics
	OpenControlPlaneAverageTimeToCreate = NamedQuery{
		Name:        "average create time to ready (seconds)",
		Description: "Average time spent until a resource reaches state ready for the first time, including reconcile lag caused by invalid configuration or the controller not being available, etc.",
		Query:       "rate(opencontrolplane_runtime_resource_create_seconds_sum[$duration])/rate(opencontrolplane_runtime_resource_create_seconds_count[$duration])",
	}
	OpenControlPlaneAverageTimeToUpdate = NamedQuery{
		Name:        "average update time to ready (seconds)",
		Description: "Average time spent until a resource reaches state ready after spec changes, including reconcile lag caused by invalid configuration or the controller not being available, etc.",
		Query:       "rate(opencontrolplane_runtime_resource_update_seconds_sum[$duration])/rate(opencontrolplane_runtime_resource_update_seconds_count[$duration])",
	}
	// kubernetes (kubelet) metrics based queries
	ContainerAverageCPUUsage = NamedQuery{
		Name:        "average container cpu usage (cores)",
		Description: "Average CPU usage rate of the container expressed in fractional cores (time-averaged over the window). Use to right-size CPU requests and limits.",
		Query:       `rate(container_cpu_usage_seconds_total{container="$container"}[$duration])`,
	}
	ContainerAverageMemoryUsage = NamedQuery{
		Name:        "average container memory usage (MiB)",
		Description: "Average working-set memory of the container in MiB. Use to right-size memory requests and limits.",
		Query:       `avg_over_time(container_memory_working_set_bytes{container="$container"}[$duration]) / (1024*1024)`,
	}
	// controller-runtime metrics based queries
	ControllerRuntimeAverageReconcileTime = NamedQuery{
		Name:        "average reconcile time (seconds)",
		Description: "Average time spent inside the Reconcile function. Tracks overall reconcile performance (but may hide outliers).",
		Query:       `rate(controller_runtime_reconcile_time_seconds_sum{controller="$controller"}[$duration]) / rate(controller_runtime_reconcile_time_seconds_count{controller="$controller"}[$duration])`,
	}
	MaxWorkqueueDepth = NamedQuery{
		Name:        "max workqueue depth",
		Description: "Peak number of items waiting in the work queue over the window. The earliest leading indicator of a processing backlog.",
		Query:       `max_over_time(workqueue_depth{controller="$controller"}[$duration])`,
	}
)

// AllQueries contains all predefined queries.
var AllQueries = []NamedQuery{
	OpenControlPlaneAverageTimeToCreate,
	OpenControlPlaneAverageTimeToUpdate,
	ContainerAverageCPUUsage,
	ContainerAverageMemoryUsage,
	ControllerRuntimeAverageReconcileTime,
	MaxWorkqueueDepth,
}

// QueryParameters define promtool query range execution and optionally PromQL placeholders.
type QueryParameters struct {
	// Container selector for kubelet metrics.
	Container string
	// Controller selector for controller runtime metrics.
	Controller string
	// JobName to identify the query job resource. Has to be unique across Query calls within the current test context.
	JobName string
	// Window to execute query range and compute duration value for step and rate durations.
	Window ObservabilityWindow
}

func (p QueryParameters) validate() error {
	if p.JobName == "" {
		return errors.New("jobname must be provided")
	}
	if p.Window.Start.IsZero() {
		return errors.New("window start must be provided")
	}
	if p.Window.End.IsZero() {
		return errors.New("window end must be provided")
	}
	if p.Window.End.Before(p.Window.Start) {
		return errors.New("start must be before end")
	}
	return nil
}

type Option func(c *QueryParameters)

// Query runs all PromQL queries as range queries in a single promtool Job inside the cluster and prints the results.
func (p Prometheus) Query(ctx context.Context, cfg *envconf.Config, queries []NamedQuery, params QueryParameters, opts ...Option) ([]QueryResult, error) {
	for _, o := range opts {
		o(&params)
	}
	if err := params.validate(); err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(cfg.Client().RESTConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}
	klog.Infof("run query job (%s)", params.JobName)
	jobsClient := clientset.BatchV1().Jobs(p.Namespace)
	job := p.promtoolJob(queries, params)
	job.Name = params.JobName
	if _, err := jobsClient.Create(ctx, job, metav1.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("failed to create promtool job: %w", err)
	}
	if err := wait.For(conditions.New(cfg.Client().Resources()).JobCompleted(job)); err != nil {
		return nil, err
	}
	logs, err := p.jobLogs(ctx, clientset, job.Name)
	if err != nil {
		return nil, err
	}
	result, err := parseLog(logs)
	if err != nil {
		return nil, err
	}
	foreground := metav1.DeletePropagationForeground
	if err := jobsClient.Delete(ctx, job.Name, metav1.DeleteOptions{PropagationPolicy: &foreground}); err != nil {
		return result, err
	}
	return result, nil
}

func (p Prometheus) jobLogs(ctx context.Context, clientset *kubernetes.Clientset, jobName string) (string, error) {
	pods, err := clientset.CoreV1().Pods(p.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", jobName),
	})
	if err != nil {
		return "", fmt.Errorf("failed to list pods for job: %w", err)
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no pods found for job %q", jobName)
	}

	req := clientset.CoreV1().Pods(p.Namespace).GetLogs(pods.Items[0].Name, &corev1.PodLogOptions{})
	stream, err := req.Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to stream pod logs: %w", err)
	}
	defer stream.Close()

	logs, err := io.ReadAll(stream)
	if err != nil {
		return "", fmt.Errorf("failed to read pod logs: %w", err)
	}
	return string(logs), nil
}

func parseQueryResult(promtoolresult string) (*model.Matrix, error) {
	test := &model.Matrix{}
	if err := json.Unmarshal([]byte(promtoolresult), test); err != nil {
		return nil, err
	}
	return test, nil
}

func parseLog(log string) ([]QueryResult, error) {
	logLines := strings.Split(strings.TrimSpace(log), "\n")
	var result []QueryResult
	for i := 0; i < len(logLines); i += 2 {
		if i+1 >= len(logLines) {
			return nil, fmt.Errorf("unexpected log format: odd number of lines near %q", logLines[i])
		}
		queryName := logLines[i]
		queryResult, err := parseQueryResult(logLines[i+1])
		if err != nil {
			return nil, err
		}
		result = append(result, QueryResult{
			Name:   queryName,
			Result: *queryResult,
		})
	}
	return result, nil
}

// build job that executes each query
func (p Prometheus) promtoolJob(queries []NamedQuery, config QueryParameters) *batchv1.Job {
	var sb strings.Builder
	for _, q := range queries {
		pql := strings.ReplaceAll(q.Query, "$duration", config.Window.duration())
		pql = strings.ReplaceAll(pql, "$container", config.Container)
		pql = strings.ReplaceAll(pql, "$controller", config.Controller)
		// escape any remaining $ so /bin/sh does not expand them as variables or command substitutions
		pql = strings.ReplaceAll(pql, "$", `\$`)
		fmt.Fprintf(&sb, "printf '%%s\\n' %q\n", q.Name)
		fmt.Fprintf(&sb, "promtool query range %s %q -o json --step %s --start %d --end %d\n", defaultEndpoint, pql, config.Window.duration(), config.Window.Start.Unix(), config.Window.End.Unix())
	}
	return &batchv1.Job{
		Namespace: p.Namespace,
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "promtool",
							Image:   p.Image,
							Command: []string{"/bin/sh", "-c", sb.String()},
						},
					},
				},
			},
		},
	}
}
