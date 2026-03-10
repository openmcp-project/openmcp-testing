package fluxcd

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/yaml"

	fluxinstall "github.com/fluxcd/flux2/v2/pkg/manifestgen/install"
	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	kustomize1 "github.com/fluxcd/kustomize-controller/api/v1"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
)

// FluxCD is an Extension that installs FluxCD (https://fluxcd.io) into the test environment.
// FluxCD is a GitOps continuous delivery solution for Kubernetes that keeps clusters in sync
// with configuration sources (like Git repositories) and automates configuration updates.
//
// This extension installs the following FluxCD components:
//   - source-controller: Handles source definitions (Git, Helm, OCI repositories)
//   - kustomize-controller: Applies Kustomize overlays from sources
//   - helm-controller: Manages Helm releases
//   - notification-controller: Handles event notifications and webhooks
//   - image-reflector-controller: Scans container registries for image metadata
//   - image-automation-controller: Automates image updates in Git
//
// All components are installed into the specified namespace (defaults to "flux-system").
//
// Usage:
//
//	openmcp := setup.OpenMCPSetup{
//	    Extensions: []extensions.Extension{
//	        &fluxcd.FluxCD{
//	            Namespace: "custom-flux-ns", // Optional, defaults to "flux-system"
//	        },
//	    },
//	}
type FluxCD struct {
	// Namespace is the Kubernetes namespace where FluxCD components will be installed.
	// If empty, defaults to "flux-system".
	Namespace string
}

// Name returns the unique identifier for this extension.
func (f *FluxCD) Name() string {
	return "fluxcd"
}

// Install deploys FluxCD into the Kubernetes cluster using the native Go API.
// This method generates FluxCD manifests programmatically and applies them to the cluster,
// eliminating the need for the external flux CLI tool.
//
// The installation process:
//  1. Generates installation manifests using flux2's manifestgen package
//  2. Splits the multi-document YAML into individual resources
//  3. Unmarshals each resource as an unstructured object
//  4. Creates each resource in the cluster (gracefully handling already-exists errors)
//
// Returns an error if manifest generation or resource creation fails.
func (f *FluxCD) Install(ctx context.Context, cfg *envconf.Config) error {
	klog.Info("installing flux...")

	// Use configured namespace or default to "flux-system"
	namespace := f.Namespace
	if namespace == "" {
		namespace = "flux-system"
	}

	// Generate the Flux installation manifests
	options := fluxinstall.MakeDefaultOptions()
	options.Namespace = namespace
	options.Components = []string{
		"source-controller",
		"kustomize-controller",
		"helm-controller",
		"notification-controller",
	}
	options.ComponentsExtra = []string{
		"image-reflector-controller",
		"image-automation-controller",
	}

	manifest, err := fluxinstall.Generate(options, "")
	if err != nil {
		return fmt.Errorf("failed to generate flux manifests: %w", err)
	}

	// Split and apply the manifests
	manifests := strings.Split(manifest.Content, "---\n")
	for _, m := range manifests {
		if strings.TrimSpace(m) == "" {
			continue
		}

		obj := &unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(m), obj); err != nil {
			return fmt.Errorf("failed to unmarshal manifest: %w", err)
		}

		if err := cfg.Client().Resources().Create(ctx, obj); err != nil {
			// Ignore already exists errors
			if !strings.Contains(err.Error(), "already exists") {
				return fmt.Errorf("failed to create resource %s/%s: %w",
					obj.GetKind(), obj.GetName(), err)
			}
		}
	}

	klog.Infof("flux installed successfully in namespace %s", namespace)
	return nil
}

// RegisterSchemes registers FluxCD's custom resource schemes with the Kubernetes client.
// This enables the test framework to work with FluxCD custom resources like HelmRelease,
// Kustomization, GitRepository, HelmRepository, etc.
//
// The following schemes are registered:
//   - helm-controller (HelmRelease)
//   - kustomize-controller (Kustomization)
//   - source-controller (GitRepository, HelmRepository, HelmChart, OCIRepository, Bucket)
//
// Returns an error if any scheme registration fails.
func (f *FluxCD) RegisterSchemes(_ context.Context, scheme *runtime.Scheme) error {
	if err := helmv2.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to register helm-controller scheme: %w", err)
	}
	if err := kustomize1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to register kustomization-controller scheme: %w", err)
	}
	if err := sourcev1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to register source-controller scheme: %w", err)
	}
	return nil
}
