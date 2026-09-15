# DNS

Package `setup/dns` provides functionality for testing webhooks in [OpenControlPlane](https://open-control-plane.io/) with [platform-service-gateway](https://github.com/openmcp-project/platform-service-gateway) and [platform-service-dns](https://github.com/openmcp-project/platform-service-dns).

`dns.CreateExternalService()` creates a dedicated DNS service to test scenarios where dynamic service discovery from multiple sources like `Service`, `TLSRoute`, `HTTPRoute` is required.

The configuration is based on the [CoreDNS with etcd](https://kubernetes-sigs.github.io/external-dns/latest/docs/tutorials/coredns-etcd/#overview) description of [external-dns](https://github.com/kubernetes-sigs/external-dns/).

See [service_test.go](../../../e2e/serviceprovider_test.go) for an example of how to integrate it in a service provider test.
