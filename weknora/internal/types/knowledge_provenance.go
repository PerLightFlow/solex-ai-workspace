package types

import (
	"context"
	"strings"
	"unicode"
)

type knowledgeUserMutationKey struct{}

// WithKnowledgeUserMutation explicitly marks a user-visible document change.
// Carrying an authenticated user alone is insufficient: background enrichment
// may inherit the initiator but must never overwrite human modification data.
func WithKnowledgeUserMutation(ctx context.Context) context.Context {
	return context.WithValue(ctx, knowledgeUserMutationKey{}, true)
}

func IsKnowledgeUserMutation(ctx context.Context) bool {
	marked, _ := ctx.Value(knowledgeUserMutationKey{}).(bool)
	return marked
}

// KnowledgeActor returns only a real user identity and a username snapshot,
// never an email or a synthetic API-key principal. An absent name stays unknown.
func KnowledgeActor(ctx context.Context) (string, string) {
	id, ok := UserIDFromContext(ctx)
	if !ok || IsSyntheticUserID(id) {
		return "", ""
	}
	if user, ok := ctx.Value(UserContextKey).(*User); ok && user != nil && user.ID == id {
		return id, KnowledgeActorName(user.Username)
	}
	return id, ""
}

// KnowledgeActorName is a bounded public username, never an email address.
func KnowledgeActorName(name string) string {
	if strings.Contains(name, "@") {
		return ""
	}
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name))
	runes := []rune(name)
	if len(runes) > 100 {
		runes = runes[:100]
	}
	return string(runes)
}
