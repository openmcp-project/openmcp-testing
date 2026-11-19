# Prometheus Extension

The Prometheus extension deploys a dedicated test Prometheus instance and enables performance testing via [PromQL](https://prometheus.io/docs/prometheus/latest/querying/basics/) queries. It can be used to test [service providers](#service-provider-testing-example) and [domain services](#domain-service-testing-example) in the context of OpenControlPlane.

Tests can be executed by bootstrapping an OpenControlPlane environment or [against an existing environment](#testing-against-existing-environments).

## Metrics Overview

The Prometheus extension is configured to collect the following metrics per target cluster (platform and optional remote clusters):

- metrics provided by an application like [controller-runtime metrics](https://book.kubebuilder.io/reference/metrics-reference.html) and [prometheus/client_golang collector metrics](https://github.com/prometheus/client_golang)
- metrics provided by the [kubelet](https://kubernetes.io/docs/reference/instrumentation/metrics/) (/metrics/resource)

## Query Parameters Mapping

Each `prometheus.Query` call results in a Kubernetes job that executes promtool [range queries](https://prometheus.io/docs/prometheus/latest/querying/api/#range-queries). The following parameters exist:

- (required) Window defines the start, end and step settings of the range query. The derived duration can also be used as a placeholder in PromQL queries (see set of predefined queries as a reference).
- (required) Job name identifies the job resource to parse and return the query result. Since the Prometheus extension uses an exclusive namespace, any name can be chosen as long as it is unique across `prometheus.Query` calls within the current test context.
- (optional) Container and controller parameters can be used as placeholders in PromQL queries; some predefined queries require these values.

## Service Provider Testing Flow

1. Add the `prometheus.Prometheus` to `OpenMCPSetup.Extensions`.
2. Use `prometheus.Query` to generate test results and reports.

## Domain Service Testing Flow

1. Add the `prometheus.Prometheus` to `OpenMCPSetup.Extensions`.
2. Add the control plane or workload cluster that runs your domain service as an additional scrape target with `prometheus.AddClusters`. A cluster has to be ready when you call this function. Note that adding clusters restarts Prometheus, which causes all previously scraped data to be lost. This is only relevant if you mix service provider and domain service testing in the same test suite, which is not recommended.
3. Use `prometheus.Query` to generate test results and reports.

## Reference Usage

The [performance e2e test](../../../../e2e/performance_test.go) provides a usage example covering Prometheus extension install/uninstall and basic error-free operation, but does not assert the correctness of the metrics or data it produces. Real-world tests should set an appropriate query window and reasonable waiting times.

## Testing Against Existing Environments

Use the extension directly instead of bootstrapping an OpenControlPlane environment.

```go
promExt := prometheus.Prometheus{}
testenv = env.NewWithConfig(envconf.New().WithNamespace(openmcp.Namespace))
testenv.Setup(func(ctx context.Context, c *envconf.Config) (context.Context, error) {
	return promExt.Install(ctx, c)
}).Finish(func(ctx context.Context, c *envconf.Config) (context.Context, error) {
	return promExt.Uninstall(ctx, c)
})
os.Exit(testenv.Run(m))
```

The current kubeconfig context must point to the platform cluster. To interact with additional [clusters](https://open-control-plane.io/developers/clusters-and-namespaces#cluster-overview), e.g. to create resources on the onboarding or a control plane cluster, use `clusterutils.ConfigByEnv` instead of the kind-based config utility functions.

Verify that an environment meets an expected configuration by using `OpenMCPSetup.Verify`.

Note that reusing an existing Prometheus deployment is not supported, as the extension relies on configuration assumptions that can't otherwise be enforced.

## Load Generation

The `resources` package provides a simple way to generate load with `CreateNObjectsFromTemplateFile` based on text templating and `klient.decoder` of [e2e-framework](https://github.com/kubernetes-sigs/e2e-framework/). Use this function to generate arbitrary resources for both service provider and domain services.
