package permissions

import "testing"

func TestDefinitionMapIncludesModelMappingsProvidersPermission(t *testing.T) {
	t.Parallel()

	key := "GET /v0/admin/model-mappings/providers"
	if _, ok := DefinitionMap()[key]; !ok {
		t.Fatalf("DefinitionMap() missing permission key %q", key)
	}
}
