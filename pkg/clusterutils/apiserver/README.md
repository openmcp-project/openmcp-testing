# DNS

Package `clusterutils/apiserver` provides functionality for updating the static pod manifest of the Kubernetes API Server of [kind](https://kind.sigs.k8s.io/) control planes.

For example to test a webhook configuration in cluster `A` that needs to reach a service in cluster `B`, add a host alias:

```go
updater, _ := apiserver.NewUpdater()
updater.AddHostAlias("my-webhook.open-control-plane.dev", "10.89.201.1")
```

See the [service-provider-template](https://github.com/openmcp-project/service-provider-template) e2e test for an example of how this can be used to test a typical webhook configuration in OpenControlPlane[https://open-control-plane.io/] in combination with [platform-service-gateway](https://github.com/openmcp-project/platform-service-gateway).
