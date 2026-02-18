package permissions

import "testing"

func TestDefinitionMapIncludesKiroTokenPermission(t *testing.T) {
	t.Parallel()

	key := "POST /v0/admin/tokens/kiro"
	if _, ok := DefinitionMap()[key]; !ok {
		t.Fatalf("DefinitionMap() missing permission key %q", key)
	}
}
