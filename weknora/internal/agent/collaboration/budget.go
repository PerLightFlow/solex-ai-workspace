package collaboration

import (
	"context"
	"errors"
)

// BudgetCloseout is a bounded, model-safe snapshot taken when no further
// delegated work may be admitted. Running children may still be stopping;
// their states and CancelRequested flag must not be described as success.
type BudgetCloseout struct {
	Events []Event      `json:"-"`
	Tasks  []MemberView `json:"tasks"`
}

// CloseBudget atomically fences further work, requests cancellation and
// captures results already committed at the boundary. It does not wait for
// running tools or approve any pending prompt. The parent's lease fence is
// checked by Store.Change, so an expired/replaced parent cannot close a task.
func (b *Binding) CloseBudget(ctx context.Context) (BudgetCloseout, error) {
	var result BudgetCloseout
	if b.Child != nil {
		return result, errors.New("only a parent can close the collaboration budget")
	}
	var sequence int64
	err := b.Runtime.Store.Change(b.Context(ctx), b.Scope, func(g *Group) error {
		result = BudgetCloseout{}
		if g.Status != "active" {
			return ErrClosed
		}
		g.ParentBudgetClosed = true
		cancelGroup(g)
		for _, member := range g.Members {
			run := current(member)
			result.Tasks = append(result.Tasks, MemberView{
				PreviousTurn: member.Inherited, ID: member.ID, Name: member.Name,
				AgentID: member.AgentID, SessionID: member.SessionID, RunID: run.ID,
				Status: run.State, CancelRequested: run.CancelRequested,
			})
		}
		for _, event := range g.Events {
			if event.Sequence > b.Seen {
				result.Events = append(result.Events, event)
			}
		}
		sequence = g.NextSequence
		return nil
	})
	if err != nil {
		return BudgetCloseout{}, err
	}
	b.Seen = sequence
	return result, nil
}
