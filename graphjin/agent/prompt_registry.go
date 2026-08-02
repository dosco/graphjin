package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// PromptRegistryHash identifies the GraphJin-owned agent signature, runtime
// instructions, and built-in skill registry compiled into this binary. It is
// stable for identical registry content and is intended for run provenance.
func PromptRegistryHash() string {
	type skillSnapshot struct {
		ID               string   `json:"id"`
		Name             string   `json:"name"`
		Content          string   `json:"content"`
		Write            bool     `json:"write"`
		AdminOnly        bool     `json:"admin_only"`
		RequiresAnyRoots []string `json:"requires_any_roots,omitempty"`
		RequiresAllRoots []string `json:"requires_all_roots,omitempty"`
	}
	type registrySnapshot struct {
		AgentSignature              string          `json:"agent_signature"`
		RuntimeSeedInstructions     string          `json:"runtime_seed_instructions"`
		ExecutorHandoff             string          `json:"executor_handoff"`
		RuntimeInstructions         string          `json:"runtime_instructions"`
		SemanticCatalogInstructions string          `json:"semantic_catalog_instructions"`
		Skills                      []skillSnapshot `json:"skills"`
	}

	skills := make([]skillSnapshot, 0, len(builtinSkills))
	for _, skill := range builtinSkills {
		skills = append(skills, skillSnapshot{
			ID:               skill.id,
			Name:             skill.name,
			Content:          skill.content,
			Write:            skill.write,
			AdminOnly:        skill.adminOnly,
			RequiresAnyRoots: append([]string(nil), skill.requiresAnyRoots...),
			RequiresAllRoots: append([]string(nil), skill.requiresAllRoots...),
		})
	}
	payload, err := json.Marshal(registrySnapshot{
		AgentSignature:              agentSignature,
		RuntimeSeedInstructions:     runtimeSeedUsageInstructions,
		ExecutorHandoff:             executorHandoffInstructions,
		RuntimeInstructions:         runtimeUsageInstructions,
		SemanticCatalogInstructions: semanticCatalogUsageInstructions,
		Skills:                      skills,
	})
	if err != nil {
		panic(err) // The snapshot contains only JSON-native values.
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
