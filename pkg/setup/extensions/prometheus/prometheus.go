package prometheus

import (
	"context"
	"embed"
	"encoding/base64"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/envconf"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"

	"github.com/openmcp-project/openmcp-testing/internal"
	openmcpconditions "github.com/openmcp-project/openmcp-testing/pkg/conditions"
	"github.com/openmcp-project/openmcp-testing/pkg/resources"
	"github.com/openmcp-project/openmcp-testing/pkg/setup/extensions"
)

const (
	defaultNamespace = "monitoring"
	defaultVersion   = "v3.4.1"
	defaultImage     = "prom/prometheus"
)

//go:embed config/*
var configFS embed.FS

var _ extensions.Extension = &Prometheus{}

// remoteCluster describes an additional cluster to scrape.
type remoteCluster struct {
	// Name is a unique (prometheus) label applied to metrics from this cluster (cluster="<Name>").
	Name string
	// KubeconfigSecret is the name of the Secret (in SecretNamespace) whose "kubeconfig" key holds
	// a valid kubeconfig for the remote cluster.
	KubeconfigSecret string
	// SecretNamespace is the namespace of KubeconfigSecret.
	// If empty, defaults to the Prometheus installation namespace.
	SecretNamespace string
	// ContainerRegex defines the container filter for cAdvisor metrics,
	// eg "sp-.*|foo" drops any metrics of containers where the container name is not "foo" or starts with "sp-".
	// Defaults to ".*".
	ContainerRegex string
	// APIServer is the host:port endpoint used to proxy kubelet metrics (see prometheus config template).
	// The value is populated automatically based on the kubeconfig of the requested cluster access.
	APIServer string
}

type TargetCluster struct {
	// Name is the name of the cluster (request), e.g. a controlplane with name test-mcp will result in a cluster-request (in a tenant namespace) with the same name
	Name string
	// Label is a unique (prometheus) label applied to metrics from this cluster (cluster="<Name>").
	Label string
}

type Prometheus struct {
	// Namespace is the Kubernetes namespace where Prometheus will be installed.
	// If empty, defaults to "monitoring".
	Namespace string
	// If empty, defaults to "openmcp-testing" to prevent conflicts with existing resources when testing agains real environments
	ClusterResourceSuffix string
}

func (p *Prometheus) applyDefaults() {
	if p.Namespace == "" {
		p.Namespace = defaultNamespace
	}
	if p.ClusterResourceSuffix == "" {
		p.ClusterResourceSuffix = "openmcp-testing"
	}
}

// Uninstall implements [extensions.Extension].
func (p *Prometheus) Uninstall(ctx context.Context, cfg *envconf.Config) error {
	klog.Info("uninstalling prometheus...")
	p.applyDefaults()
	if err := p.cleanupAccess(ctx, cfg); err != nil {
		return err
	}
	obj := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: p.Namespace}}
	if err := cfg.Client().Resources().Delete(ctx, obj); err != nil {
		return err
	}
	if err := wait.For(conditions.New(cfg.Client().Resources()).ResourceDeleted(obj)); err != nil {
		return err
	}
	// cleanup rbac
	rbacv1.AddToScheme(cfg.Client().Resources().GetScheme())
	name := "prometheus-" + p.ClusterResourceSuffix
	if err := deleteIfTestingResource(ctx, &rbacv1.ClusterRoleBinding{Name: name}, cfg); err != nil {
		return err
	}
	return deleteIfTestingResource(ctx, &rbacv1.ClusterRole{Name: name}, cfg)
}

func deleteIfTestingResource(ctx context.Context, role k8s.Object, cfg *envconf.Config) error {
	if err := cfg.Client().Resources().Get(ctx, role.GetName(), "", role); err == nil {
		if _, ok := role.GetLabels()[resources.CreatedByKey]; ok {
			if err := cfg.Client().Resources().Delete(ctx, role); err != nil && !errors.IsNotFound(err) {
				return err
			}
		}
	}
	return nil
}

// Name implements [extensions.Extension].
func (p *Prometheus) Name() string {
	return "prometheus"
}

// Install implements [extensions.Extension].
func (p *Prometheus) Install(ctx context.Context, cfg *envconf.Config) error {
	klog.Info("installing prometheus...")
	p.applyDefaults()
	if err := createIfNotExists(ctx, cfg, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: p.Namespace}}); err != nil {
		return err
	}
	if err := p.rbacIfNotExists(ctx, cfg); err != nil {
		return err
	}
	if err := p.configMap(ctx, cfg); err != nil {
		return err
	}
	if err := p.deployment(ctx, cfg); err != nil {
		return err
	}
	klog.Infof("prometheus installed successfully in namespace %s", p.Namespace)
	return nil
}

// RegisterSchemes implements [extensions.Extension].
func (p *Prometheus) RegisterSchemes(_ context.Context, _ *runtime.Scheme) error {
	return nil
}

func createIfNotExists(ctx context.Context, cfg *envconf.Config, obj k8s.Object) error {
	if err := cfg.Client().Resources().Create(ctx, obj); err != nil {
		if !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("failed to create %T %s: %w", obj, obj.GetName(), err)
		}
	}
	return nil
}

// createPrometheusJobSecret reads the cluster kubeconfigs from a source namespace and creates a copy in a destination namespace so the Prometheus pod can mount it.
// A secret copy additional contains "ca.crt" and "token" keys based on the kubeconfig to use in the prometheus config.
// It returns the remote cluster's API server host (host:port) extracted from the kubeconfig.
func createPrometheusJobSecret(ctx context.Context, cfg *envconf.Config, srcRef, dest types.NamespacedName) (string, error) {
	src := &corev1.Secret{}
	if err := cfg.Client().Resources().Get(ctx, srcRef.Name, srcRef.Namespace, src); err != nil {
		return "", fmt.Errorf("failed to get kubeconfig secret %s: %w", srcRef, err)
	}

	kubeconfigBytes, ok := src.Data["kubeconfig"]
	if !ok {
		return "", fmt.Errorf("kubeconfig secret %s has no kubeconfig key", srcRef)
	}
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse kubeconfig from secret %s: %w", srcRef, err)
	}

	var caData []byte
	switch {
	case len(restConfig.CAData) > 0:
		caData = restConfig.CAData
	case restConfig.CAFile != "":
		return "", fmt.Errorf("kubeconfig secret %s references a CA file path which is not supported in-cluster", srcRef)
	}

	tokenBytes := []byte(restConfig.BearerToken)
	if len(tokenBytes) == 0 && restConfig.BearerTokenFile != "" {
		return "", fmt.Errorf("kubeconfig secret %s references a bearer token file path which is not supported in-cluster", srcRef)
	}
	if len(tokenBytes) == 0 {
		// Try base64-decoding in case the token is stored encoded (some generators do this)
		if raw, err := base64.StdEncoding.DecodeString(string(src.Data["token"])); err == nil {
			tokenBytes = raw
		} else {
			tokenBytes = src.Data["token"]
		}
	}

	data := map[string][]byte{
		"kubeconfig": kubeconfigBytes,
	}
	if len(caData) > 0 {
		data["ca.crt"] = caData
	}
	if len(tokenBytes) > 0 {
		data["token"] = tokenBytes
	}

	copy := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dest.Name,
			Namespace: dest.Namespace,
		},
		Data: data,
		Type: src.Type,
	}
	return strings.TrimPrefix(restConfig.Host, "https://"), createIfNotExists(ctx, cfg, copy)
}

func (p *Prometheus) rbacIfNotExists(ctx context.Context, c *envconf.Config) error {
	rbacTemplate := internal.MustTmpFileFromEmbedFS(configFS, "config/rbac.yaml.tmpl")
	templateData := struct {
		Namespace string
		Suffix    string
	}{
		Namespace: p.Namespace,
		Suffix:    p.ClusterResourceSuffix,
	}
	// Check if a ClusterRole with this name already exists would risk tampering with roles on real environments.
	name := "prometheus-" + p.ClusterResourceSuffix
	role := &rbacv1.ClusterRole{}
	rbacv1.AddToScheme(c.Client().Resources().GetScheme())
	if err := c.Client().Resources().Get(ctx, name, "", role); err == nil {
		return fmt.Errorf("ClusterRole %q already exists; set a unique ClusterResourceSuffix to avoid conflicts with existing roles", name)
	}
	binding := &rbacv1.ClusterRoleBinding{}
	if err := c.Client().Resources().Get(ctx, name, "", binding); err == nil {
		return fmt.Errorf("ClusterRoleBinding %q already exists; set a unique ClusterResourceSuffix to avoid conflicts with existing bindings", name)
	}
	if _, err := resources.CreateObjectsFromTemplateFile(ctx, newConfigWithNamespace(c, p.Namespace), rbacTemplate, templateData); err != nil {
		return fmt.Errorf("failed to create prometheus rbac: %w", err)
	}
	return nil
}

func (p *Prometheus) configMap(ctx context.Context, c *envconf.Config, remoteClusters ...remoteCluster) error {
	configTemplate := internal.MustTmpFileFromEmbedFS(configFS, "config/config.yaml.tmpl")
	for i := range remoteClusters {
		if remoteClusters[i].ContainerRegex == "" {
			remoteClusters[i].ContainerRegex = ".*"
		}
	}
	templateData := struct {
		RemoteClusters []remoteCluster
	}{
		RemoteClusters: remoteClusters,
	}
	if len(remoteClusters) > 0 {
		if err := resources.UpdateObjectsFromTemplateFile(ctx, newConfigWithNamespace(c, p.Namespace), configTemplate, templateData); err != nil {
			return fmt.Errorf("failed to update prometheus config: %w", err)
		}
	} else {
		if _, err := resources.CreateObjectsFromTemplateFile(ctx, newConfigWithNamespace(c, p.Namespace), configTemplate, templateData); err != nil {
			return fmt.Errorf("failed create prometheus config: %w", err)
		}
	}
	return nil
}

func (p *Prometheus) deployment(ctx context.Context, c *envconf.Config, remoteClusters ...remoteCluster) error {
	deployTemplate := internal.MustTmpFileFromEmbedFS(configFS, "config/deployment.yaml.tmpl")
	templateData := struct {
		Image          string
		RemoteClusters []remoteCluster
	}{
		Image:          defaultImage + ":" + defaultVersion,
		RemoteClusters: remoteClusters,
	}
	if len(remoteClusters) > 0 {
		if err := resources.UpdateObjectsFromTemplateFile(ctx, newConfigWithNamespace(c, p.Namespace), deployTemplate, templateData); err != nil {
			return fmt.Errorf("failed to update prometheus deployment: %w", err)
		}
	} else {
		if _, err := resources.CreateObjectsFromTemplateFile(ctx, newConfigWithNamespace(c, p.Namespace), deployTemplate, templateData); err != nil {
			return fmt.Errorf("failed to create prometheus deployment: %w", err)
		}
	}
	klog.Info("waiting for prometheus to be ready")
	if err := wait.For(conditions.New(c.Client().Resources()).DeploymentAvailable("prometheus", promtoolJobNamespace)); err != nil {
		return fmt.Errorf("prometheus deployment not available: %w", err)
	}
	return nil
}

const accessRequestTemplate = `
apiVersion: clusters.openmcp.cloud/v1alpha1
kind: AccessRequest
metadata:
  name: prometheus-{{ .ClusterRequestName }}
  namespace: {{ .Namespace }}
spec:
  requestRef:
    name: {{ .ClusterRequestName }}
    namespace: {{ .ClusterRequestNamespace }}
  token:
    permissions:
    - rules:
      - apiGroups: [""]
        resources: ["pods", "pods/proxy", "nodes", "nodes/metrics", "nodes/proxy"]
        verbs: ["get", "list", "watch"]
`

// AddClusters adds a scrape job to the prometheus config for each given TargetCluster.
// For each cluster it creates an AccessRequest in the Prometheus namespace and updates the ConfigMap and Prometheus deployment to mount every remote kubeconfig.
func (p *Prometheus) AddClusters(ctx context.Context, cfg *envconf.Config, targetClusters ...TargetCluster) error {
	p.applyDefaults()
	var additionalClusters []remoteCluster
	for _, tc := range targetClusters {
		klog.Info("request access to scrape additional clusters")
		// 1. lookup cluster request based on TargetCluster.Name
		cr, err := findClusterRequest(ctx, cfg, tc.Name)
		if err != nil {
			return fmt.Errorf("failed to find cluster request for %s: %w", tc.Name, err)
		}

		// 2. create a corresponding AccessRequest in the prometheus namespace and wait for the reference kubeconfig secret to be created
		arData := struct {
			ClusterRequestName      string
			ClusterRequestNamespace string
			Namespace               string
		}{
			ClusterRequestName:      cr.GetName(),
			ClusterRequestNamespace: cr.GetNamespace(),
			Namespace:               p.Namespace,
		}
		arObj, err := resources.CreateObjectFromTemplate(ctx, newConfigWithNamespace(cfg, p.Namespace), accessRequestTemplate, arData)
		if err != nil {
			return fmt.Errorf("failed to create access request for cluster %s: %w", tc.Name, err)
		}
		if err := wait.For(openmcpconditions.Status(arObj, cfg, "phase", clustersv1alpha1.AccessRequestGranted)); err != nil {
			return fmt.Errorf("access request for cluster %s not granted: %w", tc.Name, err)
		}
		if err := wait.For(func(ctx context.Context) (bool, error) {
			if err := cfg.Client().Resources().Get(ctx, arObj.GetName(), arObj.GetNamespace(), arObj); err != nil {
				return false, err
			}
			_, found, err := unstructured.NestedFieldNoCopy(arObj.Object, "status", "secretRef")
			return found, err
		}); err != nil {
			return fmt.Errorf("kubeconfig secret for cluster %s not created: %w", tc.Name, err)
		}

		// 3. create a new secret based on the access request kubeconfig that can be used scrape the given cluster
		secretName, _, _ := unstructured.NestedString(arObj.Object, "status", "secretRef", "name")
		promSecretName := secretName + ".prom"
		apiServer, err := createPrometheusJobSecret(ctx, cfg, types.NamespacedName{Name: secretName, Namespace: p.Namespace}, types.NamespacedName{Name: promSecretName, Namespace: p.Namespace})
		if err != nil {
			return fmt.Errorf("failed to copy kubeconfig secret for cluster %s: %w", tc.Name, err)
		}
		rc := remoteCluster{
			Name:             tc.Label,
			KubeconfigSecret: promSecretName,
			APIServer:        apiServer,
		}
		additionalClusters = append(additionalClusters, rc)
	}

	klog.Info("update prometheus config and reload prometheus")
	// when all cluster secrets have been created: update the prometheus config and deployment to mount any additional remote cluster secrets
	if err := p.configMap(ctx, cfg, additionalClusters...); err != nil {
		return fmt.Errorf("failed to update prometheus config: %w", err)
	}
	if err := p.deployment(ctx, cfg, additionalClusters...); err != nil {
		return fmt.Errorf("failed to update prometheus deployment: %w", err)
	}
	return nil
}

// findClusterRequest looks up a ClusterRequest by name across all namespaces.
func findClusterRequest(ctx context.Context, cfg *envconf.Config, name string) (k8s.Object, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "clusters.openmcp.cloud",
		Version: "v1alpha1",
		Kind:    "ClusterRequestList",
	})
	if err := cfg.Client().Resources().List(ctx, list); err != nil {
		return nil, err
	}
	for i := range list.Items {
		if list.Items[i].GetName() == name {
			return &list.Items[i], nil
		}
	}
	return nil, fmt.Errorf("cluster request %q not found", name)
}

func newConfigWithNamespace(cfg *envconf.Config, namespace string) *envconf.Config {
	return envconf.New().WithClient(cfg.Client()).WithNamespace(namespace)
}

// cleanupAccess deletes all AccessRequests in p.Namespace
func (p *Prometheus) cleanupAccess(ctx context.Context, cfg *envconf.Config) error {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "clusters.openmcp.cloud",
		Version: "v1alpha1",
		Kind:    "AccessRequestList",
	})
	if err := cfg.Client().Resources(p.Namespace).List(ctx, list); err != nil {
		klog.Errorf("cleanup: failed to list access requests in namespace %s: %v", p.Namespace, err)
		return err
	}
	for i := range list.Items {
		ar := &list.Items[i]
		if err := cfg.Client().Resources(p.Namespace).Delete(ctx, ar); err != nil {
			klog.Errorf("cleanup: failed to delete access request %s/%s: %v", p.Namespace, ar.GetName(), err)
			return err
		}
	}
	return nil
}
