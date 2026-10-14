package http_test

import (
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.yaml.in/yaml/v3"

	"github.com/andrewhigh08/team-task-nexus/api"
	"github.com/andrewhigh08/team-task-nexus/internal/port"
)

// operationKeys are the keys of an OpenAPI path item that hold operations.
var operationKeys = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

// operation is the part of an OpenAPI operation the contract tests look at. Security is nil
// when the operation inherits the document's requirement and empty when it is public.
type operation struct {
	Security *[]map[string][]string `yaml:"security"`
}

// documentedOperations returns every operation of the OpenAPI document by "METHOD path".
func documentedOperations(t *testing.T) map[string]operation {
	t.Helper()
	var doc struct {
		OpenAPI string                          `yaml:"openapi"`
		Paths   map[string]map[string]yaml.Node `yaml:"paths"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(api.OpenAPI), &doc))
	require.True(t, strings.HasPrefix(doc.OpenAPI, "3.1."), "OpenAPI version %q", doc.OpenAPI)

	operations := make(map[string]operation)
	for path, item := range doc.Paths {
		for _, key := range operationKeys {
			node, ok := item[key]
			if !ok {
				continue
			}
			var op operation
			require.NoError(t, node.Decode(&op), "%s %s", key, path)
			operations[strings.ToUpper(key)+" "+path] = op
		}
	}
	return operations
}

// TestOpenAPI_DescribesExactlyTheServedRoutes holds the router and the contract together:
// every route must be documented, and every documented operation must be served. chi and
// OpenAPI write path parameters alike, so the paths compare as they are.
func TestOpenAPI_DescribesExactlyTheServedRoutes(t *testing.T) {
	t.Parallel()

	served := servedRoutes(t, newTestRouter(t, false, nil))
	documented := slices.Collect(maps.Keys(documentedOperations(t)))

	assert.ElementsMatch(t, documented, served)
}

// TestOpenAPI_DocumentsWhichOperationsArePublic checks the security requirement of every
// documented operation against the router: an operation documented as public must be served
// without a token, any other must answer 401 without one.
func TestOpenAPI_DocumentsWhichOperationsArePublic(t *testing.T) {
	t.Parallel()
	for route, op := range documentedOperations(t) {
		method, target := targetOf(route)
		t.Run(route, func(t *testing.T) {
			t.Parallel()
			tr := newTestRouter(t, false, nil)
			tr.limiter.EXPECT().Allow(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(port.RateDecision{Allowed: true}, nil).AnyTimes()

			rec := serve(tr, newRequest(t, method, target, "", http.NoBody))

			public := op.Security != nil && len(*op.Security) == 0
			if public {
				assert.NotEqual(t, http.StatusUnauthorized, rec.Code, "documented as public, but requires a token")
				return
			}
			assert.Equal(t, http.StatusUnauthorized, rec.Code, "documented as requiring a token, but served without one")
		})
	}
}

// TestOpenAPI_ReferencesResolve checks that every $ref of the document points at a component
// it defines.
func TestOpenAPI_ReferencesResolve(t *testing.T) {
	t.Parallel()
	var doc map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(api.OpenAPI), &doc))

	refs := collectRefs(doc)

	require.NotEmpty(t, refs)
	for _, ref := range refs {
		_, ok := resolve(doc, ref)
		assert.True(t, ok, "unresolved $ref %s", ref)
	}
}

// collectRefs returns the values of every $ref key in node.
func collectRefs(node any) []string {
	var refs []string
	switch v := node.(type) {
	case map[string]any:
		for key, child := range v {
			if ref, ok := child.(string); ok && key == "$ref" {
				refs = append(refs, ref)
				continue
			}
			refs = append(refs, collectRefs(child)...)
		}
	case []any:
		for _, child := range v {
			refs = append(refs, collectRefs(child)...)
		}
	}
	return refs
}

// resolve follows a local reference such as "#/components/schemas/Task" through doc.
func resolve(doc map[string]any, ref string) (any, bool) {
	pointer, ok := strings.CutPrefix(ref, "#/")
	if !ok {
		return nil, false
	}
	var node any = doc
	for name := range strings.SplitSeq(pointer, "/") {
		m, ok := node.(map[string]any)
		if !ok {
			return nil, false
		}
		if node, ok = m[name]; !ok {
			return nil, false
		}
	}
	return node, true
}
