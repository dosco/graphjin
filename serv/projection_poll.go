package serv

import (
	"context"
	"strings"
	"time"
)

func (s *graphjinService) startProjectionPoller(parent context.Context) {
	if s == nil || s.conf == nil || s.systemNanoDB == nil || !s.conf.Core.Artifacts.Enabled {
		return
	}
	intervalSeconds := s.conf.Core.EffectiveArtifactsConfig().PollSeconds
	if intervalSeconds <= 0 {
		return
	}
	if _, _, _, ok := s.artifactDB(); !ok {
		return
	}
	ctx := parent
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	s.addCloseFn(cancel)

	go s.projectionPollLoop(ctx, time.Duration(intervalSeconds)*time.Second)
}

func (s *graphjinService) projectionPollLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	lastRevision, err := s.artifactRevision(ctx, "artifacts")
	if err != nil {
		s.recordArtifactProjectionPollError("read initial artifact revision", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.pollArtifactProjectionRevision(ctx, &lastRevision); err != nil {
				s.recordArtifactProjectionPollError("poll artifact revision", err)
			}
		}
	}
}

func (s *graphjinService) pollArtifactProjectionRevision(ctx context.Context, lastRevision *int64) (bool, error) {
	if s == nil || lastRevision == nil || s.systemNanoDB == nil || s.conf == nil || !s.conf.Core.Artifacts.Enabled {
		return false, nil
	}
	current, err := s.artifactRevision(ctx, "artifacts")
	if err != nil {
		return false, err
	}
	if current == *lastRevision {
		return false, nil
	}
	if err := s.refreshArtifactProjection(); err != nil {
		return false, err
	}
	*lastRevision = current
	s.invalidateCatalogCache()
	return true, nil
}

func (s *graphjinService) artifactRevision(ctx context.Context, domain string) (int64, error) {
	if s == nil || s.conf == nil {
		return 0, nil
	}
	if _, _, _, ok := s.artifactDB(); !ok {
		return 0, nil
	}
	domain = strings.TrimSpace(domain)
	if domain == "" {
		domain = "artifacts"
	}
	rows, err := s.internalStoreRows(ctx, "revisions", `where: { domain: { eq: $domain } }`, revisionStoreFields, map[string]any{"domain": domain})
	if err != nil || len(rows) == 0 {
		return 0, err
	}
	return int64MapValue(rows[0], "revision"), nil
}

func (s *graphjinService) recordArtifactProjectionPollError(action string, err error) {
	if s == nil || err == nil {
		return
	}
	if s.log != nil {
		s.log.Warnf("artifact projection poller: %s failed: %v", action, err)
	}
	s.recordRuntimeEvent(context.Background(), runtimeEvent{
		Phase:      "catalog",
		Status:     runtimeStatusDegraded,
		Severity:   "warn",
		Summary:    "Artifact NanoDB projection poller failed.",
		NextAction: "Check artifact revision table connectivity and reload schema if the projection is stale.",
		ErrorCode:  "artifact_projection_poll_failed",
		Details:    map[string]any{"action": action, "error": err.Error()},
	})
}
