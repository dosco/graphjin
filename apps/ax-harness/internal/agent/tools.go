package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"unicode/utf8"

	"github.com/google/jsonschema-go/jsonschema"
)

// Tools are host-installed capabilities for one run.
type Tools struct {
	Capabilities []Capability
	ChildTools   []string // Host-admitted tools for one owned child agent; "*" admits all. Absent names and pause tools are skipped.
	Skills       []Skill  // Host-staged skill guides, found by skill search.
}

// Skill is one staged guide. Content is the SKILL.md body; Load reads it on
// demand when Content is empty, so thousands of guides cost only their index.
type Skill struct {
	Name        string
	Description string
	Content     string
	Load        func() (string, error)
}

// body returns the guide text, loading it when the host deferred the read.
func (s Skill) body() string {
	if s.Content != "" || s.Load == nil {
		return s.Content
	}
	text, err := s.Load()
	if err != nil {
		return ""
	}
	return text
}

// The Agent Skills format allows descriptions of up to 1,024 characters.
const (
	maxSkills           = 100_000
	maxSkillContent     = 64 << 10
	maxSkillDescription = 1024
)

var skillName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func ValidSkillName(name string) bool { return skillName.MatchString(name) }

func (t Tools) skills() ([]Skill, error) {
	if len(t.Skills) > maxSkills {
		return nil, fmt.Errorf("too many staged skills")
	}
	list := append([]Skill(nil), t.Skills...)
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	for i, skill := range list {
		if !skillName.MatchString(skill.Name) || utf8.RuneCountInString(skill.Description) > maxSkillDescription || len(skill.Content) > maxSkillContent ||
			(i > 0 && list[i-1].Name == skill.Name) {
			return nil, fmt.Errorf("invalid staged skill catalog")
		}
	}
	return list, nil
}

// Capability is installed by trusted host code or the host's MCP bridge for
// this run. A model or tool result can never add one.
type Capability struct {
	Name, Version, Origin, Effect, Description string
	InputSchema                                json.RawMessage
	Call                                       func(context.Context, json.RawMessage) (json.RawMessage, error)
}

type admittedTool struct {
	Capability
	schema *jsonschema.Resolved
}

func (c admittedTool) promptDescriptor(includeEffect bool) string {
	text := " Available JavaScript function " + c.Name + "(input): " + c.Description +
		" Input JSON schema: " + string(c.InputSchema) + "."
	if includeEffect {
		text += " Effect: " + c.Effect + "."
	}
	return text
}

var capabilityName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func ValidToolName(name string) bool { return capabilityName.MatchString(name) }

func (t Tools) admitted() ([]admittedTool, error) {
	list := append([]Capability(nil), t.Capabilities...)
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	seen := map[string]bool{}
	admitted := make([]admittedTool, 0, len(list))
	for _, cap := range list {
		if !capabilityName.MatchString(cap.Name) || seen[cap.Name] || cap.Version == "" || len(cap.Version) > 128 || cap.Origin == "" || len(cap.Origin) > 128 || len(cap.Description) > 1000 || cap.Description == "" || cap.Call == nil || len(cap.InputSchema) == 0 || len(cap.InputSchema) > 16384 || (cap.Effect != "read" && cap.Effect != "pause" && cap.Effect != "durable") {
			return nil, fmt.Errorf("invalid or duplicate capability")
		}
		seen[cap.Name] = true
		var schema jsonschema.Schema
		if err := json.Unmarshal(cap.InputSchema, &schema); err != nil {
			return nil, fmt.Errorf("invalid capability schema: %w", err)
		}
		resolved, err := schema.Resolve(nil)
		if err != nil {
			return nil, fmt.Errorf("unsupported capability schema: %w", err)
		}
		admitted = append(admitted, admittedTool{Capability: cap, schema: resolved})
	}
	return admitted, nil
}

// childTools returns the parent's tools the child may call. Only the parent
// turn can pause for the user, so pause tools stay with the parent.
func (t Tools) childTools(admitted []admittedTool) ([]admittedTool, error) {
	all := len(t.ChildTools) == 1 && t.ChildTools[0] == "*"
	wanted := make(map[string]bool, len(t.ChildTools))
	for _, name := range t.ChildTools {
		if all {
			break
		}
		if !ValidToolName(name) || wanted[name] {
			return nil, fmt.Errorf("invalid child capability")
		}
		wanted[name] = true
	}
	child := make([]admittedTool, 0, len(admitted))
	for _, capability := range admitted {
		if (!all && !wanted[capability.Name]) || capability.Effect == "pause" {
			continue
		}
		child = append(child, capability)
		delete(wanted, capability.Name)
	}
	return child, nil
}

func (c admittedTool) input(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 65536 {
		return "", fmt.Errorf("invalid capability input")
	}
	var decoded any
	if err = json.Unmarshal(raw, &decoded); err != nil || c.schema.Validate(decoded) != nil {
		return "", fmt.Errorf("capability input violates schema")
	}
	return string(raw), nil
}

func (c admittedTool) invoke(ctx context.Context, instruction string) (json.RawMessage, error) {
	return c.Call(ctx, json.RawMessage(instruction))
}
