package spaces

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"
)

// InviteCandidate deliberately exposes only the identity needed to address an
// invitation, never the account's personal tenant, settings or resources.
type InviteCandidate struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

type InviteCandidatePage struct {
	Items   []InviteCandidate `json:"items"`
	HasMore bool              `json:"has_more"`
}

func (s *Service) InviteCandidates(ctx context.Context, space, search string, offset, limit int) (InviteCandidatePage, error) {
	page := InviteCandidatePage{Items: []InviteCandidate{}}
	u, err := s.actor(ctx)
	if err != nil {
		return page, err
	}
	if _, err = s.member(ctx, s.db, space, u.ID, 3); err != nil {
		return page, err
	}
	if err = s.requireWritableSpace(ctx, s.db, space); err != nil {
		return page, err
	}
	search = strings.TrimSpace(search)
	if offset < 0 || offset > 100000 || limit < 1 || limit > 50 || utf8.RuneCountInString(search) > 100 {
		return page, ErrInvalid
	}
	query := s.db.WithContext(ctx).Table("users u").Select("u.id, u.username, u.email").
		Where("u.is_active = ? AND u.deleted_at IS NULL AND u.email <> ''", true).
		Where("NOT EXISTS (SELECT 1 FROM solex_space_members m WHERE m.space_id = ? AND m.user_id = u.id)", space).
		Where("NOT EXISTS (SELECT 1 FROM solex_space_invitations i WHERE i.space_id = ? AND LOWER(i.email) = LOWER(u.email) AND i.status = ? AND i.expires_at > ?)", space, "pending", time.Now().UTC())
	if search != "" {
		// Treat wildcard characters as literal username/email characters.
		pattern := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(search)) + "%"
		query = query.Where("(LOWER(u.username) LIKE ? ESCAPE '!' OR LOWER(u.email) LIKE ? ESCAPE '!')", pattern, pattern)
	}
	err = query.Order("LOWER(u.username), u.id").Offset(offset).Limit(limit + 1).Scan(&page.Items).Error
	if len(page.Items) > limit {
		page.HasMore = true
		page.Items = page.Items[:limit]
	}
	return page, err
}
