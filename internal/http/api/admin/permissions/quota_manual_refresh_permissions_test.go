package permissions

import "testing"

func TestDefinitionMapIncludesQuotaManualRefreshPermissions(t *testing.T) {
	t.Parallel()

	definitionMap := DefinitionMap()
	requiredKeys := []string{
		"POST /v0/admin/quotas/manual-refresh",
		"GET /v0/admin/quotas/manual-refresh/:task_id",
	}

	for _, key := range requiredKeys {
		key := key
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			if _, ok := definitionMap[key]; !ok {
				t.Fatalf("DefinitionMap() missing permission key %q", key)
			}
		})
	}
}
