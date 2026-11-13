# Prometheus Extension

The prometheus extension can be used to deploy prometheus to collect and query metrics data for perfomance testing.

The following use cases are supported:

Technical:
- Local test execution with cluster-provider-kind based on `setup.Bootstrap`.
- Remote test execution on existing infrastructure based on `setup.Validate`.

Functional:
- Monitoring Service Providers (default) by scraping the platform cluster.
- Monitoring Domain Services by additionally scraping for workload or control plane cluster (`prometheus.AddCluster`).

Note that adding additional clusters restarts prometheus which results in losing all previously scraped data. This is only a concern if you mix evaluation of service provider performance with domain service performance as part of the same test which is currently not recommended.

## Metrics Overview

The prometheus extension is configured to collect the following metrics per target cluster (platform and optional remote clusters):

- metrics provided by an application like [controller-runtime metrics](https://book.kubebuilder.io/reference/metrics-reference.html) and [prometheus go client collector metrics](https://github.com/prometheus/client_golang)
- metrics provided by the [kubelet](https://kubernetes.io/docs/reference/instrumentation/metrics/) (/metrics/resource)

## Query Options

- the required window query parameter defines the start, end and step settings of the executed [range query](https://prometheus.io/docs/prometheus/latest/querying/api/#range-queries).
- the required job name identifies the job resource to parse and return the query result
- Container and controller parameters are used to select the controllers under test (requirement depends on the set of promql queries passed to `prometheus.Query`)

## Service Provider Testing Example

1. Add the prometheus extension to your OpenMCPSetup:

```go
setup.OpenMCPOperatorSetup{
	Name:         "openmcp-operator",
	Image:        "ghcr.io/openmcp-project/images/openmcp-operator:v1.3.0",
	Extensions: []extensions.Extension{
		&prometheus.Prometheus{},
	},
}
```

2. Add a `prometheus.Query` in your test suite (note that in a real test you want to provide a time window that actually represents the start and end time where you e.g. created load on the controller under test):

```go
params := prometheus.QueryParameters{
	Container:  container,
	Controller: controller,
	Window: prometheus.ObservabilityWindow{
		Start: time.Now().Add(-15 * time.Minute),
		End:   time.Now(),
	},
	JobName: fmt.Sprintf("query-%s", container),
}
results, err := prometheus.Query(ctx, cfg, queries, params)
```

## Domain Service Testing Example

1. Add the prometheus extension to your OpenMCPSetup (same as before).
2. Add additional clusters to monitor with `prometheus.AddClusters` to scrape control planes and workload clusters that have been provisioned by your test suite:

```go
Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
	p := prometheus.Prometheus{}
	if err := p.AddClusters(ctx, c, prometheus.TargetCluster{
		Name:  "test", // control plane name
		Label: "test-controlplane",
	}); err != nil {
		t.Errorf("add scrape job for test control plane failed: %v", err)
	}
	return ctx
}).
```

3. Add a `prometheus.Query` in your test suite (same as before).

## Reference Usage

Both scenarios are covered in [e2e test](../../../../e2e/observability_test.go)

## Testing Against Existing Environments

Use the extension directly instead of bootstrapping an OpenControlPlane enviroment.

```go
promExt := prometheus.Prometheus{}
testenv = env.NewWithConfig(envconf.New().WithNamespace(openmcp.Namespace))
testenv.Setup(func(ctx context.Context, c *envconf.Config) (context.Context, error) {
	if err := promExt.Install(ctx, c); err != nil {
		return nil, err
	}
	return ctx, nil
}).Finish(func(ctx context.Context, c *envconf.Config) (context.Context, error) {
	if err := promExt.Uninstall(ctx, c); err != nil {
		return nil, err
	}
	return ctx, nil
})
os.Exit(testenv.Run(m))
```

It is recommended to verify that an environment meets an expected configuration by using `openMCPSetup.Verify`.
