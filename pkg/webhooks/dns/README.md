# DNS

Package `webhooks/dns` provides utility functions that help testing webhooks in [OpenControlPlane](https://open-control-plane.io/).

## Usage

### Single TLSRoute Hostname Resolution 

For simple test scenarios where a single hostname of a TLSRoute in cluster `A` needs to be resolved by a Kubernetes API server in cluster `B`, use:

```go
// in your e2e FeatureBuilder
...
Setup(dns.NewSetup(t).InjectTLSRouteAlias(
	GatewayKey:  types.NamespacedName{Namespace: "openmcp-system", Name: "default"},
	TLSRouteKey: types.NamespacedName{Namespace: "openmcp-system", Name: "foo-webhook"},
)).
...
```

This adds the hostname of the TLSRoute together with the gateway IP (see [platform-service-gateway](https://github.com/openmcp-project/platform-service-gateway)) as a host alias to the Kubernetes API server pod spec and waits until the API has been restarted by the kubelet.

### Dedicated DNS Service

For complex test scenarios that involve service discovery from multiple sources like `Service`, `TLSRoute`, `HttpRoute` in cluster `A` which need to be resolved by a Kubernetes API server in cluster `B`, use:

```go
// in your e2e FeatureBuilder
...
Setup(dns.NewSetup(t).CreateExternalService()).
...
```

This will:
1. Create a dns cluster with dedicated [coreDNS](https://coredns.io/) and [etcd](https://etcd.io/) deployments.
2. Deploy [platform-service-dns](https://github.com/openmcp-project/platform-service-dns) configured with the coredns provider of external-dns to use the coredns from step 1.
3. Add the coreDNS from step 1 to the Kubernetes API server DNS config (default is the API server of the onboarding cluster).

Note that Flux is expected to be available on the platform cluster since CoreDNS is deployed using a `HelmRelease`.
