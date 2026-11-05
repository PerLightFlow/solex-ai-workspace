package spaces

import (
	"context"
	"github.com/Tencent/WeKnora/internal/types"
	"slices"
	"time"
)

// GuardRun revalidates the user's identity and all dependencies at every tool
// call. The watcher also cancels an in-flight model or sandbox call on revoke.
// Child runs inherit this context; resuming builds a fresh guard.
func (s *Service) GuardRun(ctx context.Context, agent *types.CustomAgent, kbs []string) (context.Context, context.CancelFunc, error) {
	check := func(current context.Context) error {
		if _, e := s.actor(current); e != nil {
			return e
		}
		if agent != nil && !types.IsBuiltinAgentID(agent.ID) {
			live, e := s.Agent(current, agent.ID, agent.TenantID)
			if e != nil {
				return e
			}
			if live.Config.KBSelectionMode != agent.Config.KBSelectionMode || !slices.Equal(live.Config.KnowledgeBases, agent.Config.KnowledgeBases) {
				return ErrConflict
			}
		}
		for _, id := range kbs {
			kind, resource, tenant, e := s.ResourceScope(current, "knowledge_base", id, 0)
			if e != nil {
				return e
			}
			if e = s.Require(current, kind, resource, tenant, "read"); e != nil {
				return e
			}
		}
		return nil
	}
	if e := check(ctx); e != nil {
		return ctx, func() {}, e
	}
	next, cancel := context.WithCancel(types.WithRunAuthorization(ctx, check))
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-next.Done():
				return
			case <-ticker.C:
				if check(next) != nil {
					cancel()
					return
				}
			}
		}
	}()
	return next, cancel, nil
}
