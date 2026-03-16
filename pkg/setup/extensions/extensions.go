package extensions

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
)

// Extension represents a pluggable component that can be installed into the OpenMCP testing environment.
// Extensions allow adding third-party tools, controllers, or services (like FluxCD, ArgoCD, etc.)
// to the test environment in a modular and reusable way.
//
// Example usage:
//
//	type MyExtension struct{}
//
//	func (e *MyExtension) Name() string {
//	    return "my-extension"
//	}
//
//	func (e *MyExtension) Install(ctx context.Context, cfg *envconf.Config) error {
//	    // Install the extension's Kubernetes resources
//	    return nil
//	}
//
//	func (e *MyExtension) RegisterSchemes(ctx context.Context, scheme *runtime.Scheme) error {
//	    // Register custom resource schemes if needed
//	    return myapi.AddToScheme(scheme)
//	}
type Extension interface {
	// Name returns a unique identifier for this extension.
	// This name is used in logs and for identifying the extension during setup.
	Name() string

	// Install deploys the extension into the Kubernetes cluster.
	// This method is called during the test environment setup phase.
	// It should create all necessary Kubernetes resources (deployments, services, CRDs, etc.)
	// required for the extension to function.
	//
	// Parameters:
	//   - ctx: The context for the installation operation
	//   - cfg: The e2e-framework configuration providing access to the Kubernetes client and cluster details
	//
	// Returns an error if the installation fails.
	Install(context.Context, *envconf.Config) error

	// RegisterSchemes registers the extension's custom resource schemes with the Kubernetes client.
	// This method is called after installation to ensure the client can work with the extension's
	// custom resources (CRDs).
	//
	// If the extension doesn't use custom resources, this can return nil.
	//
	// Parameters:
	//   - ctx: The context for the scheme registration
	//   - scheme: The runtime scheme to register custom types with
	//
	// Returns an error if scheme registration fails.
	RegisterSchemes(context.Context, *runtime.Scheme) error
}
