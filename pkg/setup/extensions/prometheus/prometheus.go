package prometheus

import (
	"context"
	"embed"
	"encoding/base64"
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/klient/wait"
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

var _ extensions.Extension = Prometheus{}

// RemoteCluster describes an additional cluster to scrape.
type RemoteCluster struct {
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
	ContainerRegex string
}

type TargetCluster struct {
	// Name is the name of the cluster (request), e.g. a controlplane with name test-mcp will result in a cluster-request (in a tenant namespace) with the same name
	Name string
	// Label is a unique (prometheus) label applied to metrics from this cluster (cluster="<Name>").
	Label string
}

type Prometheus struct {
	// Version defines the Prometheus version to install.
	// If empty, defaults to "v3.4.1".
	Version string
	// Namespace is the Kubernetes namespace where Prometheus will be installed.
	// If empty, defaults to "monitoring".
	Namespace string
	// RemoteClusters lists additional clusters whose pods Prometheus should scrape.
	RemoteClusters []RemoteCluster
}

// Name implements [extensions.Extension].
func (p Prometheus) Name() string {
	return "prometheus"
}

// Install implements [extensions.Extension].
func (p Prometheus) Install(ctx context.Context, cfg *envconf.Config) error {
	klog.Info("installing prometheus...")
	namespace := p.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}
	version := p.Version
	if version == "" {
		version = defaultVersion
	}
	if err := createIfNotExists(ctx, cfg, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}); err != nil {
		return err
	}
	if err := rbac(ctx, cfg, namespace); err != nil {
		return err
	}
	for _, rc := range p.RemoteClusters {
		if err := createPrometheusJobSecret(ctx, cfg, types.NamespacedName{Name: rc.KubeconfigSecret, Namespace: rc.SecretNamespace}, types.NamespacedName{Name: rc.KubeconfigSecret, Namespace: p.Namespace}); err != nil {
			return err
		}
	}
	if err := configMap(ctx, cfg, namespace, false, p.RemoteClusters); err != nil {
		return err
	}
	if err := deployment(ctx, cfg, namespace, version, false, p.RemoteClusters); err != nil {
		return err
	}
	klog.Infof("prometheus installed successfully in namespace %s", namespace)
	return nil
}

// RegisterSchemes implements [extensions.Extension].
func (p Prometheus) RegisterSchemes(_ context.Context, _ *runtime.Scheme) error {
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
//
// TODO: Instead of copying fixed secrets the idea is to expose a function for devs to add a workload or control plane cluster scrape config on demand
// by passing the cluster name which results in creating an access requests with the set of required permissions (see prometheus rbac), calling
// calling this secret transformation function, updating the config and reloading prometheus (e.g. using reloader).
func createPrometheusJobSecret(ctx context.Context, cfg *envconf.Config, srcRef, dest types.NamespacedName) error {
	src := &corev1.Secret{}
	if err := cfg.Client().Resources(srcRef.Namespace).Get(ctx, srcRef.Name, srcRef.Namespace, src); err != nil {
		return fmt.Errorf("failed to get kubeconfig secret %s: %w", srcRef, err)
	}

	kubeconfigBytes, ok := src.Data["kubeconfig"]
	if !ok {
		return fmt.Errorf("kubeconfig secret %s has no kubeconfig key", srcRef)
	}
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		return fmt.Errorf("failed to parse kubeconfig from secret %s: %w", srcRef, err)
	}

	var caData []byte
	switch {
	case len(restConfig.CAData) > 0:
		caData = restConfig.CAData
	case restConfig.CAFile != "":
		return fmt.Errorf("kubeconfig secret %s references a CA file path which is not supported in-cluster", srcRef)
	}

	tokenBytes := []byte(restConfig.BearerToken)
	if len(tokenBytes) == 0 && restConfig.BearerTokenFile != "" {
		return fmt.Errorf("kubeconfig secret %s references a bearer token file path which is not supported in-cluster", srcRef)
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
	return createIfNotExists(ctx, cfg, copy)
}

func rbac(ctx context.Context, c *envconf.Config, namespace string) error {
	rbacTemplate := internal.MustTmpFileFromEmbedFS(configFS, "config/rbac.yaml.tmpl")
	templateData := struct {
		Namespace string
	}{
		Namespace: namespace,
	}
	if _, err := resources.CreateObjectsFromTemplateFile(ctx, c.WithNamespace(namespace), rbacTemplate, templateData); err != nil {
		return fmt.Errorf("failed to create prometheus rbac: %w", err)
	}
	return nil
}

func configMap(ctx context.Context, c *envconf.Config, namespace string, update bool, remoteClusters []RemoteCluster) error {
	configTemplate := internal.MustTmpFileFromEmbedFS(configFS, "config/config.yaml.tmpl")
	templateData := struct {
		RemoteClusters []RemoteCluster
	}{
		RemoteClusters: remoteClusters,
	}
	if update {
		if err := resources.UpdateObjectsFromTemplateFile(ctx, c.WithNamespace(namespace), configTemplate, templateData); err != nil {
			return fmt.Errorf("failed to update prometheus config: %w", err)
		}
	} else {
		if _, err := resources.CreateObjectsFromTemplateFile(ctx, c.WithNamespace(namespace), configTemplate, templateData); err != nil {
			return fmt.Errorf("failed create prometheus config: %w", err)
		}
	}
	return nil
}

func deployment(ctx context.Context, c *envconf.Config, namespace, version string, update bool, remoteClusters []RemoteCluster) error {
	deployTemplate := internal.MustTmpFileFromEmbedFS(configFS, "config/deployment.yaml.tmpl")
	templateData := struct {
		Namespace      string
		Image          string
		RemoteClusters []RemoteCluster
	}{
		Namespace:      namespace,
		Image:          defaultImage + ":" + version,
		RemoteClusters: remoteClusters,
	}
	if update {
		if err := resources.UpdateObjectsFromTemplateFile(ctx, c.WithNamespace(namespace), deployTemplate, templateData); err != nil {
			return fmt.Errorf("failed to update prometheus deployment: %w", err)
		}
	} else {
		if _, err := resources.CreateObjectsFromTemplateFile(ctx, c.WithNamespace(namespace), deployTemplate, templateData); err != nil {
			return fmt.Errorf("failed to create prometheus deployment: %w", err)
		}
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
        resources: ["pods"]
        verbs: ["get", "list", "watch"]
`

// AddClusters adds a scrape job to the prometheus config for each given TargetCluster.
// For each cluster it creates an AccessRequest in the Prometheus namespace and updates the ConfigMap and Prometheus deployment to mount every remote kubeconfig.
func (p Prometheus) AddClusters(ctx context.Context, cfg *envconf.Config, targetClusters ...TargetCluster) error {
	namespace := p.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}
	version := p.Version
	if version == "" {
		version = defaultVersion
	}

	var additionalClusters []RemoteCluster
	for _, tc := range targetClusters {
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
			Namespace:               namespace,
		}
		arObj, err := resources.CreateObjectFromTemplate(ctx, cfg.WithNamespace(namespace), accessRequestTemplate, arData)
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
		rc := RemoteCluster{
			Name:             tc.Label,
			KubeconfigSecret: promSecretName,
		}
		if err := createPrometheusJobSecret(ctx, cfg, types.NamespacedName{Name: secretName, Namespace: namespace}, types.NamespacedName{Name: promSecretName, Namespace: namespace}); err != nil {
			return fmt.Errorf("failed to copy kubeconfig secret for cluster %s: %w", tc.Name, err)
		}
		additionalClusters = append(additionalClusters, rc)
	}

	allRemoteClusters := slices.Concat(p.RemoteClusters, additionalClusters)

	// when all cluster secrets have been created: update the prometheus config and deployment to mount any additional remote cluster secrets
	if err := configMap(ctx, cfg, namespace, true, allRemoteClusters); err != nil {
		return fmt.Errorf("failed to update prometheus config: %w", err)
	}
	if err := deployment(ctx, cfg, namespace, version, true, allRemoteClusters); err != nil {
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
