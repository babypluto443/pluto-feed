// Package repository 数据存取层：只管怎么读写数据库，不做业务判断。
package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"pluto_feed/internal/model"
)

// UserRepo 用户存取接口。定义成接口的原因：
// service 层的单元测试可以用内存假实现，不用真连数据库。
type UserRepo interface {
	Create(ctx context.Context, u *model.User) error
	GetByNickname(ctx context.Context, nickname string) (*model.User, error)
	GetByID(ctx context.Context, id int64) (*model.User, error)
	// GetByIDs 批量按 id 查（Feed 页面一次性取所有作者，避免 N+1 查询）
	GetByIDs(ctx context.Context, ids []int64) (map[int64]*model.User, error)
}

type userRepo struct{ db *gorm.DB }

func NewUserRepo(db *gorm.DB) UserRepo { return &userRepo{db: db} }

func (r *userRepo) Create(ctx context.Context, u *model.User) error {
	return r.db.WithContext(ctx).Create(u).Error
}

func (r *userRepo) GetByNickname(ctx context.Context, nickname string) (*model.User, error) {
	var u model.User
	err := r.db.WithContext(ctx).Where("nickname = ?", nickname).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil // 查不到不是错误，交给上层判断
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *userRepo) GetByID(ctx context.Context, id int64) (*model.User, error) {
	var u model.User
	err := r.db.WithContext(ctx).First(&u, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *userRepo) GetByIDs(ctx context.Context, ids []int64) (map[int64]*model.User, error) {
	if len(ids) == 0 {
		return map[int64]*model.User{}, nil
	}
	var users []model.User
	// 一条 IN 查询取整页作者：20 个作者的帖子页只发 1 条 SQL，而不是 20 条
	err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&users).Error
	if err != nil {
		return nil, err
	}
	out := make(map[int64]*model.User, len(users))
	for i := range users {
		out[users[i].ID] = &users[i]
	}
	return out, nil
}
