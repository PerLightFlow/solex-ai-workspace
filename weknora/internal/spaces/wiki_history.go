package spaces

import (
	"context"
	"encoding/json"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

func (s *Service) HistoryCutoff(ctx context.Context, kbID string) (time.Time, error) {
	if types.CallerFromContext(ctx).UserID == "" {
		return time.Time{}, nil
	}
	_, _, tenant, e := s.ResourceScope(ctx, "knowledge_base", kbID, 0)
	if e != nil {
		return time.Time{}, e
	}
	a, e := s.Check(ctx, "knowledge_base", kbID, tenant)
	if e != nil || !a.Read {
		return time.Time{}, ErrForbidden
	}
	if s.IsPersonalOwner(ctx, "knowledge_base", kbID, tenant) {
		return time.Time{}, nil
	}
	var cutoff time.Time
	for _, source := range a.Sources {
		if cutoff.IsZero() || source.HistoryFrom.Before(cutoff) {
			cutoff = source.HistoryFrom
		}
	}
	if cutoff.IsZero() {
		return cutoff, ErrForbidden
	}
	return cutoff, nil
}

func (s *Service) PruneWiki(ctx context.Context, id string) error {
	r := s.Retention(ctx)
	for _, automatic := range []bool{true, false} {
		q := s.db.WithContext(ctx).Model(&types.WikiPageRevision{}).Where("page_id=?", id)
		limit := r.ManualVersions
		if automatic {
			q = q.Where("edit_source IN ?", types.WikiPrunableEditSources)
			limit = r.AutomaticVersions
		} else {
			q = q.Where("edit_source NOT IN ?", types.WikiPrunableEditSources)
		}
		var ids []string
		if e := q.Order("version DESC").Offset(limit).Pluck("id", &ids).Error; e != nil {
			return e
		}
		if len(ids) > 0 {
			if e := s.db.WithContext(ctx).Delete(&types.WikiPageRevision{}, "id IN ?", ids).Error; e != nil {
				return e
			}
		}
	}
	return nil
}

func (s *Service) PreserveWiki(ctx context.Context, page *types.WikiPage) error {
	data, e := json.Marshal(page)
	if e != nil {
		return e
	}
	now := time.Now().UTC()
	item := RecycledContent{Kind: "wiki", ResourceID: page.ID, TenantID: page.TenantID, KBID: page.KnowledgeBaseID, Title: page.Title, Payload: string(data), ActorID: types.CallerFromContext(ctx).UserID, DeletedAt: now, ExpiresAt: now.Add(time.Duration(s.Retention(ctx).RecycleDays) * 24 * time.Hour)}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{UpdateAll: true}).Create(&item).Error
}
func (s *Service) ReviveWiki(ctx context.Context, id string) (*types.WikiPage, error) {
	var item RecycledContent
	if e := s.db.WithContext(ctx).First(&item, "kind='wiki' AND resource_id=? AND expires_at>?", id, time.Now()).Error; e != nil {
		return nil, ErrNotFound
	}
	if e := s.Require(ctx, "knowledge_base", item.KBID, item.TenantID, "edit"); e != nil {
		return nil, e
	}
	var p types.WikiPage
	if e := json.Unmarshal([]byte(item.Payload), &p); e != nil {
		return nil, e
	}
	e := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "kind='wiki' AND resource_id=? AND expires_at>?", id, time.Now()).Error; e != nil {
			return ErrNotFound
		}
		result := tx.Unscoped().Model(&types.WikiPage{}).Where("id=? AND deleted_at IS NOT NULL", id).Update("deleted_at", nil)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrConflict
		}
		return tx.Delete(&item).Error
	})
	return &p, e
}
