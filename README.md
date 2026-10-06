[![REUSE status](https://api.reuse.software/badge/github.com/openmcp-project/openmcp-testing)](https://api.reuse.software/info/github.com/openmcp-project/openmcp-testing)

# openmcp-testing

## About this project

OpenMCP-testing helps to set up e2e test suites for openmcp applications. Like [xp-testing](https://github.com/crossplane-contrib/xp-testing) but for [openmcp](https://github.com/openmcp-project).

* [`pkg/clusterutils`](./pkg/clusterutils/) provides functionality to interact with the different clusters of an openMCP installation
* [`pkg/conditions`](./pkg/conditions/) provides common pre/post condition checks
* [`pkg/providers`](./pkg/providers/) provides functionality to test cluster-providers, platform-services and service-providers
* [`pkg/resources`](./pkg/resources/) provides functionality to (batch) import and delete resources
* [`pkg/setup`](./pkg/setup/) provides functionality to bootstrap and verify OpenControlPlane environments

## Configuration

The following environment variables exist:

| Variable | Description | Default |
| -------- | ----------- | ------- |
| ONBOARDING_KUBECONFIG | The path to the kubeconfig to interact with the onboarding API server. If not present, openmcp-testing will try to retrieve the config with [kind](https://kind.sigs.k8s.io/) | - |

## Requirements and Setup

You need [go](https://go.dev/) and [docker](https://www.docker.com/) to execute the sample test suite.

```shell
go test -v ./e2e/... -count=1
```

## Testing Against Existing Environments

1. Set your current kubeconfig context to point to the OpenControlPlane [platform cluster](https://open-control-plane.io/developers/clusters-and-namespaces#cluster-overview).
2. Set the `ONBOARDING_KUBECONFIG` environment variable to provide the onboarding cluster context.

This allows you to interact with the platform and onboarding clusters without requiring any changes to an existing test suite.

Use `OpenMCPSetup.Verify` instead of `OpenMCPSetup.Bootstrap` to verify that an existing environment meets the requested OpenControlPlane setup before executing your test suite.

**Limitations**

To interact with control plane or workload clusters in non-kind-based environments, you need to use the [ClusterAccess Library](https://github.com/openmcp-project/openmcp-operator/blob/main/docs/libraries/clusteraccess.md). `openmcp-testing` provides a utility function to convert a client retrieved through the ClusterAccess library back to an `envconfig.Config`, allowing you to create a typical e2e-framework test suite. Note that `clusterutils.MCPConfig` and `clusterutils.ConfigByPrefix` functionality only work for [cluster-provider-kind](https://github.com/openmcp-project/cluster-provider-kind) based environments (e.g. bootstrapped with [ocpctl](https://github.com/openmcp-project/ocpctl)).

## Support, Feedback, Contributing

This project is open to feature requests/suggestions, bug reports etc. via [GitHub issues](https://github.com/openmcp-project/openmcp-testing/issues). Contribution and feedback are encouraged and always welcome. For more information about how to contribute, the project structure, as well as additional contribution information, see our [Contribution Guidelines](CONTRIBUTING.md).

## Security / Disclosure

If you find any bug that may be a security problem, please follow our instructions at [in our security policy](https://github.com/openmcp-project/openmcp-testing/security/policy) on how to report it. Please do not create GitHub issues for security-related doubts or problems.

## Code of Conduct

We as members, contributors, and leaders pledge to make participation in our community a harassment-free experience for everyone. By participating in this project, you agree to abide by its [Code of Conduct](https://github.com/openmcp-project/.github/blob/main/CODE_OF_CONDUCT.md) at all times.

## Licensing

Copyright OpenControlPlane contributors. Please see our [LICENSE](LICENSE) for copyright and license information. Detailed information including third-party components and their licensing/copyright information is available [via the REUSE tool](https://api.reuse.software/info/github.com/openmcp-project/openmcp-testing).

---

<p align="center">
  <a href="https://apeirora.eu/content/projects/">
    <img alt="BMWK-EU funding logo" src="https://apeirora.eu/assets/img/BMWK-EU.png" width="300"/>
  </a>
</p>

<p align="center">
  OpenControlPlane is part of <a href="https://apeirora.eu/content/projects/">ApeiroRA</a>, an EU Important Project of Common European Interest (IPCEI-CIS).
</p>

<p align="center">
  Copyright Linux Foundation Europe. For web site terms of use, trademark policy and other project policies please see <a href="https://linuxfoundation.eu/en/policies">https://linuxfoundation.eu/en/policies</a>.
</p>
