package auth

import (
	"context"
	"errors"
	"testing"

	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

func TestSelectorTokenInvalidPrefersHealthyAuth(t *testing.T) {
	selector := &Selector{}
	auths := []*coreauth.Auth{
		{ID: "auth-a", Status: coreauth.StatusActive, Metadata: map[string]any{"_sys_token_invalid": true}},
		{ID: "auth-b", Status: coreauth.StatusActive},
	}

	selected, errPick := selector.Pick(context.Background(), "", "", cliproxyexecutor.Options{}, auths)
	if errPick != nil {
		t.Fatalf("expected pick ok, got %v", errPick)
	}
	if selected == nil || selected.ID != "auth-b" {
		t.Fatalf("expected healthy auth selected, got %+v", selected)
	}
}

func TestSelectorTokenInvalidAllInvalidReturnsUnavailable(t *testing.T) {
	selector := &Selector{}
	auths := []*coreauth.Auth{
		{ID: "auth-a", Status: coreauth.StatusActive, Metadata: map[string]any{"_sys_token_invalid": true}},
		{ID: "auth-b", Status: coreauth.StatusActive, Metadata: map[string]any{"_sys_token_invalid": true}},
	}

	selected, errPick := selector.Pick(context.Background(), "", "", cliproxyexecutor.Options{}, auths)
	if selected != nil {
		t.Fatalf("expected no auth selected, got %+v", selected)
	}
	if errPick == nil {
		t.Fatalf("expected auth unavailable error, got nil")
	}

	var authErr *coreauth.Error
	if !errors.As(errPick, &authErr) {
		t.Fatalf("expected *coreauth.Error, got %T", errPick)
	}
	if authErr.Code != "auth_unavailable" {
		t.Fatalf("expected auth_unavailable, got %q", authErr.Code)
	}
}
