package internal

import (
	"io"
	"os"
	"strings"
	"text/template"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/e2e-framework/klient/k8s"
)

// ExecTemplate parses and executes textTemplate with the provided data
func ExecTemplate(textTemplate string, data interface{}) (string, error) {
	tmpl, err := template.New("t").Parse(textTemplate)
	if err != nil {
		return "", err
	}
	result := strings.Builder{}
	if err := tmpl.Execute(&result, data); err != nil {
		return "", err
	}
	return result.String(), nil
}

// ExecTemplateFile parses and executes a template referenced by a file with the provided data
func ExecTemplateFile(filePath string, data interface{}) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	bytes, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}
	return ExecTemplate(string(bytes), data)
}

// ToUnstructured converts a Object to Unstructured
func ToUnstructured(obj k8s.Object) (*unstructured.Unstructured, error) {
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{
		Object: u,
	}, nil
}

// IgnoreNotFound returns returns no error for IsNotFound
func IgnoreNotFound(err error) error {
	if errors.IsNotFound(err) {
		return nil
	}
	return err
}

// UnstructuredRef returns an empty object with its identifying properties set
func UnstructuredRef(name string, namespace string, gvk schema.GroupVersionKind) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetName(name)
	obj.SetNamespace(namespace)
	obj.SetGroupVersionKind(gvk)
	return obj
}
