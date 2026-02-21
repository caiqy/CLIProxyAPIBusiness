package permissions

import "testing"

func TestDefinitionMapIncludesDashboardTransactionRequestLogPermission(t *testing.T) {
	t.Parallel()

	key := "GET /v0/admin/dashboard/transactions/:id/request-log"
	if _, ok := DefinitionMap()[key]; !ok {
		t.Fatalf("DefinitionMap() missing permission key %q", key)
	}
}
