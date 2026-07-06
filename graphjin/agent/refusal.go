package agent

import (
	"fmt"
	"strings"
)

type unblockStepSpec struct {
	step  UnblockStep
	roots []string
}

func (s *discoveryState) buildRefusal(resp Response) *Refusal {
	v, ok := s.primaryBlockingViolation()
	if !ok {
		v = blockingViolationFromErrors(resp.Errors)
	}
	if v.Code == "" {
		return nil
	}
	refusal := &Refusal{
		Code:              v.Code,
		BlockedAction:     blockedAction(v),
		Because:           refusalBecause(v),
		Unblock:           s.filteredUnblockSteps(unblockStepsForViolation(s, v)),
		LawfulAlternative: lawfulAlternativeForViolation(v),
		PolicyFinal:       policyFinalViolation(v.Code),
	}
	refusal.Retryable = !refusal.PolicyFinal
	if len(refusal.Unblock) == 0 && refusal.LawfulAlternative == "" {
		refusal.LawfulAlternative = "Use the visible GraphJin catalog/help tools to gather the missing evidence, then retry the request."
	}
	return refusal
}

func (s *discoveryState) primaryBlockingViolation() (protocolViolation, bool) {
	if s == nil {
		return protocolViolation{}, false
	}
	var best protocolViolation
	bestScore := -1
	for _, v := range s.violations {
		if !v.Blocking {
			continue
		}
		score := refusalPriority(v.Code)
		if bestScore < 0 || score > bestScore {
			best = v
			bestScore = score
		}
	}
	return best, bestScore >= 0
}

func blockingViolationFromErrors(errors []ErrorInfo) protocolViolation {
	for _, err := range errors {
		if err.Extensions == nil {
			continue
		}
		code, _ := err.Extensions["code"].(string)
		if code == "" {
			continue
		}
		tool, _ := err.Extensions["tool"].(string)
		details, _ := err.Extensions["details"].(map[string]any)
		return protocolViolation{Code: code, Message: err.Message, Tool: tool, Blocking: true, Details: details}
	}
	if len(errors) != 0 {
		return protocolViolation{Code: "agent_blocked", Message: errors[0].Message, Blocking: true}
	}
	return protocolViolation{}
}

func refusalPriority(code string) int {
	switch code {
	case "artifact_kind_locked", "access_unauthorized", "access_blocked", "authenticated_required", "identity_variable_missing", "capability_disabled", "capability-disabled":
		return 100
	case "catalog_seed_failed", "catalog_seed_required":
		return 90
	case "security_runtime_discovery_required":
		return 80
	case "mutation_evidence_required":
		return 70
	case "saved_query_detail_required":
		return 60
	case "raw_graphql_catalog_required", "model_discovery_required":
		return 50
	case "skill_evidence_required":
		return 40
	default:
		return 10
	}
}

func blockedAction(v protocolViolation) string {
	if strings.TrimSpace(v.Tool) != "" {
		return v.Tool
	}
	return "answer"
}

func refusalBecause(v protocolViolation) []string {
	if strings.TrimSpace(v.Message) == "" {
		return nil
	}
	return []string{v.Message}
}

func policyFinalViolation(code string) bool {
	switch code {
	case "artifact_kind_locked", "access_unauthorized", "access_blocked", "authenticated_required", "identity_variable_missing", "capability_disabled", "capability-disabled":
		return true
	default:
		return false
	}
}

func unblockStepsForViolation(s *discoveryState, v protocolViolation) []unblockStepSpec {
	switch v.Code {
	case "saved_query_detail_required":
		name := stringFromDetails(v.Details, "name")
		if name == "" {
			return catalogDiscoverySteps(s)
		}
		return []unblockStepSpec{{
			step: UnblockStep{
				Tool:   toolQueryCatalog,
				Args:   map[string]any{"id": "saved_query:" + name},
				Reason: "Inspect the saved query detail before executing it.",
			},
		}}
	case "mutation_evidence_required":
		tables := stringListFromDetails(v.Details, "tables")
		if len(tables) == 0 {
			return []unblockStepSpec{catalogSearchStep("mutation patterns and target table shape", "Find the mutation pattern and target table detail before executing a mutation.")}
		}
		steps := make([]unblockStepSpec, 0, len(tables))
		for _, table := range tables {
			steps = append(steps, catalogSearchStep(
				fmt.Sprintf("mutation pattern table %s", table),
				fmt.Sprintf("Gather mutation-shape evidence for %s before executing the write.", table),
			))
		}
		return steps
	case "security_runtime_discovery_required":
		return []unblockStepSpec{
			{step: UnblockStep{Tool: toolQueryCatalog, Args: map[string]any{"id": "help:security"}, Reason: "Inspect security guidance before write-capable GraphQL."}, roots: []string{systemRootSecurity}},
			{step: UnblockStep{Tool: toolQueryCatalog, Args: map[string]any{"id": "help:runtime"}, Reason: "Inspect runtime guidance before write-capable GraphQL."}, roots: []string{systemRootRuntime}},
		}
	case "skill_evidence_required":
		return skillEvidenceSteps(s, stringFromDetails(v.Details, "skill"))
	case "catalog_seed_failed", "catalog_seed_required", "model_discovery_required", "raw_graphql_catalog_required":
		return catalogDiscoverySteps(s)
	default:
		if policyFinalViolation(v.Code) {
			return nil
		}
		return catalogDiscoverySteps(s)
	}
}

func catalogDiscoverySteps(s *discoveryState) []unblockStepSpec {
	search := "GraphJin discovery"
	if s != nil && strings.TrimSpace(s.instruction) != "" {
		search = s.instruction
	}
	return []unblockStepSpec{{
		step: UnblockStep{
			Tool:   toolQueryCatalog,
			Args:   map[string]any{"search": search, "limit": 10},
			Reason: "Gather catalog evidence before answering or executing GraphQL.",
		},
	}, {
		step: UnblockStep{
			Tool:   toolGraphQLHelp,
			Args:   map[string]any{"for": "discovery"},
			Reason: "Use GraphJin help when the right catalog route is unclear.",
		},
	}}
}

func catalogSearchStep(search, reason string) unblockStepSpec {
	return unblockStepSpec{step: UnblockStep{
		Tool:   toolQueryCatalog,
		Args:   map[string]any{"search": search, "limit": 10},
		Reason: reason,
	}}
}

func skillEvidenceSteps(s *discoveryState, skill string) []unblockStepSpec {
	switch skill {
	case skillAdminWrite, skillAdminRead:
		return []unblockStepSpec{
			{step: UnblockStep{Tool: toolQueryCatalog, Args: map[string]any{"id": "help:security"}, Reason: "Read security posture before control-plane work."}, roots: []string{systemRootSecurity}},
			{step: UnblockStep{Tool: toolQueryCatalog, Args: map[string]any{"id": "help:runtime"}, Reason: "Read runtime status before control-plane work."}, roots: []string{systemRootRuntime}},
		}
	case skillWorkflowWrite, skillWorkflowRead:
		return []unblockStepSpec{{step: UnblockStep{Tool: toolQueryCatalog, Args: map[string]any{"where": map[string]any{"kind": map[string]any{"eq": "workflow"}}, "limit": 10}, Reason: "Inspect workflow rows and detail before executing or changing workflows."}, roots: []string{systemRootWorkflow}}}
	case skillWatchWrite, skillWatchRead:
		return []unblockStepSpec{
			{step: UnblockStep{Tool: toolQueryCatalog, Args: map[string]any{"search": "gj_watch watch capability", "limit": 10}, Reason: "Inspect the watch capability rows before reviewing or changing watches."}, roots: []string{systemRootWatch}},
			{step: UnblockStep{Tool: toolQueryCatalog, Args: map[string]any{"id": "help:security"}, Reason: "Read security posture before watch mutations (control-plane write gate)."}, roots: []string{systemRootSecurity}},
		}
	case skillCodeWrite, skillCodeRead:
		return []unblockStepSpec{catalogSearchStep("code source help gj_code", "Inspect code-source catalog guidance before answering or changing code.")}
	case skillDataWrite:
		return []unblockStepSpec{catalogSearchStep("mutation pattern target table", "Inspect mutation patterns and target table details before writing data.")}
	default:
		return catalogDiscoverySteps(s)
	}
}

func (s *discoveryState) filteredUnblockSteps(specs []unblockStepSpec) []UnblockStep {
	if len(specs) == 0 {
		return nil
	}
	profile := (*CapabilityProfile)(nil)
	if s != nil {
		profile = s.capabilities
	}
	out := make([]UnblockStep, 0, len(specs))
	for _, spec := range specs {
		if !profileAllowsTool(profile, spec.step.Tool) {
			continue
		}
		if !profileAllowsRoots(profile, spec.roots) {
			continue
		}
		out = append(out, spec.step)
	}
	return out
}

func profileAllowsTool(profile *CapabilityProfile, tool string) bool {
	tool = strings.TrimSpace(tool)
	if tool == "" {
		return false
	}
	if profile == nil {
		return true
	}
	if len(profile.AvailableTools) == 0 {
		return false
	}
	for _, available := range profile.AvailableTools {
		if strings.EqualFold(strings.TrimSpace(available), tool) {
			return true
		}
	}
	return false
}

func profileAllowsRoots(profile *CapabilityProfile, roots []string) bool {
	if len(roots) == 0 {
		return true
	}
	if profile == nil {
		return false
	}
	for _, root := range roots {
		if !profileHasSystemRoot(profile, root) {
			return false
		}
	}
	return true
}

func lawfulAlternativeForViolation(v protocolViolation) string {
	switch v.Code {
	case "authenticated_required", "identity_variable_missing":
		return "Authenticate with the required verified identity, or ask an authorized operator to run the action; catalog discovery will not satisfy this policy."
	case "access_unauthorized", "access_blocked", "capability_disabled", "capability-disabled":
		return "Choose an allowed GraphJin action or ask an administrator to change the policy; retrying the same blocked action will not succeed."
	case "saved_query_detail_required":
		return "Inspect the saved query detail first, then execute the approved saved query."
	case "mutation_evidence_required":
		return "Use catalog detail and validation evidence, or an approved saved mutation, before executing the write."
	case "security_runtime_discovery_required":
		return "Read visible security/runtime guidance first; if those roots are not visible, ask an authorized operator to perform the write."
	case "catalog_seed_failed", "catalog_seed_required", "model_discovery_required", "raw_graphql_catalog_required":
		return "Perform catalog-first discovery, then answer or run the GraphQL action from that evidence."
	case "skill_evidence_required":
		return "Gather the evidence required by the selected GraphJin skill before answering."
	default:
		if policyFinalViolation(v.Code) {
			return "Choose an allowed GraphJin action or ask an administrator to change the policy; retrying the same blocked action will not succeed."
		}
		return "Gather the missing GraphJin evidence through visible catalog/help tools, then retry."
	}
}

func stringFromDetails(details map[string]any, key string) string {
	if details == nil {
		return ""
	}
	value, ok := details[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func stringListFromDetails(details map[string]any, key string) []string {
	if details == nil {
		return nil
	}
	value, ok := details[key]
	if !ok || value == nil {
		return nil
	}
	values := anySlice(value)
	out := make([]string, 0, len(values))
	for _, value := range values {
		text := strings.TrimSpace(fmt.Sprint(value))
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}
