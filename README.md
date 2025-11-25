[![REUSE status](https://api.reuse.software/badge/github.com/openmcp-project/openmcp-testing)](https://api.reuse.software/info/github.com/openmcp-project/openmcp-testing)

# openmcp-testing

## About this project

OpenMCP-testing helps to set up e2e test suites for openmcp applications. Like [xp-testing](https://github.com/crossplane-contrib/xp-testing) but for [openmcp](https://github.com/openmcp-project).

* [`pkg/clusterutils`](./pkg/clusterutils/) provides functionality to interact with the different clusters of an openMCP installation
* [`pkg/conditions`](./pkg/conditions/) provides common pre/post condition checks
* [`pkg/providers`](./pkg/providers/) provides functionality to test cluster-providers, platform-services and service-providers
* [`pkg/resources`](./pkg/resources/) provides functionality to (batch) import and delete resources
* [`pkg/setup`](./pkg/setup/) provides functionality to bootstrap an openmcp environment

## Requirements and Setup

You need [go](https://go.dev/) and [docker](https://www.docker.com/) to execute the sample test suite.

```shell
    go test -v ./e2e/...
```

## Support, Feedback, Contributing

This project is open to feature requests/suggestions, bug reports etc. via [GitHub issues](https://github.com/openmcp-project/openmcp-testing/issues). Contribution and feedback are encouraged and always welcome. For more information about how to contribute, the project structure, as well as additional contribution information, see our [Contribution Guidelines](CONTRIBUTING.md).

## Security / Disclosure

If you find any bug that may be a security problem, please follow our instructions at [in our security policy](https://github.com/openmcp-project/openmcp-testing/security/policy) on how to report it. Please do not create GitHub issues for security-related doubts or problems.

## Code of Conduct

We as members, contributors, and leaders pledge to make participation in our community a harassment-free experience for everyone. By participating in this project, you agree to abide by its [Code of Conduct](https://github.com/SAP/.github/blob/main/CODE_OF_CONDUCT.md) at all times.

## Licensing

Copyright 2025 SAP SE or an SAP affiliate company and openmcp-testing contributors. Please see our [LICENSE](LICENSE) for copyright and license information. Detailed information including third-party components and their licensing/copyright information is available [via the REUSE tool](https://api.reuse.software/info/github.com/openmcp-project/openmcp-testing).
