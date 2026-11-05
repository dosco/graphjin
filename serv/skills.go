package serv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

// A skill is an artifact kind: a named, reusable instruction a user writes
// for the lead agent ("how we run the weekly churn review"). The lead sees
// each visible skill's name and description and loads its content when a
// request matches. Skills follow the memory tiers: an "observed" skill is
// personal (owner-only); "approved" shares it with the owner's account. They
// are read and written like any gj_artifacts row, so a GUI needs only GraphQL.

const (
	artifactKindSkill = "skill"

	maxSkillNameBytes        = 64
	maxSkillDescriptionBytes = 200
	maxSkillContentBytes     = 8 * 1024
	maxSkillsPerOwner        = 50
)

var errSkillNotFound = errors.New("skill_not_found")

// Skills are a lead feature: only the lead reads them.
func (s *graphjinService) skillsEnabled() bool {
	return s != nil && s.conf != nil && s.conf.Core.Artifacts.Enabled && s.conf.Agent.Lead.Enabled
}

func (h artifactControlPlane) upsertSkill(ctx context.Context, root core.ManagedMutationRoot, existing map[string]any) (map[string]any, error) {
	s := h.service
	if !s.skillsEnabled() {
		return nil, fmt.Errorf("skills are disabled: set agent.lead.enabled")
	}
	actorID, ok := artifactUserID(ctx)
	if !ok {
		return nil, fmt.Errorf("skill write requires user identity")
	}
	if err := s.checkArtifactKindWritable(artifactKindSkill); err != nil {
		return nil, err
	}
	for field := range root.Input {
		switch field {
		case "kind", "name", "description", "content", "tier", "id":
		default:
			return nil, fmt.Errorf("skill %s is not writable", field)
		}
	}
	if existing != nil {
		if normalizeArtifactKind(stringMapValue(existing, "kind")) != artifactKindSkill {
			return nil, fmt.Errorf("skill update target is not a skill")
		}
		if !s.identityRoleIsAdmin(ctx) && stringMapValue(existing, "owner_id") != actorID {
			return nil, errSkillNotFound
		}
	} else if root.Operation == "update" {
		return nil, errSkillNotFound
	}

	name := stringMapValue(existing, "name")
	if _, supplied := root.Input["name"]; supplied || existing == nil {
		name = strings.Join(strings.Fields(stringInput(root.Input, "name", "")), " ")
	}
	if name == "" {
		return nil, fmt.Errorf("skill name is required")
	}
	if len(name) > maxSkillNameBytes {
		return nil, fmt.Errorf("skill name exceeds %d bytes", maxSkillNameBytes)
	}
	description := skillDescription(existing)
	if _, supplied := root.Input["description"]; supplied {
		description = strings.Join(strings.Fields(stringInput(root.Input, "description", "")), " ")
	}
	if description == "" {
		return nil, fmt.Errorf("skill description is required: one line saying when to use it")
	}
	if len(description) > maxSkillDescriptionBytes {
		return nil, fmt.Errorf("skill description exceeds %d bytes", maxSkillDescriptionBytes)
	}
	content := stringMapValue(existing, "content")
	if _, supplied := root.Input["content"]; supplied {
		content = strings.TrimSpace(stringInput(root.Input, "content", ""))
	}
	if content == "" {
		return nil, fmt.Errorf("skill content is required")
	}
	if len(content) > maxSkillContentBytes {
		return nil, fmt.Errorf("skill content exceeds %d bytes", maxSkillContentBytes)
	}
	tier := annotationTierObserved
	if existing != nil {
		tier = normalizeAnnotationTier(stringMapValue(existing, "tier"))
	}
	if _, supplied := root.Input["tier"]; supplied {
		var err error
		if tier, err = parseAnnotationTier(stringInput(root.Input, "tier", "")); err != nil {
			return nil, err
		}
	} else if existing != nil && (content != stringMapValue(existing, "content") || description != skillDescription(existing)) {
		// Editing a shared skill takes it back to personal until republished,
		// as with team memory and shared agent profiles.
		tier = annotationTierObserved
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	ownerID := actorID
	accountID, _ := identityVarString(ctx, "account_id")
	id, createdAt, revision := "", now, int64(1)
	approvedBy, approvedAt := "", ""
	if existing != nil {
		ownerID = stringMapValue(existing, "owner_id")
		accountID = stringMapValue(existing, "account_id")
		id = stringMapValue(existing, "id")
		createdAt = stringMapValue(existing, "created_at")
		revision = int64MapValue(existing, "revision") + 1
		approvedBy = stringMapValue(existing, "approved_by")
		approvedAt = stringMapValue(existing, "approved_at")
	} else {
		id = artifactKindSkill + ":" + taskRandomHex()
	}
	if err := s.checkSkillOwnerRules(ctx, ownerID, id, name, existing == nil); err != nil {
		return nil, err
	}
	visibility := "user"
	if tier == annotationTierApproved {
		visibility = "account"
		if approvedBy == "" {
			approvedBy, approvedAt = actorID, now
		}
	} else {
		approvedBy, approvedAt = "", ""
	}
	metadata, _ := json.Marshal(map[string]string{"description": description})
	input := map[string]any{
		"id": id, "name": name, "kind": artifactKindSkill, "path": "", "source": "database",
		"visibility": visibility, "read_only": false, "account_id": accountID, "owner_id": ownerID,
		"content": content, "content_json": nil, "metadata_json": string(metadata),
		"content_hash": hashString(name + "\x00" + description + "\x00" + content), "status": "approved", "target_ref": "", "tier": tier,
		"task_id": "", "approved_by": approvedBy, "approved_at": approvedAt,
		"revision": revision, "created_at": createdAt, "updated_at": now,
	}
	var rows []map[string]any
	var err error
	if existing == nil {
		rows, err = s.internalStoreMutationRows(ctx, "artifacts", `insert: $input`, artifactStoreFields, map[string]any{"input": input})
	} else {
		update := cloneMap(input)
		for _, field := range []string{"id", "account_id", "owner_id", "created_at"} {
			delete(update, field)
		}
		rows, err = s.internalStoreMutationRows(ctx, "artifacts", `where: { id: { eq: $id } }, update: $input`, artifactStoreFields, map[string]any{"id": id, "input": update})
	}
	if err != nil {
		return nil, err
	}
	if err := s.bumpArtifactRevision(ctx, "artifacts"); err != nil {
		return nil, err
	}
	s.markArtifactChanged("skill mutation")
	admin := s.identityRoleIsAdmin(ctx)
	if len(rows) != 0 {
		return artifactProjectionRow(rows[0], admin), nil
	}
	return artifactProjectionRow(input, admin), nil
}

// checkSkillOwnerRules keeps an owner's skill names unique, since the lead
// loads a skill by name, and caps how many one owner keeps.
func (s *graphjinService) checkSkillOwnerRules(ctx context.Context, ownerID, id, name string, creating bool) error {
	rows, err := s.internalStoreAllRows(ctx, "artifacts", `where: { kind: { eq: "skill" }, owner_id: { eq: $owner_id } }`,
		"id name", map[string]any{"owner_id": ownerID})
	if err != nil {
		return err
	}
	if creating && len(rows) >= maxSkillsPerOwner {
		return fmt.Errorf("skill limit reached: delete a skill before adding another (limit %d)", maxSkillsPerOwner)
	}
	for _, row := range rows {
		if stringMapValue(row, "id") != id && strings.EqualFold(stringMapValue(row, "name"), name) {
			return fmt.Errorf("a skill named %q already exists", name)
		}
	}
	return nil
}

func skillDescription(row map[string]any) string {
	var meta struct {
		Description string `json:"description"`
	}
	if raw := jsonMapString(row, "metadata_json"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &meta)
	}
	return meta.Description
}
