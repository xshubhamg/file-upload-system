package docs_test

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/xshubhamg/file-upload/docs"
)

// TestOpenAPIContract keeps the embedded specification parseable and catches
// broken internal references before the service starts serving them.
func TestOpenAPIContract(t *testing.T) {
	var spec map[string]any
	if err := yaml.Unmarshal(docs.OpenAPI, &spec); err != nil {
		t.Fatalf("openapi.yaml is not valid YAML: %v", err)
	}

	if version, _ := spec["openapi"].(string); version != "3.0.3" {
		t.Fatalf("openapi version = %q, want 3.0.3", spec["openapi"])
	}
	if info, ok := spec["info"].(map[string]any); !ok || info["title"] == "" || info["version"] == "" {
		t.Fatalf("openapi info must define title and version: %#v", spec["info"])
	}

	paths, ok := spec["paths"].(map[string]any)
	if !ok || len(paths) == 0 {
		t.Fatalf("openapi paths must be a non-empty object: %#v", spec["paths"])
	}
	for path, raw := range paths {
		if !strings.HasPrefix(path, "/") {
			t.Errorf("path %q must start with /", path)
		}
		operations, ok := raw.(map[string]any)
		if !ok || len(operations) == 0 {
			t.Errorf("path %q has no operations", path)
			continue
		}
		for method, rawOperation := range operations {
			switch method {
			case "get", "post", "put", "patch", "delete", "options", "head", "trace":
			default:
				continue
			}
			operation, ok := rawOperation.(map[string]any)
			if !ok {
				t.Errorf("%s %s is not an object", method, path)
				continue
			}
			if responses, ok := operation["responses"].(map[string]any); !ok || len(responses) == 0 {
				t.Errorf("%s %s has no responses", method, path)
			}
		}
	}

	var refs []string
	collectRefs(spec, &refs)
	for _, ref := range refs {
		if !strings.HasPrefix(ref, "#/") {
			t.Errorf("external $ref %q is not supported by the embedded spec", ref)
			continue
		}
		if _, ok := resolveRef(spec, ref); !ok {
			t.Errorf("unresolved $ref %q", ref)
		}
	}
}

func collectRefs(value any, refs *[]string) {
	switch node := value.(type) {
	case map[string]any:
		for key, child := range node {
			if key == "$ref" {
				if ref, ok := child.(string); ok {
					*refs = append(*refs, ref)
				}
				continue
			}
			collectRefs(child, refs)
		}
	case []any:
		for _, child := range node {
			collectRefs(child, refs)
		}
	}
}

func resolveRef(spec map[string]any, ref string) (any, bool) {
	var current any = spec
	for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}
