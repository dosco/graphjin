package serv

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const WatchEventsUnseenResourceURI = "graphjin://watch-events/unseen"

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
}

type watchEventScope struct {
	OwnerID      string `json:"owner_id"`
	AccountID    string `json:"account_id,omitempty"`
	SourceNodeID string `json:"source_node_id,omitempty"`
}

type watchMCPSubscription struct {
	SessionID string
	UserID    string
	AccountID string
	Server    *server.MCPServer
}

type watchMCPSubscriptionRegistry struct {
	mu   sync.RWMutex
	subs map[string]watchMCPSubscription
}

func (r *watchMCPSubscriptionRegistry) subscribe(ctx context.Context, srv *server.MCPServer, s *graphjinService, uri string) {
	if uri != WatchEventsUnseenResourceURI || srv == nil || s == nil {
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
	key := watchMCPSubscriptionKey(session.SessionID(), userID, accountID)
	r.subs[key] = watchMCPSubscription{
		SessionID: session.SessionID(),
		UserID:    userID,
		AccountID: accountID,
		Server:    srv,
	}
	r.mu.Unlock()
	payload, err := s.unseenWatchEventsPayload(ctx)
	if err == nil && payload.Count > 0 {
		if !sendWatchEventsResourceUpdate(srv, session.SessionID()) {
			r.removeSubscription(session.SessionID(), userID, accountID)
		}
	}
}

func (r *watchMCPSubscriptionRegistry) unsubscribe(ctx context.Context, uri string) {
	if uri != WatchEventsUnseenResourceURI {
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
	r.removeSubscription(session.SessionID(), userID, accountID)
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

func (r *watchMCPSubscriptionRegistry) removeSubscription(sessionID, userID, accountID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.subs, watchMCPSubscriptionKey(sessionID, userID, accountID))
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

func watchMCPSubscriptionKey(sessionID, userID, accountID string) string {
	return strings.Join([]string{sessionID, userID, accountID}, "\x00")
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
			payload, err := ms.service.unseenWatchEventsPayload(ms.effectiveIdentityContext(ctx))
			if err != nil {
				return nil, err
			}
			data, err := mcpMarshalJSON(payload, true)
			if err != nil {
				return nil, err
			}
			return []mcp.ResourceContents{
				mcp.TextResourceContents{URI: req.Params.URI, MIMEType: "application/json", Text: string(data)},
			}, nil
		},
	)
}

func (s *graphjinService) unseenWatchEventsPayload(ctx context.Context) (watchEventsUnseenResource, error) {
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
	if accountID, ok := identityVarString(ctx, "account_id"); ok {
		where = `where: { owner_id: { eq: $owner_id }, account_id: { eq: $account_id } }`
		vars["account_id"] = accountID
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
		payload.Events = append(payload.Events, entry)
		payload.EventIDs = append(payload.EventIDs, entry.ID)
		if payload.Since == "" || (entry.CreatedAt != "" && entry.CreatedAt < payload.Since) {
			payload.Since = entry.CreatedAt
		}
	}
	payload.Count = len(payload.Events)
	return payload, nil
}

func (s *graphjinService) notifyWatchEventsResource(ownerID string, accountID ...string) {
	scope := watchEventScope{OwnerID: strings.TrimSpace(ownerID)}
	if len(accountID) != 0 {
		scope.AccountID = strings.TrimSpace(accountID[0])
	}
	s.notifyWatchEventsResourceScope(scope, true)
}

func (s *graphjinService) notifyWatchEventsResourceScope(scope watchEventScope, publish bool) {
	ownerID := strings.TrimSpace(scope.OwnerID)
	if s == nil || ownerID == "" {
		return
	}
	for _, sub := range s.mcpWatchSubs.matching(ownerID, scope.AccountID) {
		if !sendWatchEventsResourceUpdate(sub.Server, sub.SessionID) {
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
		}
		if scope.OwnerID == "" {
			continue
		}
		key := scope.OwnerID + "\x00" + scope.AccountID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, scope)
	}
	return out
}

func sendWatchEventsResourceUpdate(srv *server.MCPServer, sessionID string) bool {
	if srv == nil || strings.TrimSpace(sessionID) == "" {
		return false
	}
	err := srv.SendNotificationToSpecificClient(sessionID, mcp.MethodNotificationResourceUpdated, map[string]any{
		"uri": WatchEventsUnseenResourceURI,
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
