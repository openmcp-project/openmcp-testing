package setup

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"sigs.k8s.io/e2e-framework/klient/wait"
	klientconditions "sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/envconf"

	openmcpconditions "github.com/openmcp-project/openmcp-testing/pkg/conditions"
	"github.com/openmcp-project/openmcp-testing/pkg/resources"
)

// recreateServiceProvider, recreateClusterProvider, recreatePlatformService
// implement the LoadImageToCluster reuse-mode rollout for SP/CP/PS: delete
// the CR by name, wait for deletion, re-create it with the configured image,
// then wait for Ready=True. The operator owns the rest of the reconciliation
// (init job + run deployment + readiness condition), so this exercises the
// full operator path on each reuse run.

func recreateServiceProvider(ctx context.Context, c *envconf.Config, name, image string, waitOpts ...wait.Option) error {
	return recreateOpenMCPCR(ctx, c, schema.GroupVersionKind{
		Group:   "openmcp.cloud",
		Version: "v1alpha1",
		Kind:    "ServiceProvider",
	}, name, image, waitOpts...)
}

func recreateClusterProvider(ctx context.Context, c *envconf.Config, name, image string, waitOpts ...wait.Option) error {
	return recreateOpenMCPCR(ctx, c, schema.GroupVersionKind{
		Group:   "openmcp.cloud",
		Version: "v1alpha1",
		Kind:    "ClusterProvider",
	}, name, image, waitOpts...)
}

func recreatePlatformService(ctx context.Context, c *envconf.Config, name, image string, waitOpts ...wait.Option) error {
	return recreateOpenMCPCR(ctx, c, schema.GroupVersionKind{
		Group:   "openmcp.cloud",
		Version: "v1alpha1",
		Kind:    "PlatformService",
	}, name, image, waitOpts...)
}

// recreateOpenMCPCR is the shared implementation: delete the CR identified by
// gvk + name, wait for the API server to confirm deletion (which lets the
// operator finalize), then re-apply with the same spec.image and wait for the
// Ready condition. ResourceDeleted polls; the operator's controllers tear
// down the dependent Deployment/Job during this window.
func recreateOpenMCPCR(ctx context.Context, c *envconf.Config, gvk schema.GroupVersionKind, name, image string, waitOpts ...wait.Option) error {
	klog.Infof("LoadImageToCluster reuse: deleting %s/%s", gvk.Kind, name)
	ref := &unstructured.Unstructured{}
	ref.SetGroupVersionKind(gvk)
	ref.SetName(name)
	if err := c.Client().Resources().Delete(ctx, ref); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete %s/%s: %w", gvk.Kind, name, err)
	}
	allOpts := append([]wait.Option{wait.WithImmediate()}, waitOpts...)
	if err := wait.For(klientconditions.New(c.Client().Resources()).ResourceDeleted(ref), allOpts...); err != nil {
		return fmt.Errorf("wait %s/%s deletion: %w", gvk.Kind, name, err)
	}

	klog.Infof("LoadImageToCluster reuse: re-creating %s/%s", gvk.Kind, name)
	desired := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": gvk.GroupVersion().String(),
			"kind":       gvk.Kind,
			"metadata":   map[string]any{"name": name},
			"spec":       map[string]any{"image": image},
		},
	}
	if err := resources.CreateOrUpdate(ctx, c, desired); err != nil {
		return fmt.Errorf("recreate %s/%s: %w", gvk.Kind, name, err)
	}

	klog.Infof("LoadImageToCluster reuse: waiting for %s/%s Ready=True", gvk.Kind, name)
	if err := wait.For(openmcpconditions.Match(desired, c, "Ready", corev1.ConditionTrue), allOpts...); err != nil {
		return fmt.Errorf("wait %s/%s Ready: %w", gvk.Kind, name, err)
	}
	klog.Infof("LoadImageToCluster reuse: %s/%s rollout complete", gvk.Kind, name)
	return nil
}

// recreateOperator handles the LoadImageToCluster reuse-mode rollout for
// OpenMCPOperatorSetup. The operator isn't an SP-style CR — it's a raw
// Deployment + ServiceAccount + ClusterRoleBinding + ConfigMap stack applied
// from operator.yaml.tmpl. To trigger a rollout we delete the operator's
// Deployment, wait, then re-apply the manifest via
// CreateObjectsFromTemplateFile and wait for DeploymentAvailable.
//
// TODO: this path does NOT re-run the operator's own init Job. The init Job
// is part of operator.yaml.tmpl; if it already exists from a prior run, the
// re-apply through CreateOrUpdate updates its spec but a completed Job won't
// re-run on a spec update. SP/CP/PS recreate sidesteps this because the
// operator owns the dependent Job's lifecycle and re-creates it on its own
// reconcile. To exercise the operator's init code on reuse, we'd need to
// explicitly delete the existing operator init Job here. Filing as a known
// gap; not a regression vs. pre-LoadImageToCluster-reuse behavior.
func recreateOperator(ctx context.Context, c *envconf.Config, op OpenMCPOperatorSetup, operatorTemplate string) error {
	klog.Infof("LoadImageToCluster reuse: deleting operator Deployment %s/%s", op.Namespace, op.Name)
	dep := &unstructured.Unstructured{}
	dep.SetGroupVersionKind(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"})
	dep.SetName(op.Name)
	dep.SetNamespace(op.Namespace)
	if err := c.Client().Resources().Delete(ctx, dep); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete operator Deployment: %w", err)
	}
	allOpts := append([]wait.Option{wait.WithImmediate()}, op.WaitOpts...)
	if err := wait.For(klientconditions.New(c.Client().Resources()).ResourceDeleted(dep), allOpts...); err != nil {
		return fmt.Errorf("wait operator Deployment deletion: %w", err)
	}

	klog.Infof("LoadImageToCluster reuse: re-applying operator manifest")
	if _, err := resources.CreateObjectsFromTemplateFile(ctx, c, operatorTemplate, op); err != nil {
		return fmt.Errorf("re-apply operator manifest: %w", err)
	}
	if err := wait.For(klientconditions.New(c.Client().Resources()).DeploymentAvailable(op.Name, op.Namespace), allOpts...); err != nil {
		return fmt.Errorf("wait operator DeploymentAvailable: %w", err)
	}
	klog.Infof("LoadImageToCluster reuse: operator rollout complete")
	return nil
}
