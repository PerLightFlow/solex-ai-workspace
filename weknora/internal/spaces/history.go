package spaces

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

// ContentVersion retains the original source locator, not a copy of a signed
// download URL. Payloads are never returned by listing or audit endpoints.
type ContentVersion struct {
	ID         uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	Kind       string    `json:"kind" gorm:"index:content_history;type:varchar(24)"`
	ResourceID string    `json:"resource_id" gorm:"index:content_history;type:varchar(36)"`
	TenantID   uint64    `json:"-" gorm:"index:content_history"`
	KBID       string    `json:"knowledge_base_id" gorm:"index;type:varchar(36)"`
	Hash       string    `json:"revision" gorm:"type:varchar(64)"`
	Payload    string    `json:"-" gorm:"type:text"`
	SourcePath string    `json:"-" gorm:"type:text;index"`
	ActorID    string    `json:"actor_id" gorm:"type:varchar(64)"`
	ActorName  string    `json:"actor_name" gorm:"type:varchar(100)"`
	Automatic  bool      `json:"automatic"`
	CreatedAt  time.Time `json:"created_at"`
}

func (ContentVersion) TableName() string { return "solex_content_versions" }

type RecycledContent struct {
	Kind       string    `json:"kind" gorm:"primaryKey;type:varchar(24)"`
	ResourceID string    `json:"resource_id" gorm:"primaryKey;type:varchar(36)"`
	TenantID   uint64    `json:"tenant_id" gorm:"primaryKey"`
	KBID       string    `json:"knowledge_base_id" gorm:"index;type:varchar(36)"`
	Title      string    `json:"title" gorm:"type:text"`
	Payload    string    `json:"-" gorm:"type:text"`
	SourcePath string    `json:"-" gorm:"type:text;index"`
	ActorID    string    `json:"actor_id" gorm:"type:varchar(64)"`
	DeletedAt  time.Time `json:"deleted_at"`
	ExpiresAt  time.Time `json:"expires_at" gorm:"index"`
}

func (RecycledContent) TableName() string { return "solex_recycled_content" }

type Retention struct {
	ID                int `json:"-" gorm:"primaryKey"`
	ManualVersions    int `json:"manual_versions"`
	AutomaticVersions int `json:"automatic_versions"`
	RecycleDays       int `json:"recycle_days"`
}

func (Retention) TableName() string { return "solex_content_retention" }
func (s *Service) Retention(ctx context.Context) Retention {
	r := Retention{ID: 1, ManualVersions: 30, AutomaticVersions: 30, RecycleDays: 30}
	var stored Retention
	if s.db.WithContext(ctx).First(&stored, "id=1").Error == nil {
		r = stored
	}
	return r
}
func (s *Service) SaveRetention(ctx context.Context, r Retention) error {
	u, e := s.actor(ctx)
	if e != nil || !u.IsSystemAdmin {
		return ErrForbidden
	}
	if r.ManualVersions < 2 || r.ManualVersions > 200 || r.AutomaticVersions < 2 || r.AutomaticVersions > 200 || r.RecycleDays < 1 || r.RecycleDays > 365 {
		return ErrInvalid
	}
	r.ID = 1
	return s.db.WithContext(ctx).Save(&r).Error
}

func KnowledgeRevision(k *types.Knowledge) string {
	// Processing status, generated summaries and timestamps are not content
	// revisions and must not create conflicts while parsing completes.
	content := map[string]any{"title": k.Title, "source": k.Source, "file_path": k.FilePath, "file_hash": k.FileHash, "file_name": k.FileName, "folder_path": k.FolderPath, "custom_metadata": k.CustomMetadata, "description": k.Description}
	if k.IsManual() {
		if m, e := k.ManualMetadata(); e == nil && m != nil {
			content["content"] = m.Content
			content["status"] = m.Status
		}
	}
	raw, _ := json.Marshal(content)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

func (s *Service) recordKnowledge(ctx context.Context, k *types.Knowledge) error {
	hash := KnowledgeRevision(k)
	var last ContentVersion
	err := s.db.WithContext(ctx).Where("kind='knowledge' AND resource_id=? AND tenant_id=?", k.ID, k.TenantID).Order("id DESC").First(&last).Error
	if err == nil && last.Hash == hash {
		return nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if e := s.retainFile(ctx, k); e != nil {
		return e
	}
	payload, e := json.Marshal(k)
	if e != nil {
		return e
	}
	caller := types.CallerFromContext(ctx)
	caller.UserID = k.LastModifiedBy
	if caller.UserID == "" {
		caller.UserID = k.CreatedBy
	}
	name := k.LastModifiedByName
	if name == "" {
		name = k.CreatedByName
	}
	if name == "" {
		name = "系统"
	}
	if caller.UserID != "" {
		var u types.User
		if s.db.WithContext(ctx).First(&u, "id=?", caller.UserID).Error == nil {
			name = u.Username
		}
	}
	v := ContentVersion{Kind: "knowledge", ResourceID: k.ID, TenantID: k.TenantID, KBID: k.KnowledgeBaseID, Hash: hash, Payload: string(payload), SourcePath: k.FilePath, ActorID: caller.UserID, ActorName: name, Automatic: caller.UserID == "", CreatedAt: time.Now().UTC()}
	if e = s.db.WithContext(ctx).Create(&v).Error; e != nil {
		return e
	}
	limit := s.Retention(ctx).ManualVersions
	if v.Automatic {
		limit = s.Retention(ctx).AutomaticVersions
	}
	var obsolete []uint64
	if e = s.db.WithContext(ctx).Model(&ContentVersion{}).Where("kind=? AND resource_id=? AND tenant_id=? AND automatic=?", v.Kind, v.ResourceID, v.TenantID, v.Automatic).Order("id DESC").Offset(limit).Pluck("id", &obsolete).Error; e != nil {
		return e
	}
	if len(obsolete) > 0 {
		return s.db.WithContext(ctx).Delete(&ContentVersion{}, "id IN ?", obsolete).Error
	}
	return nil
}

// MutateKnowledge serializes source changes and snapshots on the same DB
// transaction. A failed write cannot leave a version claiming it succeeded.
func MutateKnowledge(ctx context.Context, db *gorm.DB, id string, write func(*gorm.DB) error) error {
	if !types.UserSpacesEnabled() {
		return write(db.WithContext(ctx))
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var before types.Knowledge
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&before, "id=?", id).Error; e != nil {
			return e
		}
		if expected := types.ExpectedContentVersion(ctx); expected != "" && expected != KnowledgeRevision(&before) {
			return ErrConflict
		}
		s := New(tx)
		if caller := types.CallerFromContext(ctx); caller.UserID != "" {
			if e := s.Require(ctx, "knowledge_base", before.KnowledgeBaseID, before.TenantID, "edit"); e != nil {
				return e
			}
		}
		if e := s.recordKnowledge(ctx, &before); e != nil {
			return e
		}
		if e := write(tx); e != nil {
			return e
		}
		var after types.Knowledge
		if e := tx.First(&after, "id=?", id).Error; e != nil {
			return e
		}
		return s.recordKnowledge(ctx, &after)
	})
}

func (s *Service) Versions(ctx context.Context, id string) ([]ContentVersion, string, error) {
	var k types.Knowledge
	if e := s.db.WithContext(ctx).First(&k, "id=?", id).Error; e != nil {
		return nil, "", ErrNotFound
	}
	a, e := s.Check(ctx, "knowledge_base", k.KnowledgeBaseID, k.TenantID)
	if e != nil || !a.Read {
		return nil, "", ErrForbidden
	}
	q := s.db.WithContext(ctx).Where("kind='knowledge' AND resource_id=? AND tenant_id=?", id, k.TenantID)
	if !s.IsPersonalOwner(ctx, "knowledge_base", k.KnowledgeBaseID, k.TenantID) {
		var from time.Time
		for _, source := range a.Sources {
			if from.IsZero() || source.HistoryFrom.Before(from) {
				from = source.HistoryFrom
			}
		}
		if from.IsZero() {
			return nil, "", ErrForbidden
		}
		q = q.Where("created_at>=?", from)
	}
	rows := []ContentVersion{}
	e = q.Select("id,kind,resource_id,kb_id,hash,actor_id,actor_name,automatic,created_at").Order("id DESC").Limit(400).Find(&rows).Error
	return rows, KnowledgeRevision(&k), e
}

func (s *Service) KnowledgeVersion(ctx context.Context, id string, version uint64) (*types.Knowledge, error) {
	rows, _, e := s.Versions(ctx, id)
	if e != nil {
		return nil, e
	}
	allowed := false
	for _, r := range rows {
		if r.ID == version {
			allowed = true
		}
	}
	if !allowed {
		return nil, ErrNotFound
	}
	var v ContentVersion
	if e = s.db.WithContext(ctx).First(&v, "id=?", version).Error; e != nil {
		return nil, e
	}
	var k types.Knowledge
	if e = json.Unmarshal([]byte(v.Payload), &k); e != nil {
		return nil, e
	}
	return &k, nil
}

func (s *Service) PreserveKnowledge(ctx context.Context, items []*types.Knowledge) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		s := New(tx)
		now := time.Now().UTC()
		days := s.Retention(ctx).RecycleDays
		for _, k := range items {
			if e := s.recordKnowledge(ctx, k); e != nil {
				return e
			}
			data, e := s.recyclePayload(ctx, k)
			if e != nil {
				return e
			}
			item := RecycledContent{Kind: "knowledge", ResourceID: k.ID, TenantID: k.TenantID, KBID: k.KnowledgeBaseID, Title: k.Title, Payload: string(data), SourcePath: k.FilePath, ActorID: types.CallerFromContext(ctx).UserID, DeletedAt: now, ExpiresAt: now.Add(time.Duration(days) * 24 * time.Hour)}
			if e = tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&item).Error; e != nil {
				return e
			}
		}
		return nil
	})
}
func (s *Service) RecycleBin(ctx context.Context, kbID string) ([]RecycledContent, error) {
	_, _, tenant, e := s.ResourceScope(ctx, "knowledge_base", kbID, 0)
	if e != nil {
		return nil, e
	}
	if e = s.Require(ctx, "knowledge_base", kbID, tenant, "edit"); e != nil {
		return nil, e
	}
	rows := []RecycledContent{}
	e = s.db.WithContext(ctx).Select("kind,resource_id,kb_id,title,actor_id,deleted_at,expires_at").Where("kb_id=? AND expires_at>? AND ((kind='knowledge' AND resource_id IN (SELECT id FROM knowledges WHERE deleted_at IS NOT NULL)) OR (kind='wiki' AND resource_id IN (SELECT id FROM wiki_pages WHERE deleted_at IS NOT NULL)))", kbID, time.Now()).Order("deleted_at DESC").Find(&rows).Error
	return rows, e
}

func (s *Service) ReviveKnowledge(ctx context.Context, id string) (*types.Knowledge, error) {
	var item RecycledContent
	if e := s.db.WithContext(ctx).First(&item, "kind='knowledge' AND resource_id=? AND expires_at>?", id, time.Now()).Error; e != nil {
		return nil, ErrNotFound
	}
	if e := s.Require(ctx, "knowledge_base", item.KBID, item.TenantID, "edit"); e != nil {
		return nil, e
	}
	var k types.Knowledge
	if e := json.Unmarshal([]byte(item.Payload), &k); e != nil {
		return nil, e
	}
	e := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked RecycledContent
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, "kind='knowledge' AND resource_id=? AND expires_at>?", id, time.Now()).Error; e != nil {
			return ErrNotFound
		}
		// Restore the source only; handlers enqueue the normal index/Wiki rebuild.
		result := tx.Unscoped().Model(&types.Knowledge{}).Where("id=? AND deleted_at IS NOT NULL", id).Updates(map[string]any{"deleted_at": nil, "parse_status": types.ParseStatusFailed, "enable_status": "disabled", "error_message": "恢复原件完成，等待重建索引", "updated_at": time.Now()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrConflict
		}
		return tx.Delete(&item).Error
	})
	return &k, e
}

func (s *Service) RestoreSource(ctx context.Context, k *types.Knowledge) error {
	return MutateKnowledge(ctx, s.db, k.ID, func(tx *gorm.DB) error {
		u, e := s.actor(ctx)
		if e != nil {
			return e
		}
		now := time.Now().UTC()
		return tx.Model(&types.Knowledge{}).Where("id=? AND tenant_id=?", k.ID, k.TenantID).Updates(map[string]any{
			"title": k.Title, "description": k.Description, "file_name": k.FileName, "folder_path": k.FolderPath, "file_type": k.FileType, "file_hash": k.FileHash, "file_size": k.FileSize, "file_path": k.FilePath, "metadata": k.Metadata, "custom_metadata": k.CustomMetadata,
			"updated_at": now, "last_modified_by": u.ID, "last_modified_by_name": u.Username, "last_modified_at": now, "enable_status": "disabled",
		}).Error
	})
}
