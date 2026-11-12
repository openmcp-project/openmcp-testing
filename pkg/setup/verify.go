package setup

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
)

// Verify tests if an enivroment meets the expected setup instead of creating it before executing a test suite.
// It ensures the requested OpenMCPSetup components are available and exits if these requirements are not met.
// Cleanup of test resources has to be implemented as part of the test suite.
func (s *OpenMCPSetup) Verify(testenv env.Environment) {
	klog.Info("validate environment")
	testenv.
		Setup(func(ctx context.Context, c *envconf.Config) (context.Context, error) {
			// namespace exists
			if err := c.Client().Resources().Get(ctx, s.Namespace, "", &corev1.Namespace{}); err != nil {
				return ctx, fmt.Errorf("failed to retrieve expected namespace %s: %w", s.Namespace, err)
			}
			klog.Infof("namespace (%s) ready", s.Namespace)
			return ctx, nil
		}).
		Setup(s.openControlPlaneOperatorAvailable).
		Setup(s.providersAvailable)
}

func (s *OpenMCPSetup) openControlPlaneOperatorAvailable(ctx context.Context, c *envconf.Config) (context.Context, error) {
	if s.Operator.Namespace == "" {
		s.Operator.Namespace = s.Namespace
	}
	operatorDep := &appsv1.Deployment{}
	operatorDep.SetName(s.Operator.Name)
	operatorDep.SetNamespace(s.Operator.Namespace)
	if err := wait.For(conditions.New(c.Client().Resources()).ResourceMatch(operatorDep, func(object k8s.Object) bool {
		klog.Infof("wait for OpenControlPlaneOperator (%s/%s) with image (%s)", s.Operator.Namespace, s.Operator.Name, s.Operator.Image)
		dep := object.(*appsv1.Deployment)
		containers := dep.Spec.Template.Spec.Containers
		if len(containers) != 1 {
			return false
		}
		return containers[0].Image == s.Operator.Image
	})); err != nil {
		return ctx, fmt.Errorf("failed to retrieve OpenControlPlane operator (%s/%s) with image (%s): %w", s.Operator.Namespace, s.Operator.Name, s.Operator.Image, err)
	}
	if err := wait.For(conditions.New(c.Client().Resources()).DeploymentAvailable(operatorDep.Name, operatorDep.Namespace)); err != nil {
		return ctx, fmt.Errorf("OpenControlPlane operator not ready: %w", err)
	}
	klog.Infof("OpenControlPlane Operator (%s/%s) ready", s.Operator.Namespace, s.Operator.Name)
	return ctx, nil
}

func (s *OpenMCPSetup) providersAvailable(ctx context.Context, c *envconf.Config) (context.Context, error) {
	// cluster providers
	for _, cp := range s.ClusterProviders {
		if err := cp.Available(ctx, c); err != nil {
			return ctx, err
		}
		klog.Infof("ClusterProvider (%s) ready", cp.Name)
	}
	// platform services
	for _, ps := range s.PlatformServices {
		if err := ps.Available(ctx, c); err != nil {
			return ctx, err
		}
	}
	// service providers
	for _, sp := range s.ServiceProviders {
		if err := sp.Available(ctx, c); err != nil {
			return ctx, err
		}
	}
	return ctx, nil
}
