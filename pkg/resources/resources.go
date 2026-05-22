package resources

import (
	"context"
	"fmt"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/e2e-framework/klient/decoder"
	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/envconf"

	"github.com/openmcp-project/openmcp-testing/internal"
)

// DeleteObject deletes the passed in object if it exists
func DeleteObject(ctx context.Context, c *envconf.Config, obj k8s.Object, options ...wait.Option) error {
	err := c.Client().Resources().Get(ctx, obj.GetName(), obj.GetNamespace(), obj)
	if err != nil {
		return internal.IgnoreNotFound(err)
	}
	if err = c.Client().Resources().Delete(ctx, obj); err != nil {
		return internal.IgnoreNotFound(err)
	}
	if options != nil {
		return wait.For(conditions.New(c.Client().Resources()).ResourceDeleted(obj), options...)
	}
	return nil
}

// CreateObjectsFromTemplateFile creates objects by first applying the passed data to a template file on the file system
func CreateObjectsFromTemplateFile(ctx context.Context, cfg *envconf.Config, filePath string, data interface{}) (*unstructured.UnstructuredList, error) {
	manifest, err := internal.ExecTemplateFile(filePath, data)
	if err != nil {
		return nil, err
	}
	return createObjectsFromManifest(ctx, cfg, manifest)
}

// CreateObjectFromTemplate creates a single object by first applying the passed in data to a template
func CreateObjectFromTemplate(ctx context.Context, cfg *envconf.Config, template string, data interface{}) (*unstructured.Unstructured, error) {
	manifest, err := internal.ExecTemplate(template, data)
	if err != nil {
		return nil, err
	}
	obj := &unstructured.Unstructured{}
	err = decoder.DecodeString(manifest, obj, decoder.MutateNamespace(cfg.Namespace()))
	if err != nil {
		return nil, err
	}
	if err := CreateOrUpdate(ctx, cfg, obj); err != nil {
		return nil, err
	}
	return obj, nil
}

// CreateOrUpdate creates obj if it does not exist on the server; otherwise it
// copies obj's spec, labels, and annotations onto the live object and updates
// it. status is never touched. Idempotent re-applies that produce no semantic
// change skip the Update entirely (controllerutil.CreateOrUpdate's deep-equal
// optimization).
//
// The intent is to make install paths idempotent so that the test harness can
// be re-run against an existing cluster (see E2E_REUSE_CLUSTER) and propagate
// spec changes (e.g. a new image) without erroring on AlreadyExists.
func CreateOrUpdate(ctx context.Context, cfg *envconf.Config, obj client.Object) error {
	crClient, err := client.New(cfg.Client().RESTConfig(), client.Options{
		Scheme: cfg.Client().Resources().GetScheme(),
	})
	if err != nil {
		return fmt.Errorf("CreateOrUpdate: build client: %w", err)
	}
	desired := obj.DeepCopyObject().(client.Object)
	_, err = controllerutil.CreateOrUpdate(ctx, crClient, obj, func() error {
		return mergeDesiredIntoLive(desired, obj)
	})
	return err
}

func mergeDesiredIntoLive(desired, live client.Object) error {
	du, ok := desired.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("CreateOrUpdate: desired must be *unstructured.Unstructured, got %T", desired)
	}
	lu, ok := live.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("CreateOrUpdate: live must be *unstructured.Unstructured, got %T", live)
	}
	// controllerutil.CreateOrUpdate asserts that name and namespace are
	// unchanged between pre-Get and post-mutate. For cluster-scoped resources
	// (e.g. ClusterRoleBinding) the API server returns metadata.namespace=""
	// even when our caller stamped one via decoder.MutateNamespace, which
	// would otherwise trip the assertion. Restore both fields from desired
	// so the key invariant holds; cluster-scoped Updates ignore namespace.
	lu.SetName(du.GetName())
	lu.SetNamespace(du.GetNamespace())
	spec, found, err := unstructured.NestedFieldCopy(du.Object, "spec")
	if err != nil {
		return err
	}
	if found {
		lu.Object["spec"] = spec
	}
	lu.SetLabels(mergeStringMap(lu.GetLabels(), du.GetLabels()))
	lu.SetAnnotations(mergeStringMap(lu.GetAnnotations(), du.GetAnnotations()))
	return nil
}

func mergeStringMap(into, from map[string]string) map[string]string {
	if len(from) == 0 {
		return into
	}
	if into == nil {
		into = make(map[string]string, len(from))
	}
	for k, v := range from {
		into[k] = v
	}
	return into
}

func createObjectsFromManifest(ctx context.Context, cfg *envconf.Config, manifest string) (*unstructured.UnstructuredList, error) {
	r := strings.NewReader(manifest)
	list := &unstructured.UnstructuredList{}
	err := decoder.DecodeEach(ctx, r,
		func(ctx context.Context, obj k8s.Object) error {
			return createAndPopulateList(ctx, obj, list, cfg)
		}, decoder.MutateNamespace(cfg.Namespace()))
	return list, err
}

// CreateObjectsFromDir creates objects specified by a file on the file system
func CreateObjectsFromDir(ctx context.Context, cfg *envconf.Config, dir string) (*unstructured.UnstructuredList, error) {
	list := &unstructured.UnstructuredList{}
	err := decoder.DecodeEachFile(ctx, os.DirFS(dir), "*",
		func(ctx context.Context, obj k8s.Object) error {
			return createAndPopulateList(ctx, obj, list, cfg)
		}, decoder.MutateNamespace(cfg.Namespace()))
	return list, err
}

func createAndPopulateList(ctx context.Context, obj k8s.Object, list *unstructured.UnstructuredList, cfg *envconf.Config) error {
	u, err := internal.ToUnstructured(obj)
	if err != nil {
		return err
	}
	list.Items = append(list.Items, *u)
	klog.Infof("applying object (%s) %s/%s", obj.GetObjectKind().GroupVersionKind(), obj.GetNamespace(), obj.GetName())
	return CreateOrUpdate(ctx, cfg, u)
}
