package permissions

import "testing"

func TestDefinitionMapIncludesKimiTokenPermission(t *testing.T) {
	t.Parallel()

	key := "POST /v0/admin/tokens/kimi"
	if _, ok := DefinitionMap()[key]; !ok {
		t.Fatalf("DefinitionMap() missing permission key %q", key)
	}
}

func TestDefinitionMapIncludesGitHubCopilotTokenPermission(t *testing.T) {
	t.Parallel()

	key := "POST /v0/admin/tokens/github-copilot"
	if _, ok := DefinitionMap()[key]; !ok {
		t.Fatalf("DefinitionMap() missing permission key %q", key)
	}
}

func TestDefinitionMapIncludesKiloTokenPermission(t *testing.T) {
	t.Parallel()

	key := "POST /v0/admin/tokens/kilo"
	if _, ok := DefinitionMap()[key]; !ok {
		t.Fatalf("DefinitionMap() missing permission key %q", key)
	}
}
