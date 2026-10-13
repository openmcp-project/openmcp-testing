package prometheus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/prometheus/common/model"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
)

const (
	promtoolJobName      = "promtool-query"
	promtoolJobNamespace = defaultNamespace
	promtoolEndpoint     = "http://prometheus:9090"
)

type ObservabilityWindow struct {
	Start   time.Time
	End     time.Time
	PodName string
}

// duration returns the window size as a Prometheus duration string (e.g. "900s").
func (w ObservabilityWindow) duration() string {
	return fmt.Sprintf("%ds", int(w.End.Sub(w.Start).Seconds()))
}

// NamedQuery is a PromQL query with a display name and description.
// The Query string may contain the placeholders $window and $pod which are replaced with values derived from the ObservabilityWindow passed to Query.
type NamedQuery struct {
	Name        string
	Description string
	Query       string
}

type QueryResult struct {
	Name   string
	Result model.Vector
}

func (r QueryResult) String() string {
	var value string
	if r.Result.Len() > 0 {
		value = r.Result[0].Value.String()
	}
	return fmt.Sprintf("%s: %s", r.Name, value)
}

// Predefined queries. $window and $pod are replaced in Query with values from ObservabilityWindow.
var (
	QueryMeanTimeToCreate = NamedQuery{
		Name:        "average create time to ready",
		Description: "Average time spent until a resource reaches state ready for the first time, including reconcile lag caused by invalid configuration or the controller not being available, etc.",
		Query:       "rate(opencontrolplane_runtime_resource_create_seconds_sum[$window])/rate(opencontrolplane_runtime_resource_create_seconds_count[$window])",
	}
	QueryMeanTimeToUpdate = NamedQuery{
		Name:        "average update time to ready",
		Description: "Average time spent until a resource reaches state ready after spec changes, including reconcile lag caused by invalid configuration or the controller not being available, etc.",
		Query:       "rate(opencontrolplane_runtime_resource_update_seconds_sum[$window])/rate(opencontrolplane_runtime_resource_update_seconds_count[$window])",
	}
	QueryAveragePodCPU = NamedQuery{
		Name:        "average pod cpu usage (cores)",
		Description: "Average CPU consumption of the pod over the observation window, expressed in fractional cores. Use to right-size CPU requests and spot sustained saturation.",
		Query:       `avg_over_time(rate(pod_cpu_usage_seconds_total{pod="$pod"}[2m])[$window:30s])`,
	}
	QueryAveragePodMemoryMiB = NamedQuery{
		Name:        "average pod memory usage (MiB)",
		Description: "Average working-set memory of the pod over the observation window in MiB. Working-set excludes reclaimable page cache and is the value kubelet compares against the memory limit.",
		Query:       `avg_over_time(pod_memory_working_set_bytes{pod="$pod"}[$window]) / (1024*1024)`,
	}
	QueryAverageReconcileSeconds = NamedQuery{
		Name:        "average reconcile time (seconds)",
		Description: "Mean time spent inside the Reconcile function per controller over the window. Tracks overall reconcile performance; pair with p99 to detect outliers hidden by the average.",
		Query:       `rate(controller_runtime_reconcile_time_seconds_sum{pod="$pod"}[$window]) / rate(controller_runtime_reconcile_time_seconds_count{pod="$pod"}[$window])`,
	}
	QueryTotalReconcileErrors = NamedQuery{
		Name:        "total reconcile errors",
		Description: "Cumulative count of reconcile iterations that returned a non-nil error and triggered a requeue. A rising value indicates persistent failures; correlate with QueryReconcileErrorRate for rate context.",
		Query:       `sum(increase(controller_runtime_reconcile_errors_total{pod="$pod"}[$window]))`,
	}
	QueryReconcileRate = NamedQuery{
		Name:        "reconcile rate (ops/sec)",
		Description: "Rate of reconcile iterations completed per second, broken down by controller. Establishes the throughput baseline; a sudden drop may indicate a stuck worker or API server pressure.",
		Query:       `sum(rate(controller_runtime_reconcile_total{pod="$pod"}[$window])) by (controller)`,
	}
	QueryReconcileErrorRate = NamedQuery{
		Name:        "reconcile error rate (%)",
		Description: "Percentage of reconcile iterations that failed, per controller. More actionable than raw error counts because it normalises against throughput — a high rate at low volume is more alarming than a low rate at high volume.",
		Query:       `100 * sum(rate(controller_runtime_reconcile_errors_total{pod="$pod"}[$window])) by (controller) / sum(rate(controller_runtime_reconcile_total{pod="$pod"}[$window])) by (controller)`,
	}
	QueryReconcileP99 = NamedQuery{
		Name:        "reconcile time p99 (seconds)",
		Description: "99th-percentile reconcile duration per controller. Reveals tail latency caused by a single slow resource type or a flapping object that the average reconcile time would mask.",
		Query:       `histogram_quantile(0.99, sum(rate(controller_runtime_reconcile_time_seconds_bucket{pod="$pod"}[$window])) by (le, controller))`,
	}
	QueryWorkqueueDepth = NamedQuery{
		Name:        "workqueue depth",
		Description: "Peak number of items waiting in the work queue over the window. The earliest leading indicator of a processing backlog — rises before reconcile latency or error rate does.",
		Query:       `max_over_time(workqueue_depth{pod="$pod"}[$window])`,
	}
	QueryWorkqueueQueueLatencyP99 = NamedQuery{
		Name:        "workqueue queue latency p99 (seconds)",
		Description: "99th-percentile time an item spent waiting in the queue before a worker picked it up. A high value relative to process latency means workers are saturated; a low value means the bottleneck is inside Reconcile itself.",
		Query:       `histogram_quantile(0.99, sum(rate(workqueue_queue_duration_seconds_bucket{pod="$pod"}[$window])) by (le, name))`,
	}
	QueryWorkqueueProcessLatencyP99 = NamedQuery{
		Name:        "workqueue process latency p99 (seconds)",
		Description: "99th-percentile time spent actively processing an item (i.e. inside the handler). Isolates slow reconcile logic from queue scheduling delays.",
		Query:       `histogram_quantile(0.99, sum(rate(workqueue_work_duration_seconds_bucket{pod="$pod"}[$window])) by (le, name))`,
	}
	QueryWorkqueueRetries = NamedQuery{
		Name:        "total workqueue retries",
		Description: "Total requeue count per queue over the window. A high retry count relative to reconcile rate indicates objects are failing repeatedly; can reveal requeue storms before they surface as cascading errors.",
		Query:       `sum(increase(workqueue_retries_total{pod="$pod"}[$window])) by (name)`,
	}
	QueryWorkerUtilization = NamedQuery{
		Name:        "worker utilization (%)",
		Description: "Ratio of active workers to the configured MaxConcurrentReconciles limit. Sustained values near 100% mean the controller is goroutine-bound and MaxConcurrentReconciles should be increased.",
		Query:       `100 * avg_over_time(controller_runtime_active_workers{pod="$pod"}[$window]) / controller_runtime_max_concurrent_reconciles{pod="$pod"}`,
	}
	QueryAPIServerRequestRate = NamedQuery{
		Name:        "api server request rate (req/sec)",
		Description: "Rate of outbound API server requests broken down by HTTP method and status code. A spike in 429 (TooManyRequests) responses indicates the controller is being throttled by the API server rate limiter.",
		Query:       `sum(rate(rest_client_requests_total{pod="$pod"}[$window])) by (method, code)`,
	}
	// cAdvisor-backed metrics (requires ScrapeKubelet: true)
	QueryCPUThrottleRate = NamedQuery{
		Name:        "cpu throttle rate (%)",
		Description: "Percentage of CPU time the container was throttled by the kernel cgroup, relative to total CPU usage. Throttling is invisible to the process but directly slows reconcile loops; a non-zero value means the CPU limit is too tight.",
		Query:       `100 * sum(increase(container_cpu_throttled_seconds_total{pod="$pod"}[$window])) / sum(increase(container_cpu_usage_seconds_total{pod="$pod"}[$window]))`,
	}
	QueryMemoryLimitUtilization = NamedQuery{
		Name:        "memory limit utilization (%)",
		Description: "Working-set memory as a percentage of the container memory limit. Values consistently above 80% indicate OOMKill risk; the pod will be killed with no warning once it reaches 100%.",
		Query:       `100 * avg_over_time(container_memory_working_set_bytes{pod="$pod", container!=""}[$window]) / avg_over_time(container_spec_memory_limit_bytes{pod="$pod", container!=""}[$window])`,
	}
	QueryNetworkReceiveBytesRate = NamedQuery{
		Name:        "network receive rate (bytes/sec)",
		Description: "Inbound network throughput of the pod per interface. Useful for controllers that perform heavy watch or list operations against the API server or external services.",
		Query:       `sum(rate(container_network_receive_bytes_total{pod="$pod"}[$window])) by (interface)`,
	}
	QueryNetworkTransmitBytesRate = NamedQuery{
		Name:        "network transmit rate (bytes/sec)",
		Description: "Outbound network throughput of the pod per interface. Useful for controllers that push large payloads (e.g. status updates, events) or communicate with external APIs at high frequency.",
		Query:       `sum(rate(container_network_transmit_bytes_total{pod="$pod"}[$window])) by (interface)`,
	}
)

// AllQueries contains all predefined queries.
var AllQueries = []NamedQuery{
	QueryMeanTimeToCreate,
	QueryMeanTimeToUpdate,
	QueryAveragePodCPU,
	QueryAveragePodMemoryMiB,
	QueryAverageReconcileSeconds,
	QueryTotalReconcileErrors,
	QueryReconcileRate,
	QueryReconcileErrorRate,
	QueryReconcileP99,
	QueryWorkqueueDepth,
	QueryWorkqueueQueueLatencyP99,
	QueryWorkqueueProcessLatencyP99,
	QueryWorkqueueRetries,
	QueryWorkerUtilization,
	QueryAPIServerRequestRate,
	QueryCPUThrottleRate,
	QueryMemoryLimitUtilization,
	QueryNetworkReceiveBytesRate,
	QueryNetworkTransmitBytesRate,
}

type QueryConfig struct {
	prometheusVersion string
	queries           []NamedQuery
}

type Option func(c *QueryConfig)

// TODO
func NewQueryConfig(opts ...Option) *QueryConfig {
	cfg := &QueryConfig{
		prometheusVersion: defaultVersion,
		queries:           AllQueries,
	}
	for _, o := range opts {
		o(cfg)
	}
	return cfg
}

// Query runs all PromQL queries as instant queries in a single promtool Job inside the cluster and prints the results.
func Query(ctx context.Context, cfg *envconf.Config, queries []NamedQuery, window ObservabilityWindow, version string) ([]QueryResult, error) {
	if version == "" {
		version = defaultVersion
	}
	clientset, err := kubernetes.NewForConfig(cfg.Client().RESTConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}
	jobsClient := clientset.BatchV1().Jobs(promtoolJobNamespace)
	job := promtoolJob(version, queries, window)
	if _, err := jobsClient.Create(ctx, job, metav1.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("failed to create promtool job: %w", err)
	}
	if err := wait.For(conditions.New(cfg.Client().Resources()).JobCompleted(job)); err != nil {
		return nil, err
	}
	logs, err := jobLogs(ctx, clientset)
	if err != nil {
		return nil, err
	}
	return parseLog(logs)
}

func jobLogs(ctx context.Context, clientset *kubernetes.Clientset) (string, error) {
	pods, err := clientset.CoreV1().Pods(promtoolJobNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("job-name=%s", promtoolJobName),
	})
	if err != nil {
		return "", fmt.Errorf("failed to list pods for job: %w", err)
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no pods found for job %q", promtoolJobName)
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

func parseQueryResult(promtoolresult string) (*model.Vector, error) {
	test := &model.Vector{}
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
func promtoolJob(version string, queries []NamedQuery, window ObservabilityWindow) *batchv1.Job {
	image := defaultImage + ":" + version
	var sb strings.Builder
	for _, q := range queries {
		pql := strings.ReplaceAll(q.Query, "$window", window.duration())
		pql = strings.ReplaceAll(pql, "$pod", window.PodName)
		fmt.Fprintf(&sb, "echo '%s'\n", q.Name)
		fmt.Fprintf(&sb, "promtool query instant -o json %s %q\n", promtoolEndpoint, pql)
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      promtoolJobName,
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
