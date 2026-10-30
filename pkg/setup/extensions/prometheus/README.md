# Prometheus Extension

The prometheus extension can be used to deploy prometheus to collect and query metrics data for perfomance testing.

The following use cases are supported:

Technical:
- Local test execution with cluster-provider-kind based on `setup.Bootstrap`.
- Remote test execution on existing infrastructure based on `setup.Validate`.

Functional:
- Monitoring Service Providers (default) by scraping the platform cluster.
- Monitoring Domain Services by additionally scraping for workload or control plane cluster.

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

2. Add a `prometheus.Query` in your test suite:


## Domain Service Testing Example

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

2. Add additional clusters to monitor with `prometheus.AddClusters` to scrape control planes and workload clusters that have been provisioned by your test suite:

```go
func TestAddClusters(t *testing.T) {
	addClusters := features.New("addClusters").
		Setup(providers.CreateMCP("test")).
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			if _, err := clusterutils.ImportToPlatformCluster(ctx, c, "crossplaneconfig"); err != nil {
				t.Fatalf("provider config import failed: %v", err)
			}
			if _, err := clusterutils.ImportToOnboardingCluster(ctx, "onboarding"); err != nil {
				t.Fatalf("onboarding resources import failed: %v", err)
			}
			return ctx
		}).
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			p := prometheus.Prometheus{}
			if err := p.AddClusters(ctx, c, prometheus.TargetCluster{
				Name:  "test", // control plane name
				Label: "test-controlplane",
			}); err != nil {
				t.Errorf("add scrape job for test control plane failed: %v", err)
			}
			return ctx
		})

	testenv.Test(t, addClusters.Feature())
}
```

3. Add a `prometheus.Query` in your test suite:

```go
```
