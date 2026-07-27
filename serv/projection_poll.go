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

	start := func(domain string, refresh func() error) {
		s.revisionConsumerWG.Add(1)
		go func() {
			defer s.revisionConsumerWG.Done()
			s.projectionPollLoopForDomain(ctx, time.Duration(intervalSeconds)*time.Second, domain, refresh)
		}()
	}
	start("artifacts", s.refreshArtifactProjection)
	if s.tasksEnabled() {
		start("tasks", s.refreshTaskProjection)
	}
}

func (s *graphjinService) projectionPollLoop(ctx context.Context, interval time.Duration) {
	s.projectionPollLoopWithRecovery(ctx, interval, internalRecoveryInterval)
}

func (s *graphjinService) projectionPollLoopForDomain(ctx context.Context, interval time.Duration, domain string, refresh func() error) {
	s.projectionPollLoopWithRecoveryForDomain(ctx, interval, internalRecoveryInterval, domain, refresh)
}

func (s *graphjinService) projectionPollLoopWithRecovery(
	ctx context.Context,
	interval time.Duration,
	recoveryInterval time.Duration,
) {
	s.projectionPollLoopWithRecoveryForDomain(ctx, interval, recoveryInterval, "artifacts", s.refreshArtifactProjection)
}

func (s *graphjinService) projectionPollLoopWithRecoveryForDomain(
	ctx context.Context,
	interval time.Duration,
	recoveryInterval time.Duration,
	domain string,
	refreshProjection func() error,
) {
	if interval <= 0 {
		return
	}
	domain = strings.TrimSpace(domain)
	if domain == "" || refreshProjection == nil {
		return
	}
	if recoveryInterval <= 0 {
		recoveryInterval = internalRecoveryInterval
	}
	// The initial subscription result intentionally forces one refresh. It
	// closes the race between building the startup projection and subscribing
	// to its revision; do not replace this sentinel with the current revision.
	lastRevision := int64(-1)
	signals := s.revisionSignals(ctx, domain, true, interval)
	pendingRevision := lastRevision
	var retryTimer *time.Timer
	var retry <-chan time.Time
	refresh := func(force bool) {
		if !force && pendingRevision == lastRevision {
			return
		}
		if err := refreshProjection(); err != nil {
			s.recordArtifactProjectionPollError("refresh "+domain+" projection", err)
			if retryTimer == nil {
				retryTimer = time.NewTimer(interval)
			} else {
				if !retryTimer.Stop() {
					select {
					case <-retryTimer.C:
					default:
					}
				}
				retryTimer.Reset(interval)
			}
			retry = retryTimer.C
			return
		}
		lastRevision = pendingRevision
		s.invalidateCatalogCache()
		// A revision signal may originate on another replica. The projection is
		// current now, so let the local semantic worker compare the combined
		// catalog/artifact revision and rebuild only when needed.
		if domain == "artifacts" && s.semantic != nil {
			s.semantic.CatalogChanged()
		}
		if retryTimer != nil {
			if !retryTimer.Stop() {
				select {
				case <-retryTimer.C:
				default:
				}
			}
		}
		retry = nil
	}
	recoveryTicker := time.NewTicker(recoveryInterval)
	defer recoveryTicker.Stop()
	defer func() {
		if retryTimer != nil {
			retryTimer.Stop()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case signal, ok := <-signals:
			if !ok {
				return
			}
			pendingRevision = signal.Revision
			refresh(false)
		case <-retry:
			retry = nil
			refresh(false)
		case <-recoveryTicker.C:
			refresh(true)
		}
	}
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
