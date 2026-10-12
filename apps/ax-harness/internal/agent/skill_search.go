package agent

import (
	"context"
	"encoding/json"
	"strings"

	ax "github.com/ax-llm/ax/packages/go"
)

// SkillSearchTool is the host's semantic skill index. The harness calls it for
// discover(); the model never sees it as a tool.
const SkillSearchTool = "mcp_neko_guides_find"

const (
	skillMatchesPerSearch = 2
	maxSkillMatches       = 5
	shortlistSize         = 8
	// MiniLM similarity below which a guide is unrelated to the request.
	shortlistMinScore = 0.2
)

// skillShortlist names the guides closest in meaning to the user's message, so
// the model sees them before its first step and loads the one that fits.
// skillStubs seed the executor's loaded guides with one line per shortlisted
// skill; discover() of the same name replaces a stub with the full guide.
func skillStubs(skills []Skill, names []string) []ax.Value {
	described := make(map[string]string, len(skills))
	for _, skill := range skills {
		described[skill.Name] = skill.Description
	}
	stubs := make([]ax.Value, 0, len(names))
	for _, name := range names {
		stubs = append(stubs, ax.Object("id", name, "name", name, "content",
			"Guide available for this request: "+described[name]+" Load the full guide with discover({ skills: ['"+name+"'] }) before you start when it fits the task."))
	}
	return stubs
}

func skillShortlist(ctx context.Context, search *admittedTool, skills []Skill, query string) (string, []string, error) {
	if search == nil || strings.TrimSpace(query) == "" {
		return "", nil, nil
	}
	input, _ := json.Marshal(map[string]any{"query": query, "limit": shortlistSize})
	raw, err := search.Call(ctx, input)
	if err != nil {
		return "", nil, err
	}
	described := make(map[string]string, len(skills))
	for _, skill := range skills {
		described[skill.Name] = skill.Description
	}
	var lines, names []string
	for _, match := range skillMatches(raw) {
		description, staged := described[match.Name]
		if staged && match.Score >= shortlistMinScore {
			lines = append(lines, "- "+match.Name+" — "+description)
			names = append(names, match.Name)
		}
	}
	if len(lines) == 0 {
		return "", nil, nil
	}
	return "\n\nSkill guides that may fit this request. Load the one that fits with discover() before you start, and follow it:\n" + strings.Join(lines, "\n"), names, nil
}

// searchSkills asks the host index for the guides closest in meaning to each
// discover() query and returns the staged guides it names, in rank order.
func searchSkills(ctx context.Context, search *admittedTool, skills []Skill, searches []ax.Value) []Skill {
	if search == nil {
		return nil
	}
	staged := make(map[string]Skill, len(skills))
	for _, skill := range skills {
		staged[skill.Name] = skill
	}
	var found []Skill
	seen := map[string]bool{}
	for _, value := range searches {
		query := searchText(value)
		if query == "" {
			continue
		}
		input, _ := json.Marshal(map[string]any{"query": query, "limit": skillMatchesPerSearch})
		raw, err := search.Call(ctx, input)
		if err != nil {
			continue
		}
		for _, match := range skillMatches(raw) {
			name := match.Name
			skill, ok := staged[name]
			if ok && !seen[name] && len(found) < maxSkillMatches {
				seen[name] = true
				found = append(found, skill)
			}
		}
	}
	return found
}

type skillMatch struct {
	Name  string  `json:"name"`
	Score float64 `json:"score"`
}

// skillMatches reads {"skills":[{"name":...,"score":...}]} from an MCP text result.
func skillMatches(raw json.RawMessage) []skillMatch {
	var envelope struct {
		Content []json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return nil
	}
	var matches []skillMatch
	for _, item := range envelope.Content {
		var text string
		if json.Unmarshal(item, &text) != nil {
			var block struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(item, &block) != nil {
				continue
			}
			text = block.Text
		}
		var result struct {
			Skills []skillMatch `json:"skills"`
		}
		if json.Unmarshal([]byte(text), &result) == nil {
			matches = append(matches, result.Skills...)
		}
	}
	return matches
}

func searchText(search ax.Value) string {
	switch v := search.(type) {
	case string:
		return v
	case map[string]ax.Value:
		for _, key := range []string{"query", "search", "name", "id"} {
			if text, ok := v[key].(string); ok {
				return text
			}
		}
	}
	return ""
}
