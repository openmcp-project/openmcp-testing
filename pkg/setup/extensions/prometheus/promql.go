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

const (
	promtoolJobNamespace = defaultNamespace
	promtoolEndpoint     = "http://prometheus:9090"
)

type ObservabilityWindow struct {
	Start time.Time
	End   time.Time
}

// duration returns the window size as a Prometheus duration string (e.g. "900s").
func (w ObservabilityWindow) duration() string {
	return fmt.Sprintf("%ds", int(w.End.Sub(w.Start).Seconds()))
}

// NamedQuery is a PromQL query with a display name and description.
// The Query string may contain the placeholders $duration and $container which are replaced with values derived from the ObservabilityWindow passed to Query.
type NamedQuery struct {
	Name        string
	Description string
	Query       string
}

type QueryResult struct {
	Name   string
	Result model.Matrix
}

func (r QueryResult) String() string {
	var value string
	if r.Result.Len() > 0 {
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
		Description: "Average CPU consumption of the container expressed in fractional cores. Use to right-size CPU requests and limits.",
		Query:       `rate(container_cpu_usage_seconds_total{container="$container"}[$duration])`,
	}
	ContainerAverageMemoryUsage = NamedQuery{
		Name:        "average container memory usage (MiB)",
		Description: "Average working-set memory of the container in MiB. Use to righ-size memory requests and limits.",
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

type QueryParameters struct {
	// Container selector for kubelet metrics
	Container string
	// Controller selector for controller runtime metrics
	Controller string
	// JobName to identify the query job resource
	JobName string
	// Window to execute query range and compute duration value for step and rate duration
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
	return nil
}

type Option func(c *QueryParameters)

// Query runs all PromQL queries as range queries in a single promtool Job inside the cluster and prints the results.
func Query(ctx context.Context, cfg *envconf.Config, queries []NamedQuery, params QueryParameters, opts ...Option) ([]QueryResult, error) {
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
	jobsClient := clientset.BatchV1().Jobs(promtoolJobNamespace)
	job := promtoolJob(defaultVersion, queries, params)
	job.Name = params.JobName
	if _, err := jobsClient.Create(ctx, job, metav1.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("failed to create promtool job: %w", err)
	}
	if err := wait.For(conditions.New(cfg.Client().Resources()).JobCompleted(job)); err != nil {
		return nil, err
	}
	logs, err := jobLogs(ctx, clientset, job.Name)
	if err != nil {
		return nil, err
	}
	return parseLog(logs)
}

func jobLogs(ctx context.Context, clientset *kubernetes.Clientset, jobName string) (string, error) {
	pods, err := clientset.CoreV1().Pods(promtoolJobNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", jobName),
	})
	if err != nil {
		return "", fmt.Errorf("failed to list pods for job: %w", err)
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no pods found for job %q", jobName)
	}

	req := clientset.CoreV1().Pods(promtoolJobNamespace).GetLogs(pods.Items[0].Name, &corev1.PodLogOptions{})
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
func promtoolJob(version string, queries []NamedQuery, config QueryParameters) *batchv1.Job {
	image := defaultImage + ":" + version
	var sb strings.Builder
	for _, q := range queries {
		pql := strings.ReplaceAll(q.Query, "$duration", config.Window.duration())
		pql = strings.ReplaceAll(pql, "$container", config.Container)
		pql = strings.ReplaceAll(pql, "$controller", config.Controller)
		fmt.Fprintf(&sb, "echo '%s'\n", q.Name)
		fmt.Fprintf(&sb, "promtool query range %s %q -o json --step %s --start %d --end %d\n", promtoolEndpoint, pql, config.Window.duration(), config.Window.Start.Unix(), config.Window.End.Unix())
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: promtoolJobNamespace,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "promtool",
							Image:   image,
							Command: []string{"/bin/sh", "-c", sb.String()},
						},
					},
				},
			},
		},
	}
}
