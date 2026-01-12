package modelregistry

import (
	"strings"
	"sync"

	sdkcliproxy "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy"
)

// Store maintains in-memory model info indexed by provider and client.
type Store struct {
	mu sync.RWMutex

	// providerClientModels stores the raw model infos grouped by provider and clientID.
	// provider -> clientID -> lower(modelID) -> ModelInfo
	providerClientModels map[string]map[string]map[string]*sdkcliproxy.ModelInfo

	// providerModels stores the merged model infos per provider.
	// provider -> lower(modelID) -> ModelInfo
	providerModels map[string]map[string]*sdkcliproxy.ModelInfo
}

// NewStore constructs a Store.
func NewStore() *Store {
	return &Store{
		providerClientModels: make(map[string]map[string]map[string]*sdkcliproxy.ModelInfo),
		providerModels:       make(map[string]map[string]*sdkcliproxy.ModelInfo),
	}
}

// Upsert replaces model infos for a provider/client and rebuilds merged views.
func (s *Store) Upsert(provider, clientID string, infos []*sdkcliproxy.ModelInfo) {
	if s == nil {
		return
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	clientID = strings.TrimSpace(clientID)
	if provider == "" || clientID == "" {
		return
	}

	next := make(map[string]*sdkcliproxy.ModelInfo)
	for _, info := range infos {
		if info == nil {
			continue
		}
		id := strings.TrimSpace(info.ID)
		if id == "" {
			continue
		}
		key := strings.ToLower(id)
		cloned := cloneModelInfo(info)
		cloned.ID = id
		next[key] = cloned
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	byClient := s.providerClientModels[provider]
	if byClient == nil {
		byClient = make(map[string]map[string]*sdkcliproxy.ModelInfo)
		s.providerClientModels[provider] = byClient
	}
	byClient[clientID] = next

	s.rebuildProviderLocked(provider)
}

// Remove deletes model infos for a provider/client and rebuilds merged views.
func (s *Store) Remove(provider, clientID string) {
	if s == nil {
		return
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	clientID = strings.TrimSpace(clientID)
	if provider == "" || clientID == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	byClient := s.providerClientModels[provider]
	if byClient == nil {
		return
	}
	delete(byClient, clientID)
	if len(byClient) == 0 {
		delete(s.providerClientModels, provider)
		delete(s.providerModels, provider)
		return
	}

	s.rebuildProviderLocked(provider)
}

// GetByProviderModelID returns a cloned model info by provider and model ID.
func (s *Store) GetByProviderModelID(provider, modelID string) *sdkcliproxy.ModelInfo {
	if s == nil {
		return nil
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	modelID = strings.TrimSpace(modelID)
	if provider == "" || modelID == "" {
		return nil
	}

	key := strings.ToLower(modelID)

	s.mu.RLock()
	info := s.providerModels[provider][key]
	s.mu.RUnlock()
	if info == nil {
		return nil
	}
	return cloneModelInfo(info)
}

// SnapshotAll returns a de-duplicated list of models across providers.
func (s *Store) SnapshotAll() []*sdkcliproxy.ModelInfo {
	if s == nil {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	seen := make(map[string]struct{})
	out := make([]*sdkcliproxy.ModelInfo, 0)
	for _, modelsByID := range s.providerModels {
		for key, info := range modelsByID {
			if info == nil {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, cloneModelInfo(info))
		}
	}
	return out
}

// SnapshotByProvider returns a copy of all models grouped by provider.
func (s *Store) SnapshotByProvider() map[string][]*sdkcliproxy.ModelInfo {
	if s == nil {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make(map[string][]*sdkcliproxy.ModelInfo, len(s.providerModels))
	for provider, modelsByID := range s.providerModels {
		list := make([]*sdkcliproxy.ModelInfo, 0, len(modelsByID))
		for _, info := range modelsByID {
			if info == nil {
				continue
			}
			list = append(list, cloneModelInfo(info))
		}
		if len(list) > 0 {
			out[provider] = list
		}
	}
	return out
}

// rebuildProviderLocked merges client-specific models into provider-level views.
func (s *Store) rebuildProviderLocked(provider string) {
	if s == nil {
		return
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return
	}

	byClient := s.providerClientModels[provider]
	if byClient == nil {
		delete(s.providerModels, provider)
		return
	}

	merged := make(map[string]*sdkcliproxy.ModelInfo)
	for _, modelsByID := range byClient {
		for key, info := range modelsByID {
			if info == nil {
				continue
			}
			if _, exists := merged[key]; exists {
				continue
			}
			merged[key] = info
		}
	}

	if len(merged) == 0 {
		delete(s.providerModels, provider)
		return
	}
	s.providerModels[provider] = merged
}

// cloneModelInfo deep-copies a model info struct.
func cloneModelInfo(info *sdkcliproxy.ModelInfo) *sdkcliproxy.ModelInfo {
	if info == nil {
		return nil
	}

	cloned := *info
	if len(info.SupportedGenerationMethods) > 0 {
		cloned.SupportedGenerationMethods = append([]string(nil), info.SupportedGenerationMethods...)
	}
	if len(info.SupportedParameters) > 0 {
		cloned.SupportedParameters = append([]string(nil), info.SupportedParameters...)
	}
	if info.Thinking != nil {
		t := *info.Thinking
		if len(info.Thinking.Levels) > 0 {
			t.Levels = append([]string(nil), info.Thinking.Levels...)
		}
		cloned.Thinking = &t
	}
	return &cloned
}
