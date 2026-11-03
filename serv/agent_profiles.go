package serv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/auth/v3"
	"github.com/dosco/graphjin/core/v3"
)

// An agent profile is an artifact kind: a named agent with standing
// instructions, like a personal bot. It follows the memory tiers: an
// "observed" profile is personal (owner-only); "approved" shares it with the
// owner's account. Each user gets one home thread per profile they can see,
// and runs on a thread whose profile is visible to the caller carry the
// profile's instructions.

const (
	artifactKindAgentProfile = "agent_profile"

	maxProfileNameBytes         = 80
	maxProfileIconBytes         = 64
	maxProfileInstructionsBytes = 2 * 1024
	maxProfilesPerOwner         = 50
)

var errProfileNotFound = errors.New("agent_profile_not_found")

// Profiles live beside threads: a profile without a home thread is no use.
func (s *graphjinService) profilesEnabled() bool {
	return s.threadsEnabled()
}

func (h artifactControlPlane) upsertAgentProfile(ctx context.Context, root core.ManagedMutationRoot, existing map[string]any) (map[string]any, error) {
	s := h.service
	if !s.profilesEnabled() {
		return nil, fmt.Errorf("agent profiles are disabled: set threads.enabled")
	}
	actorID, ok := artifactUserID(ctx)
	if !ok {
		return nil, fmt.Errorf("agent profile write requires user identity")
	}
	if err := s.checkArtifactKindWritable(artifactKindAgentProfile); err != nil {
		return nil, err
	}
	for field := range root.Input {
		switch field {
		case "kind", "name", "instructions", "content", "icon", "tier", "id":
		default:
			return nil, fmt.Errorf("agent profile %s is not writable", field)
		}
	}
	if existing != nil {
		if normalizeArtifactKind(stringMapValue(existing, "kind")) != artifactKindAgentProfile {
			return nil, fmt.Errorf("agent profile update target is not an agent profile")
		}
		if !s.identityRoleIsAdmin(ctx) && stringMapValue(existing, "owner_id") != actorID {
			return nil, errProfileNotFound
		}
	} else if root.Operation == "update" {
		return nil, errProfileNotFound
	}

	name := stringMapValue(existing, "name")
	if _, supplied := root.Input["name"]; supplied || existing == nil {
		name = strings.Join(strings.Fields(stringInput(root.Input, "name", "")), " ")
	}
	if name == "" {
		return nil, fmt.Errorf("agent profile name is required")
	}
	if len(name) > maxProfileNameBytes {
		return nil, fmt.Errorf("agent profile name exceeds %d bytes", maxProfileNameBytes)
	}
	// instructions is the REST name; content is the gj_artifacts column.
	instructions := stringMapValue(existing, "content")
	for _, field := range []string{"content", "instructions"} {
		if _, supplied := root.Input[field]; supplied {
			instructions = strings.TrimSpace(stringInput(root.Input, field, ""))
		}
	}
	if len(instructions) > maxProfileInstructionsBytes {
		return nil, fmt.Errorf("agent profile instructions exceed %d bytes", maxProfileInstructionsBytes)
	}
	icon := profileIcon(existing)
	if _, supplied := root.Input["icon"]; supplied {
		icon = strings.TrimSpace(stringInput(root.Input, "icon", ""))
	}
	if len(icon) > maxProfileIconBytes {
		return nil, fmt.Errorf("agent profile icon exceeds %d bytes", maxProfileIconBytes)
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
	} else if existing != nil && instructions != stringMapValue(existing, "content") {
		// Changing a shared profile's instructions takes it back to personal
		// until republished, as with team memory.
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
		if err := s.enforceProfileLimit(ctx, actorID); err != nil {
			return nil, err
		}
		id = artifactKindAgentProfile + ":" + taskRandomHex()
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
	metadata := ""
	if icon != "" {
		raw, _ := json.Marshal(map[string]string{"icon": icon})
		metadata = string(raw)
	}
	input := map[string]any{
		"id": id, "name": name, "kind": artifactKindAgentProfile, "path": "", "source": "database",
		"visibility": visibility, "read_only": false, "account_id": accountID, "owner_id": ownerID,
		"content": instructions, "content_json": nil, "metadata_json": nullableJSONString(metadata),
		"content_hash": hashString(name + "\x00" + instructions), "status": "approved", "target_ref": "", "tier": tier,
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
	s.markArtifactChanged("agent profile mutation")
	if len(rows) != 0 {
		return profileAPIRow(rows[0], actorID), nil
	}
	return profileAPIRow(input, actorID), nil
}

func (s *graphjinService) enforceProfileLimit(ctx context.Context, ownerID string) error {
	rows, err := s.internalStoreRows(ctx, "artifacts",
		fmt.Sprintf(`where: { kind: { eq: "agent_profile" }, owner_id: { eq: $owner_id } }, limit: %d`, maxProfilesPerOwner),
		"id", map[string]any{"owner_id": ownerID})
	if err != nil {
		return err
	}
	if len(rows) >= maxProfilesPerOwner {
		return fmt.Errorf("agent profile limit reached: delete an agent before adding another (limit %d)", maxProfilesPerOwner)
	}
	return nil
}

// visibleProfileRows returns the caller's own profiles plus the profiles
// shared with the caller's account.
func (s *graphjinService) visibleProfileRows(ctx context.Context) ([]map[string]any, error) {
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return nil, nil
	}
	rows, err := s.internalStoreAllRows(ctx, "artifacts", `where: { kind: { eq: "agent_profile" }, owner_id: { eq: $owner_id } }`,
		artifactStoreFields, map[string]any{"owner_id": ownerID})
	if err != nil {
		return nil, err
	}
	if accountID, _ := identityVarString(ctx, "account_id"); accountID != "" {
		team, err := s.internalStoreAllRows(ctx, "artifacts",
			`where: { kind: { eq: "agent_profile" }, tier: { eq: "approved" }, account_id: { eq: $account_id } }`,
			artifactStoreFields, map[string]any{"account_id": accountID})
		if err != nil {
			return nil, err
		}
		for _, row := range team {
			if stringMapValue(row, "owner_id") != ownerID {
				rows = append(rows, row)
			}
		}
	}
	return rows, nil
}

// visibleProfileRow returns one profile the caller may use, or nil.
func (s *graphjinService) visibleProfileRow(ctx context.Context, id string) (map[string]any, error) {
	id = strings.TrimSpace(id)
	ownerID, ok := artifactUserID(ctx)
	if id == "" || !ok {
		return nil, nil
	}
	row, err := s.internalArtifactStoreRow(ctx, id)
	if err != nil || row == nil || normalizeArtifactKind(stringMapValue(row, "kind")) != artifactKindAgentProfile {
		return nil, err
	}
	if stringMapValue(row, "owner_id") == ownerID {
		return row, nil
	}
	accountID, _ := identityVarString(ctx, "account_id")
	if accountID != "" && stringMapValue(row, "account_id") == accountID &&
		normalizeAnnotationTier(stringMapValue(row, "tier")) == annotationTierApproved {
		return row, nil
	}
	return nil, nil
}

// requestProfile is the profile instructions a run on thread carries: none
// unless the thread names a profile the caller can see.
func (s *graphjinService) requestProfile(ctx context.Context, thread map[string]any) *gjagent.ProfileInstructions {
	profileID := stringMapValue(thread, "profile")
	if profileID == "" || !s.profilesEnabled() {
		return nil
	}
	row, err := s.visibleProfileRow(ctx, profileID)
	if err != nil && s.log != nil {
		s.log.Warnf("thread %s: agent profile skipped: %v", stringMapValue(thread, "id"), err)
	}
	if row == nil {
		return nil
	}
	return &gjagent.ProfileInstructions{Name: stringMapValue(row, "name"), Instructions: stringMapValue(row, "content")}
}

func profileIcon(row map[string]any) string {
	var meta struct {
		Icon string `json:"icon"`
	}
	if raw := jsonMapString(row, "metadata_json"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &meta)
	}
	return meta.Icon
}

func profileAPIRow(row map[string]any, viewerID string) map[string]any {
	scope := "personal"
	if normalizeAnnotationTier(stringMapValue(row, "tier")) == annotationTierApproved {
		scope = "team"
	}
	return map[string]any{
		"id":           stringMapValue(row, "id"),
		"name":         stringMapValue(row, "name"),
		"instructions": stringMapValue(row, "content"),
		"icon":         profileIcon(row),
		"scope":        scope,
		"tier":         normalizeAnnotationTier(stringMapValue(row, "tier")),
		"mine":         stringMapValue(row, "owner_id") == viewerID,
		"owner_ref":    safeArtifactIdentity(stringMapValue(row, "owner_id"), false),
		"created_at":   stringMapValue(row, "created_at"),
		"updated_at":   stringMapValue(row, "updated_at"),
	}
}

// --- home thread -------------------------------------------------------------

// profileHomeThreadID is the id of ownerID's home thread for a profile. It is
// derived rather than random so concurrent first uses, on any replica, insert
// the same primary key and the store keeps exactly one.
func profileHomeThreadID(ownerID, profileID string) string {
	return "th_home_" + hashString(ownerID + "\x00" + profileID)[:32]
}

// profileHomeThread finds or creates the caller's home thread for a profile
// they can see. Watches that post to an agent target this thread.
func (s *graphjinService) profileHomeThread(ctx context.Context, profileID string) (map[string]any, error) {
	profile, err := s.visibleProfileRow(ctx, profileID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, errProfileNotFound
	}
	ownerID, _ := artifactUserID(ctx)
	id := profileHomeThreadID(ownerID, stringMapValue(profile, "id"))
	if row, err := s.threadStoreRow(ctx, id); err != nil || row != nil {
		if err != nil {
			return nil, err
		}
		return threadAPIRow(row), nil
	}
	created, err := s.createThread(ctx, threadSpec{ID: id, Title: stringMapValue(profile, "name"), Profile: stringMapValue(profile, "id")})
	if err == nil {
		return created, nil
	}
	// A concurrent caller inserted it first: the primary key rejected ours.
	if row, rerr := s.threadStoreRow(ctx, id); rerr == nil && row != nil {
		return threadAPIRow(row), nil
	}
	return nil, err
}

// --- REST --------------------------------------------------------------------

func (s1 *HttpService) AgentProfiles(ah auth.HandlerFunc) http.Handler {
	return apiV1Handler(s1, nil, s1.apiV1AgentProfiles(), ah)
}

func (s1 *HttpService) AgentProfilesWithNS(ah auth.HandlerFunc, ns string) http.Handler {
	return apiV1Handler(s1, &ns, s1.apiV1AgentProfiles(), ah)
}

// apiV1AgentProfiles serves the console's agents:
//
//	GET    /api/v1/agent/profiles              the caller's agents and team agents
//	POST   /api/v1/agent/profiles              add an agent {name, instructions, icon}
//	PATCH  /api/v1/agent/profiles/{id}         edit, or share {tier: approved|observed}
//	DELETE /api/v1/agent/profiles/{id}         delete
//	GET    /api/v1/agent/profiles/{id}/thread  the caller's home thread (find or create)
func (s1 *HttpService) apiV1AgentProfiles() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := s1.Load().(*graphjinService)
		w.Header().Set("Content-Type", "application/json")
		if !s.profilesEnabled() {
			writeJSONStatus(w, http.StatusNotFound, map[string]any{"error": "agent profiles are disabled"})
			return
		}
		if err := s.checkGraphJinInitialized(); err != nil {
			renderErr(w, err)
			return
		}
		ctx := s.applyIdentityContext(r.Context())
		viewerID, ok := artifactUserID(ctx)
		if !ok {
			writeJSONStatus(w, http.StatusUnauthorized, map[string]any{"error": "agent profiles require a signed-in user"})
			return
		}
		cp := newArtifactControlPlane(s)
		id, action, _ := strings.Cut(strings.Trim(strings.TrimPrefix(r.URL.Path, routeAgentProfiles), "/"), "/")
		switch {
		case id == "" && r.Method == http.MethodGet:
			rows, err := s.visibleProfileRows(ctx)
			out := make([]map[string]any, 0, len(rows))
			for _, row := range rows {
				out = append(out, profileAPIRow(row, viewerID))
			}
			sort.SliceStable(out, func(i, j int) bool {
				return strings.ToLower(stringMapValue(out[i], "name")) < strings.ToLower(stringMapValue(out[j], "name"))
			})
			writeProfileResult(w, http.StatusOK, out, err)
		case id == "" && r.Method == http.MethodPost:
			input, err := readJSONMap(r)
			if err != nil {
				writeProfileResult(w, http.StatusBadRequest, nil, err)
				return
			}
			fields := map[string]any{}
			for _, field := range []string{"name", "instructions", "icon"} {
				if v, ok := input[field]; ok {
					fields[field] = v
				}
			}
			row, err := cp.upsertAgentProfile(ctx, core.ManagedMutationRoot{Operation: "insert", Input: fields}, nil)
			writeProfileResult(w, http.StatusCreated, row, err)
		case id != "" && action == "thread" && r.Method == http.MethodGet:
			row, err := s.profileHomeThread(ctx, id)
			writeProfileResult(w, http.StatusOK, row, err)
		case id != "" && action == "" && r.Method == http.MethodPatch:
			input, err := readJSONMap(r)
			if err != nil {
				writeProfileResult(w, http.StatusBadRequest, nil, err)
				return
			}
			existing, err := s.internalArtifactStoreRow(ctx, id)
			var row map[string]any
			if err == nil {
				if existing == nil {
					err = errProfileNotFound
				} else {
					row, err = cp.upsertAgentProfile(ctx, core.ManagedMutationRoot{Operation: "update", Input: input}, existing)
				}
			}
			writeProfileResult(w, http.StatusOK, row, err)
		case id != "" && action == "" && r.Method == http.MethodDelete:
			existing, err := s.internalArtifactStoreRow(ctx, id)
			if err == nil && (existing == nil || normalizeArtifactKind(stringMapValue(existing, "kind")) != artifactKindAgentProfile ||
				(stringMapValue(existing, "owner_id") != viewerID && !s.identityRoleIsAdmin(ctx))) {
				err = errProfileNotFound
			}
			if err == nil {
				_, err = cp.deleteArtifact(ctx, core.ManagedMutationRoot{Operation: "delete", Input: map[string]any{"id": id}})
			}
			writeProfileResult(w, http.StatusOK, map[string]any{"id": id, "deleted": err == nil}, err)
		default:
			writeJSONStatus(w, http.StatusNotFound, map[string]any{"error": "not found"})
		}
	})
}

func writeProfileResult(w http.ResponseWriter, status int, data any, err error) {
	if err != nil {
		code := threadErrorStatus(err)
		if errors.Is(err, errProfileNotFound) {
			code = http.StatusNotFound
		}
		writeJSONStatus(w, code, map[string]any{"error": err.Error()})
		return
	}
	writeJSONStatus(w, status, map[string]any{"data": data})
}
