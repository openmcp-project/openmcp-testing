# Prometheus Extension

The prometheus extension can be used to deploy prometheus to collect and query metrics data for perfomance testing.

The following use cases are supported:

- Local test execution with cluster-provider-kind based on `setup.Bootstrap`.
- Remote test execution on existing infrastructure based on `setup.Validate` and .
- Monitoring Service Providers with target=platform (default).
- Monitoring Domain Services with target=workload.

## Usage

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

