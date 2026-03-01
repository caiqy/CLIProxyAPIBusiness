package permissions

import "testing"

func TestDefinitionMapIncludesAuthFilesModelPresetsPermission(t *testing.T) {
	t.Parallel()

	key := "GET /v0/admin/auth-files/model-presets"
	if _, ok := DefinitionMap()[key]; !ok {
		t.Fatalf("DefinitionMap() missing permission key %q", key)
	}
}
