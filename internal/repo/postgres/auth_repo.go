package postgres

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"gophermind/internal/core/model"
)

// AuthRepository 负责用户与刷新令牌存储。
type AuthRepository struct {
	db *gorm.DB
}

// NewAuthRepository 构建 AuthRepository。
func NewAuthRepository(db *gorm.DB) *AuthRepository {
	return &AuthRepository{db: db}
}

// CreateUser 创建用户。
func (r *AuthRepository) CreateUser(ctx context.Context, username string, passwordHash string, role string) (model.AuthUser, error) {
	user := UserModel{
		Username:     username,
		PasswordHash: passwordHash,
		Role:         role,
	}
	err := r.db.WithContext(ctx).Create(&user).Error
	if err != nil {
		return model.AuthUser{}, err
	}
	return model.AuthUser{
		ID:           user.ID,
		Username:     user.Username,
		PasswordHash: user.PasswordHash,
		Role:         user.Role,
	}, nil
}

// GetUserByUsername 按用户名读取用户。
func (r *AuthRepository) GetUserByUsername(ctx context.Context, username string) (model.AuthUser, error) {
	var user UserModel
	err := r.db.WithContext(ctx).Where("username = ?", username).First(&user).Error
	if err != nil {
		return model.AuthUser{}, err
	}
	return model.AuthUser{
		ID:           user.ID,
		Username:     user.Username,
		PasswordHash: user.PasswordHash,
		Role:         user.Role,
	}, nil
}

// SaveRefreshToken 保存 refresh token 哈希。
func (r *AuthRepository) SaveRefreshToken(ctx context.Context, userID uint64, tokenJTI string, tokenHash string, deviceID string, expiresAt time.Time) error {
	model := RefreshTokenModel{
		UserID:    userID,
		TokenJTI:  tokenJTI,
		TokenHash: tokenHash,
		DeviceID:  deviceID,
		ExpiresAt: expiresAt,
	}
	return r.db.WithContext(ctx).Create(&model).Error
}

// GetActiveRefreshToken 获取未吊销且未过期的 refresh token 记录。
func (r *AuthRepository) GetActiveRefreshToken(ctx context.Context, tokenJTI string, tokenHash string) (model.RefreshTokenRecord, error) {
	var m RefreshTokenModel
	err := r.db.WithContext(ctx).
		Where("token_jti = ? AND token_hash = ? AND revoked_at IS NULL AND expires_at > ?", tokenJTI, tokenHash, time.Now()).
		First(&m).Error
	if err != nil {
		return model.RefreshTokenRecord{}, err
	}
	return model.RefreshTokenRecord{
		UserID:    m.UserID,
		TokenJTI:  m.TokenJTI,
		TokenHash: m.TokenHash,
		DeviceID:  m.DeviceID,
		ExpiresAt: m.ExpiresAt,
	}, nil
}

// RevokeRefreshToken 吊销指定 refresh token。
func (r *AuthRepository) RevokeRefreshToken(ctx context.Context, tokenJTI string) error {
	now := time.Now()
	return r.db.WithContext(ctx).
		Model(&RefreshTokenModel{}).
		Where("token_jti = ? AND revoked_at IS NULL", tokenJTI).
		Update("revoked_at", &now).Error
}

// RevokeAllRefreshTokens 吊销用户所有 refresh token。
func (r *AuthRepository) RevokeAllRefreshTokens(ctx context.Context, userID uint64) error {
	now := time.Now()
	return r.db.WithContext(ctx).
		Model(&RefreshTokenModel{}).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Update("revoked_at", &now).Error
}

// IsNotFound 返回是否未找到。
func (r *AuthRepository) IsNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}
