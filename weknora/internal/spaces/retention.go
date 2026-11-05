package spaces

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type RetainedFile struct {
	ID        string `gorm:"primaryKey;type:varchar(64)"`
	TenantID  uint64
	KBID      string `gorm:"type:varchar(36)"`
	Path      string `gorm:"type:text"`
	CreatedAt time.Time
}

func (RetainedFile) TableName() string { return "solex_retained_files" }
func (s *Service) retainPath(ctx context.Context, k *types.Knowledge, path string) error {
	if path == "" {
		return nil
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", k.TenantID, path)))
	row := RetainedFile{ID: hex.EncodeToString(sum[:]), TenantID: k.TenantID, KBID: k.KnowledgeBaseID, Path: path, CreatedAt: time.Now().UTC()}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.Assignments(map[string]any{"kb_id": k.KnowledgeBaseID})}).Create(&row).Error
}
func (s *Service) retainFile(ctx context.Context, k *types.Knowledge) error {
	if e := s.retainPath(ctx, k, k.FilePath); e != nil {
		return e
	}
	raw, e := json.Marshal(k)
	if e != nil {
		return e
	}
	for _, ref := range types.ScanResourceReferences(string(raw)) {
		if e = s.retainPath(ctx, k, ref); e != nil {
			return e
		}
	}
	return nil
}

// Deletion retains derived files as well as the source. The extra payload field
// is ignored by Knowledge decoding but keeps each file tied to the recycle item.
func (s *Service) recyclePayload(ctx context.Context, k *types.Knowledge) ([]byte, error) {
	var refs []string
	if e := s.db.WithContext(ctx).Table("resource_bindings b").Select("'resource://' || r.handle").Joins("JOIN resources r ON r.id=b.resource_id AND r.state='active'").Where("b.owner_type='knowledge' AND b.owner_id=?", k.ID).Scan(&refs).Error; e != nil {
		return nil, e
	}
	for _, ref := range refs {
		if e := s.retainPath(ctx, k, ref); e != nil {
			return nil, e
		}
	}
	return json.Marshal(struct {
		*types.Knowledge
		RetainedPaths []string `json:"retained_paths"`
	}{k, refs})
}
func (s *Service) containsPath(column string) string {
	if s.db.Dialector.Name() == "sqlite" {
		return "instr(" + column + ",f.path)>0"
	}
	return "strpos(" + column + ",f.path)>0"
}
func (s *Service) retainedReferences() string {
	return "EXISTS(SELECT 1 FROM knowledges k WHERE k.tenant_id=f.tenant_id AND k.deleted_at IS NULL AND (k.file_path=f.path OR " + s.containsPath("CAST(k.metadata AS TEXT)") + ")) OR EXISTS(SELECT 1 FROM solex_content_versions v WHERE v.tenant_id=f.tenant_id AND (v.source_path=f.path OR " + s.containsPath("v.payload") + ")) OR EXISTS(SELECT 1 FROM solex_recycled_content r WHERE r.tenant_id=f.tenant_id AND (r.source_path=f.path OR " + s.containsPath("r.payload") + " OR (r.kind='knowledge_base' AND r.resource_id=f.kb_id)))"
}
func (s *Service) liveFileBindings() string {
	return "EXISTS(SELECT 1 FROM resources stored JOIN resource_bindings b ON b.resource_id=stored.id WHERE stored.tenant_id=f.tenant_id AND (stored.physical_path=f.path OR 'resource://' || stored.handle=f.path) AND (b.owner_type<>'knowledge' OR EXISTS(SELECT 1 FROM knowledges k WHERE k.id=b.owner_id AND k.deleted_at IS NULL)))"
}

// FileRetained prevents normal reparse cleanup from deleting bytes still used
// by a retained source version or recycle item. Errors keep the bytes safely.
func (s *Service) FileRetained(ctx context.Context, path string) (bool, error) {
	var n int64
	e := s.db.WithContext(ctx).Table("solex_retained_files f").Where("f.path=?", path).Where(s.retainedReferences()).Count(&n).Error
	return n > 0, e
}

// SweepRetention is bounded, retryable, and never deletes a source still
// referenced by a live document, retained version, or unexpired recycle item.
// The caller supplies the normal configured storage implementation.
func (s *Service) SweepRetention(ctx context.Context, remove func(context.Context, *types.KnowledgeBase, string) error) error {
	var expired []RecycledContent
	if e := s.db.WithContext(ctx).Where("expires_at<=?", time.Now()).Order("expires_at").Limit(100).Find(&expired).Error; e != nil {
		return e
	}
	for _, item := range expired {
		if e := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var current RecycledContent
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "kind=? AND resource_id=? AND tenant_id=?", item.Kind, item.ResourceID, item.TenantID).Error; e != nil {
				return e
			}
			if current.ExpiresAt.After(time.Now()) {
				return nil
			}
			table := "knowledges"
			switch item.Kind {
			case "wiki":
				table = "wiki_pages"
			case "knowledge_base":
				table = "knowledge_bases"
			case "agent":
				table = "custom_agents"
			}
			var active int64
			if e := tx.Table(table).Where("id=? AND deleted_at IS NULL", item.ResourceID).Count(&active).Error; e != nil {
				return e
			}
			if active == 0 {
				if item.Kind == "knowledge_base" {
					if e := tx.Where("kb_id=?", item.ResourceID).Delete(&ContentVersion{}).Error; e != nil {
						return e
					}
					if e := tx.Where("kb_id=? AND kind IN ?", item.ResourceID, []string{"knowledge", "wiki"}).Delete(&RecycledContent{}).Error; e != nil {
						return e
					}
					if e := tx.Where("knowledge_base_id=?", item.ResourceID).Delete(&types.WikiPageRevision{}).Error; e != nil {
						return e
					}
					if e := tx.Where("knowledge_base_id=?", item.ResourceID).Delete(&types.ChunkRevision{}).Error; e != nil {
						return e
					}
				}
				if e := tx.Where("kind=? AND resource_id=? AND tenant_id=?", item.Kind, item.ResourceID, item.TenantID).Delete(&ContentVersion{}).Error; e != nil {
					return e
				}
				if item.Kind == "wiki" {
					if e := tx.Where("page_id=?", item.ResourceID).Delete(&types.WikiPageRevision{}).Error; e != nil {
						return e
					}
				}
			}
			return tx.Delete(&current).Error
		}); e != nil {
			return e
		}
	}
	if remove == nil {
		return nil
	}
	var files []RetainedFile
	// Select only unreferenced candidates; live files must not starve the queue.
	if e := s.db.WithContext(ctx).Table("solex_retained_files f").Where("NOT (" + s.retainedReferences() + ") AND NOT (" + s.liveFileBindings() + ")").Order("f.created_at").Limit(50).Find(&files).Error; e != nil {
		return e
	}
	var failures []error
	for _, file := range files {
		e := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var locked RetainedFile
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, "id=?", file.ID).Error; e != nil {
				if errors.Is(e, gorm.ErrRecordNotFound) {
					return nil
				}
				return e
			}
			// retainFile takes this same row lock before adding any new reference.
			var n int64
			if e := tx.Table("solex_retained_files f").Where("f.id=?", file.ID).Where("NOT (" + s.retainedReferences() + ") AND NOT (" + s.liveFileBindings() + ")").Count(&n).Error; e != nil {
				return e
			}
			if n == 0 {
				return nil
			}
			var kb types.KnowledgeBase
			if e := tx.Unscoped().First(&kb, "id=? AND tenant_id=?", file.KBID, file.TenantID).Error; e != nil {
				return e
			}
			// Drop claims belonging to permanently deleted documents only.
			if e := tx.Where("owner_type='knowledge' AND owner_id IN (SELECT id FROM knowledges WHERE deleted_at IS NOT NULL) AND resource_id IN (SELECT id FROM resources WHERE tenant_id=? AND (physical_path=? OR 'resource://' || handle=?))", file.TenantID, file.Path, file.Path).Delete(&types.ResourceBinding{}).Error; e != nil {
				return e
			}
			if e := remove(ctx, &kb, file.Path); e != nil {
				return e
			}
			return tx.Delete(&locked).Error
		})
		if e != nil {
			failures = append(failures, e)
		}
	}
	return errors.Join(failures...)
}
