package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	WatchEventsUnseenResourceURI         = "graphjin://watch-events/unseen"
	WatchEventsUnseenResourceTemplateURI = WatchEventsUnseenResourceURI + "/{watch_id}"
)

type watchEventsUnseenResource struct {
	GeneratedAt string                   `json:"generated_at"`
	Count       int                      `json:"count"`
	Since       string                   `json:"since,omitempty"`
	EventIDs    []string                 `json:"event_ids"`
	Events      []watchEventsUnseenEntry `json:"events"`
}

type watchEventsUnseenEntry struct {
	ID             string `json:"id"`
	WatchID        string `json:"watch_id"`
	CreatedAt      string `json:"created_at"`
	DataHash       string `json:"data_hash"`
	DataTruncated  bool   `json:"data_truncated"`
	DeliveryStatus string `json:"delivery_status"`
	Verdict        string `json:"verdict,omitempty"`
	Severity       string `json:"severity,omitempty"`
	Summary        string `json:"summary,omitempty"`
}

type watchEventScope struct {
	OwnerID      string `json:"owner_id"`
	AccountID    string `json:"account_id,omitempty"`
	WatchID      string `json:"watch_id,omitempty"`
	SourceNodeID string `json:"source_node_id,omitempty"`
}

type watchMCPSubscription struct {
	SessionID string
	UserID    string
	AccountID string
	WatchID   string
	URI       string
	Server    *server.MCPServer
}

type watchMCPSubscriptionRegistry struct {
	mu   sync.RWMutex
	subs map[string]watchMCPSubscription
}

func (r *watchMCPSubscriptionRegistry) subscribe(ctx context.Context, srv *server.MCPServer, s *graphjinService, uri string) {
	watchID, ok := watchIDFromUnseenResourceURI(uri)
	if !ok || uri != watchEventsUnseenResourceURI(watchID) || srv == nil || s == nil {
		return
	}
	session := server.ClientSessionFromContext(ctx)
	if session == nil {
		return
	}
	userID, ok := artifactUserID(ctx)
	if !ok {
		return
	}
	accountID, _ := identityVarString(ctx, "account_id")
	r.mu.Lock()
	if r.subs == nil {
		r.subs = map[string]watchMCPSubscription{}
	}
	key := watchMCPSubscriptionKey(session.SessionID(), userID, accountID, uri)
	r.subs[key] = watchMCPSubscription{
		SessionID: session.SessionID(),
		UserID:    userID,
		AccountID: accountID,
		WatchID:   watchID,
		URI:       uri,
		Server:    srv,
	}
	r.mu.Unlock()
	payload, err := s.unseenWatchEventsPayload(ctx, watchID)
	if err == nil && payload.Count > 0 {
		if !sendWatchEventsResourceUpdate(srv, session.SessionID(), uri) {
			r.removeSubscription(session.SessionID(), userID, accountID, uri)
		}
	}
}

func (r *watchMCPSubscriptionRegistry) unsubscribe(ctx context.Context, uri string) {
	watchID, ok := watchIDFromUnseenResourceURI(uri)
	if !ok || uri != watchEventsUnseenResourceURI(watchID) {
		return
	}
	session := server.ClientSessionFromContext(ctx)
	if session == nil {
		return
	}
	userID, ok := artifactUserID(ctx)
	if !ok {
		r.remove(session.SessionID())
		return
	}
	accountID, _ := identityVarString(ctx, "account_id")
	r.removeSubscription(session.SessionID(), userID, accountID, uri)
}

func (r *watchMCPSubscriptionRegistry) remove(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, sub := range r.subs {
		if sub.SessionID == sessionID {
			delete(r.subs, key)
		}
	}
}

func (r *watchMCPSubscriptionRegistry) removeSubscription(sessionID, userID, accountID, uri string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.subs, watchMCPSubscriptionKey(sessionID, userID, accountID, uri))
}

func (r *watchMCPSubscriptionRegistry) matching(ownerID string, accountID ...string) []watchMCPSubscription {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]watchMCPSubscription, 0, len(r.subs))
	wantAccount := ""
	if len(accountID) != 0 {
		wantAccount = accountID[0]
	}
	for _, sub := range r.subs {
		if sub.UserID == ownerID && (wantAccount == "" || sub.AccountID == wantAccount) {
			out = append(out, sub)
		}
	}
	return out
}

func (r *watchMCPSubscriptionRegistry) matchingScope(scope watchEventScope) []watchMCPSubscription {
	r.mu.RLock()
	defer r.mu.RUnlock()
	wantAccount := strings.TrimSpace(scope.AccountID)
	wantWatch := strings.TrimSpace(scope.WatchID)
	candidates := make([]watchMCPSubscription, 0, len(r.subs))
	exactBySession := map[string]bool{}
	for _, sub := range r.subs {
		if sub.UserID != strings.TrimSpace(scope.OwnerID) || (wantAccount != "" && sub.AccountID != wantAccount) {
			continue
		}
		candidates = append(candidates, sub)
		if wantWatch != "" && sub.WatchID != "" {
			exactBySession[watchMCPSubscriptionSessionKey(sub)] = true
		}
	}
	out := make([]watchMCPSubscription, 0, len(candidates))
	for _, sub := range candidates {
		if wantWatch == "" {
			// Old Redis publishers do not include watch_id. Preserve the aggregate
			// wakeup path during rolling upgrades and do not guess an exact URI.
			if sub.WatchID == "" {
				out = append(out, sub)
			}
			continue
		}
		if exactBySession[watchMCPSubscriptionSessionKey(sub)] {
			if sub.WatchID == wantWatch {
				out = append(out, sub)
			}
			continue
		}
		if sub.WatchID == "" {
			out = append(out, sub)
		}
	}
	return out
}

func (r *watchMCPSubscriptionRegistry) watchIDsForSession(sessionID, userID, accountID string) ([]string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	set := map[string]struct{}{}
	for _, sub := range r.subs {
		if sub.SessionID != sessionID || sub.UserID != userID || sub.AccountID != accountID || sub.WatchID == "" {
			continue
		}
		set[sub.WatchID] = struct{}{}
	}
	if len(set) == 0 {
		return nil, false
	}
	out := make([]string, 0, len(set))
	for watchID := range set {
		out = append(out, watchID)
	}
	sort.Strings(out)
	return out, true
}

func watchMCPSubscriptionKey(sessionID, userID, accountID, uri string) string {
	return strings.Join([]string{sessionID, userID, accountID, uri}, "\x00")
}

func watchMCPSubscriptionSessionKey(sub watchMCPSubscription) string {
	return strings.Join([]string{sub.SessionID, sub.UserID, sub.AccountID}, "\x00")
}

func watchEventsUnseenResourceURI(watchID string) string {
	watchID = strings.TrimSpace(watchID)
	if watchID == "" {
		return WatchEventsUnseenResourceURI
	}
	// RFC 6570 simple-string expansion encodes reserved characters such as the
	// colon in GraphJin's generated watch IDs. QueryEscape has the right reserved
	// character behavior here; normalize spaces to path-safe percent encoding.
	encoded := strings.ReplaceAll(url.QueryEscape(watchID), "+", "%20")
	return WatchEventsUnseenResourceURI + "/" + encoded
}

func watchIDFromUnseenResourceURI(uri string) (string, bool) {
	if uri == WatchEventsUnseenResourceURI {
		return "", true
	}
	prefix := WatchEventsUnseenResourceURI + "/"
	if !strings.HasPrefix(uri, prefix) {
		return "", false
	}
	encoded := strings.TrimPrefix(uri, prefix)
	if encoded == "" || strings.Contains(encoded, "/") {
		return "", false
	}
	watchID, err := url.PathUnescape(encoded)
	if err != nil || strings.TrimSpace(watchID) == "" {
		return "", false
	}
	return watchID, true
}

func (ms *mcpServer) registerWatchResources() {
	if ms == nil || ms.service == nil || !ms.service.watchesEnabled() {
		return
	}
	ms.srv.AddResource(
		mcp.NewResource(
			WatchEventsUnseenResourceURI,
			"GraphJin Unseen Watch Events",
			mcp.WithResourceDescription("Caller-scoped unseen watch-event summary; read this after notifications/resources/updated."),
			mcp.WithMIMEType("application/json"),
		),
		func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			return ms.watchEventsResourceContents(ctx, req.Params.URI, "")
		},
	)
	ms.srv.AddResourceTemplate(
		mcp.NewResourceTemplate(
			WatchEventsUnseenResourceTemplateURI,
			"GraphJin Unseen Watch Events by Watch",
			mcp.WithTemplateDescription("Caller-scoped unseen event summary for one watch; RFC 6570-expand this template with the retained watch ID and subscribe to that concrete URI."),
			mcp.WithTemplateMIMEType("application/json"),
		),
		func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			watchID, ok := watchIDFromUnseenResourceURI(req.Params.URI)
			if !ok || watchID == "" {
				return nil, fmt.Errorf("invalid GraphJin watch-events resource URI")
			}
			return ms.watchEventsResourceContents(ctx, req.Params.URI, watchID)
		},
	)
}

func (ms *mcpServer) watchEventsResourceContents(ctx context.Context, uri, watchID string) ([]mcp.ResourceContents, error) {
	payload, err := ms.service.unseenWatchEventsPayload(ms.effectiveIdentityContext(ctx), watchID)
	if err != nil {
		return nil, err
	}
	data, err := mcpMarshalJSON(payload, true)
	if err != nil {
		return nil, err
	}
	return []mcp.ResourceContents{
		mcp.TextResourceContents{URI: uri, MIMEType: "application/json", Text: string(data)},
	}, nil
}

func (s *graphjinService) unseenWatchEventsPayload(ctx context.Context, watchIDs ...string) (watchEventsUnseenResource, error) {
	payload := watchEventsUnseenResource{GeneratedAt: time.Now().UTC().Format(time.RFC3339)}
	if _, _, _, _, ok := s.watchDB(); !ok {
		return payload, nil
	}
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return payload, nil
	}
	where := `where: { owner_id: { eq: $owner_id } }`
	vars := map[string]any{"owner_id": ownerID}
	accountID, hasAccount := identityVarString(ctx, "account_id")
	watchID := ""
	if len(watchIDs) != 0 {
		watchID = strings.TrimSpace(watchIDs[0])
	}
	switch {
	case hasAccount && watchID != "":
		where = `where: { owner_id: { eq: $owner_id }, account_id: { eq: $account_id }, watch_id: { eq: $watch_id } }`
		vars["account_id"] = accountID
		vars["watch_id"] = watchID
	case hasAccount:
		where = `where: { owner_id: { eq: $owner_id }, account_id: { eq: $account_id } }`
		vars["account_id"] = accountID
	case watchID != "":
		where = `where: { owner_id: { eq: $owner_id }, watch_id: { eq: $watch_id } }`
		vars["watch_id"] = watchID
	}
	rows, err := s.internalStoreRows(ctx, "watch_events", where, watchEventStoreFields, vars)
	if err != nil {
		return payload, err
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return stringMapValue(rows[i], "created_at") > stringMapValue(rows[j], "created_at")
	})
	for _, row := range rows {
		if boolMapValue(row, "seen") {
			continue
		}
		entry := watchEventsUnseenEntry{
			ID:             stringMapValue(row, "id"),
			WatchID:        stringMapValue(row, "watch_id"),
			CreatedAt:      stringMapValue(row, "created_at"),
			DataHash:       stringMapValue(row, "data_hash"),
			DataTruncated:  boolMapValue(row, "data_truncated"),
			DeliveryStatus: watchDeliveryStatus(stringMapValue(row, "delivery_status")),
		}
		enrichment := mapFromAny(parseJSONValue(jsonMapString(row, "enrichment_json")))
		entry.Verdict = stringFromAny(enrichment["verdict"])
		entry.Severity = stringFromAny(enrichment["severity"])
		entry.Summary = stringFromAny(enrichment["summary"])
		payload.Events = append(payload.Events, entry)
		payload.EventIDs = append(payload.EventIDs, entry.ID)
		if payload.Since == "" || (entry.CreatedAt != "" && entry.CreatedAt < payload.Since) {
			payload.Since = entry.CreatedAt
		}
	}
	payload.Count = len(payload.Events)
	return payload, nil
}

func (s *graphjinService) notifyWatchEventsResource(ownerID, accountID, watchID string) {
	s.notifyWatchEventsResourceScope(watchEventScope{
		OwnerID:   strings.TrimSpace(ownerID),
		AccountID: strings.TrimSpace(accountID),
		WatchID:   strings.TrimSpace(watchID),
	}, true)
}

func (s *graphjinService) notifyWatchEventsResourceScope(scope watchEventScope, publish bool) {
	ownerID := strings.TrimSpace(scope.OwnerID)
	if s == nil || ownerID == "" {
		return
	}
	for _, sub := range s.mcpWatchSubs.matchingScope(scope) {
		if !sendWatchEventsResourceUpdate(sub.Server, sub.SessionID, sub.URI) {
			s.mcpWatchSubs.remove(sub.SessionID)
		}
	}
	if publish {
		if coord := s.currentWatchCoordinator(); coord != nil {
			if err := coord.PublishUnseen(context.Background(), scope); err != nil {
				s.recordWatchRunnerError("publish watch unseen notification", err, nil)
			}
		}
	}
}

func (s *graphjinService) notifyWatchEventScopes(scopes []watchEventScope) {
	for _, scope := range scopes {
		s.notifyWatchEventsResourceScope(scope, true)
	}
}

func watchEventScopesFromRows(rows []map[string]any) []watchEventScope {
	if len(rows) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]watchEventScope, 0, len(rows))
	for _, row := range rows {
		scope := watchEventScope{
			OwnerID:   strings.TrimSpace(stringMapValue(row, "owner_id")),
			AccountID: strings.TrimSpace(stringMapValue(row, "account_id")),
			WatchID:   strings.TrimSpace(stringMapValue(row, "watch_id")),
		}
		if scope.OwnerID == "" {
			continue
		}
		key := scope.OwnerID + "\x00" + scope.AccountID + "\x00" + scope.WatchID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, scope)
	}
	return out
}

func (s *graphjinService) watchIDsForMCPContext(ctx context.Context) ([]string, bool) {
	session := server.ClientSessionFromContext(ctx)
	if s == nil || session == nil {
		return nil, false
	}
	userID, ok := artifactUserID(ctx)
	if !ok {
		return nil, false
	}
	accountID, _ := identityVarString(ctx, "account_id")
	return s.mcpWatchSubs.watchIDsForSession(session.SessionID(), userID, accountID)
}

func sendWatchEventsResourceUpdate(srv *server.MCPServer, sessionID, uri string) bool {
	if srv == nil || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(uri) == "" {
		return false
	}
	err := srv.SendNotificationToSpecificClient(sessionID, mcp.MethodNotificationResourceUpdated, map[string]any{
		"uri": uri,
	})
	return err == nil
}

func watchEventsResourceText(content []mcp.ResourceContents) (watchEventsUnseenResource, error) {
	var payload watchEventsUnseenResource
	if len(content) == 0 {
		return payload, nil
	}
	if text, ok := content[0].(mcp.TextResourceContents); ok {
		return payload, json.Unmarshal([]byte(text.Text), &payload)
	}
	return payload, nil
}
