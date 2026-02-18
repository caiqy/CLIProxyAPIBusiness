package permissions

import "testing"

func TestDefinitionMapIncludesAuthFilesImportByProviderPermission(t *testing.T) {
	t.Parallel()

	key := "POST /v0/admin/auth-files/import-by-provider"
	if _, ok := DefinitionMap()[key]; !ok {
		t.Fatalf("DefinitionMap() missing permission key %q", key)
	}
}
