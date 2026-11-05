package spaces

import (
	"context"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"time"
)

type ResourceLinkPrincipal struct {
	TokenHash string `gorm:"primaryKey;type:varchar(64)"`
	UserID    string `gorm:"type:varchar(36)"`
	TenantID  uint64
	ExpiresAt time.Time
}

func (ResourceLinkPrincipal) TableName() string { return "solex_resource_link_principals" }

// The link is a bounded bearer capability tied to the issuing account's live
// grants. Old stateless or resource-wide links cannot bypass revoked sharing.
func (s *Service) AuthorizeStoredResource(ctx context.Context, r *types.StoredResource) error {
	u, e := s.actor(ctx)
	if e != nil {
		return e
	}
	var bindings []types.ResourceBinding
	if e = s.db.WithContext(ctx).Where("resource_id=?", r.ID).Find(&bindings).Error; e != nil {
		return e
	}
	for _, b := range bindings {
		if b.OwnerType == types.ResourceOwnerKnowledge {
			kind, id, tenant, e := s.ResourceScope(ctx, "knowledge", b.OwnerID, 0)
			permission := "read"
			if b.Relation == types.ResourceRelationSourceFile {
				permission = "download"
			}
			if e == nil && s.Require(ctx, kind, id, tenant, permission) == nil {
				return nil
			}
		}
		if b.OwnerType == types.ResourceOwnerMessage && (b.Relation == types.ResourceRelationArtifact || b.Relation == types.ResourceRelationAttachment) {
			var n int64
			if e := s.db.WithContext(ctx).Table("messages m").Joins("JOIN sessions s ON s.id=m.session_id").Where("m.id=? AND s.user_id=? AND s.tenant_id=? AND s.deleted_at IS NULL AND m.deleted_at IS NULL", b.OwnerID, u.ID, u.TenantID).Count(&n).Error; e == nil && n > 0 {
				return nil
			}
		}
	}
	return ErrForbidden
}
func (s *Service) IssueResourceLink(ctx context.Context, r *types.StoredResource, hash string, ttl time.Duration) error {
	if e := s.AuthorizeStoredResource(ctx, r); e != nil {
		return e
	}
	if ttl <= 0 || ttl > 2*time.Hour {
		ttl = 2 * time.Hour
	}
	c := types.CallerFromContext(ctx)
	expiry := time.Now().UTC().Add(ttl)
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := tx.Where("expires_at<=?", time.Now()).Delete(&ResourceLinkPrincipal{}).Error; e != nil {
			return e
		}
		if e := tx.Create(&types.ResourceAccessGrant{TokenHash: hash, ResourceID: r.ID, AccessScope: "read", ExpiresAt: expiry}).Error; e != nil {
			return e
		}
		return tx.Create(&ResourceLinkPrincipal{TokenHash: hash, UserID: c.UserID, TenantID: c.TenantID, ExpiresAt: expiry}).Error
	})
}
func (s *Service) ResolveResourceLink(ctx context.Context, hash string) (*types.StoredResource, error) {
	var principal ResourceLinkPrincipal
	if e := s.db.WithContext(ctx).First(&principal, "token_hash=? AND expires_at>?", hash, time.Now()).Error; e != nil {
		return nil, ErrNotFound
	}
	var grant types.ResourceAccessGrant
	if e := s.db.WithContext(ctx).First(&grant, "token_hash=? AND revoked_at IS NULL AND expires_at>?", hash, time.Now()).Error; e != nil {
		return nil, ErrNotFound
	}
	var resource types.StoredResource
	if e := s.db.WithContext(ctx).First(&resource, "id=? AND state=?", grant.ResourceID, types.ResourceStateActive).Error; e != nil {
		return nil, ErrNotFound
	}
	ctx = types.WithCaller(ctx, types.Caller{UserID: principal.UserID, TenantID: principal.TenantID, Role: types.TenantRoleOwner})
	if e := s.AuthorizeStoredResource(ctx, &resource); e != nil {
		return nil, e
	}
	return &resource, nil
}
