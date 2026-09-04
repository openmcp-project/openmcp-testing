# DNS

Package `dns` provides utility functions that help testing webhooks in [OpenControlPlane](https://open-control-plane.io/).

Complex test scenarios that involve service discovery from multiple sources like `Service`, `TLSRoute`, `HttpRoute` in cluster `A` for webhook configurations in cluster `B`, can be tested with `dns.CreateExternalService()`.

This will:
1. Create a dns cluster with dedicated [coreDNS](https://coredns.io/) and [etcd](https://etcd.io/) deployments.
2. Deploy [platform-service-dns](https://github.com/openmcp-project/platform-service-dns) configured with the coredns provider of external-dns to use the coredns from step 1.
3. Add the coreDNS from step 1 to the Kubernetes API server DNS config (default is the API server of the onboarding cluster).

See [service_test.go](../../../e2e/serviceprovider_test.go) for an example of how to use it.
