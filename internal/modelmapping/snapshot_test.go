package modelmapping

import (
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPIBusiness/internal/models"
)

func u64ptr(v uint64) *uint64 {
	value := v
	return &value
}

func TestStoreModelMappings_PrefersExplicitAliasForNewModelName(t *testing.T) {
	now := time.Now().UTC()
	StoreModelMappings(now, []models.ModelMapping{
		{
			ID:           10,
			Provider:     "claude",
			ModelName:    "real-model",
			NewModelName: "alias",
			Selector:     2,
			RateLimit:    7,
			UserGroupID:  models.UserGroupIDs{u64ptr(123)},
			IsEnabled:    true,
		},
		{
			ID:           11,
			Provider:     "claude",
			ModelName:    "alias",
			NewModelName: "alias",
			Selector:     0,
			RateLimit:    0,
			UserGroupID:  models.UserGroupIDs{u64ptr(456)},
			IsEnabled:    true,
		},
	})

	mappingID, selector, ok := LookupSelector("claude", "alias")
	if !ok {
		t.Fatalf("expected selector lookup to succeed")
	}
	if mappingID != 10 {
		t.Fatalf("expected mappingID=10, got %d", mappingID)
	}
	if selector != 2 {
		t.Fatalf("expected selector=2, got %d", selector)
	}

	allowed, okAllowed := LookupUserGroupIDs("claude", "alias")
	if !okAllowed {
		t.Fatalf("expected user group lookup to succeed")
	}
	values := allowed.Values()
	if len(values) != 1 || values[0] != 123 {
		t.Fatalf("expected allowed user groups [123], got %v", values)
	}
}

func TestStoreModelMappings_UsesIdentityAliasWhenNoExplicitAliasExists(t *testing.T) {
	now := time.Now().UTC()
	StoreModelMappings(now, []models.ModelMapping{
		{
			ID:           11,
			Provider:     "claude",
			ModelName:    "alias",
			NewModelName: "alias",
			Selector:     1,
			RateLimit:    0,
			UserGroupID:  models.UserGroupIDs{u64ptr(456)},
			IsEnabled:    true,
		},
	})

	mappingID, selector, ok := LookupSelector("claude", "alias")
	if !ok {
		t.Fatalf("expected selector lookup to succeed")
	}
	if mappingID != 11 {
		t.Fatalf("expected mappingID=11, got %d", mappingID)
	}
	if selector != 1 {
		t.Fatalf("expected selector=1, got %d", selector)
	}

	allowed, okAllowed := LookupUserGroupIDs("claude", "alias")
	if !okAllowed {
		t.Fatalf("expected user group lookup to succeed")
	}
	values := allowed.Values()
	if len(values) != 1 || values[0] != 456 {
		t.Fatalf("expected allowed user groups [456], got %v", values)
	}
}
