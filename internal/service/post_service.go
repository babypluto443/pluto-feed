package service

import (
	"context"
	"encoding/binary"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/sync/singleflight"

	"pluto_feed/internal/apierror"
	"pluto_feed/internal/bloom"
	"pluto_feed/internal/cache"
	"pluto_feed/internal/model"
	"pluto_feed/internal/repository"
)

// idBytes int64 → 8 字节（布隆的输入统一用定长字节，避免字符串化歧义）
func idBytes(id int64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(id))
	return b
}

// 缓存 TTL 约定：
//   - 正常实体 10 分钟——TTL 是"写时失效"漏网之鱼的自愈兜底
//   - 空值 30 秒——防穿透：不存在的帖子也缓存，避免恶意 id 打穿 DB
const (
	postCacheTTL = 10 * time.Minute
	nilCacheTTL  = 30 * time.Second
)

// 帖子内容边界（YAGNI：只定本期需要的，别造一堆用不上的规则）
const (
	MaxContentLen = 1000 // 正文上限（字符数，按 Unicode 字符算）
	MaxImages     = 9    // 图片上限（小红书也是 9 图）
	MaxTags       = 5    // 标签上限
)

// PostService 帖子业务：发布 / 详情 / 删除。
// cache 可为 nil（单测/降级）：为 nil 时所有读直连 DB。
type PostService struct {
	posts        repository.PostRepo
	users        UserRepo
	cache        cache.Store
	interactions repository.InteractionRepo // 查询"当前用户是否点过赞"
	bloom        *bloom.Bloom               // 防穿透：id 一定不存在时直接 404，不碰 DB（可为 nil）
	sf           singleflight.Group         // 防击穿：同一 key 的并发回源只放一个请求进 DB
}

func NewPostService(posts repository.PostRepo, users UserRepo, c cache.Store, interactions repository.InteractionRepo, bl *bloom.Bloom) *PostService {
	return &PostService{posts: posts, users: users, cache: c, interactions: interactions, bloom: bl}
}

// CreateInput 发布帖子的输入。
type CreateInput struct {
	ImageURLs []string
	Content   string
	Tags      []string
}

// Create 发布帖子。uid 来自认证中间件（发帖人=当前登录用户，客户端说了不算）。
func (s *PostService) Create(ctx context.Context, uid int64, in CreateInput) (*PostView, error) {
	if len(in.ImageURLs) < 1 {
		return nil, apierror.New(400, 40010, "至少需要 1 张图片")
	}
	if len(in.ImageURLs) > MaxImages {
		return nil, apierror.New(400, 40011, "图片最多 9 张")
	}
	n := utf8.RuneCountInString(in.Content)
	if n < 1 {
		return nil, apierror.New(400, 40012, "正文不能为空")
	}
	if n > MaxContentLen {
		return nil, apierror.New(400, 40013, "正文最多 1000 字")
	}
	if len(in.Tags) > MaxTags {
		return nil, apierror.New(400, 40014, "标签最多 5 个")
	}

	p := &model.Post{
		UserID:    uid,
		ImageURLs: model.StringList(in.ImageURLs),
		Content:   in.Content,
		Tags:      model.StringList(in.Tags),
	}
	if err := s.posts.Create(ctx, p); err != nil {
		return nil, err
	}
	if s.bloom != nil {
		s.bloom.Add(idBytes(p.ID)) // 新帖进布隆，之后的详情请求不会被误拦
	}

	// 自己刚发的帖子，作者信息查一次（发帖是低频写操作，多一查无妨）
	author, err := s.users.GetByID(ctx, uid)
	if err != nil {
		return nil, err
	}
	view := NewPostView(p, author)
	return &view, nil
}

// postEnvelope 缓存信封：found=false 表示"确认不存在"的空值缓存（防穿透）。
type postEnvelope struct {
	Found bool       `json:"found"`
	Post  *model.Post `json:"post"`
}

// Get 帖子详情（公开接口，任何人可看）—— viewerUID 为登录用户 id（未登录传 0）。
// Cache Aside 完整实现：
//
//	读：缓存命中 → 直接返回；未命中 → singleflight 回源 DB → 回填缓存
//	写（点赞/评论/删帖）：DEL 缓存 → 下次读自然重建（写时失效）
//	自愈：TTL 到期自动重建，兜底任何失效遗漏
//	防穿透：不存在的帖子也缓存 30 秒（found=false 空值信封）
//	防击穿：singleflight 保证同一帖子的并发回源只有一个请求打到 DB
func (s *PostService) Get(ctx context.Context, viewerUID int64, id int64) (*PostView, error) {
	key := cache.PostKey(id)

	// 1) 读缓存（缓存层错误当未命中处理——Redis 挂了不能拖死主链路）
	if s.cache != nil {
		var env postEnvelope
		if found, _ := s.cache.GetJSON(ctx, key, &env); found {
			if !env.Found {
				return nil, apierror.ErrNotFound // 空值缓存：确认不存在
			}
			author, err := s.getUserCached(ctx, env.Post.UserID)
			if err != nil {
				return nil, err
			}
			view := NewPostView(env.Post, author)
			s.fillLiked(ctx, &view, viewerUID)
			return &view, nil
		}
	}

	// 1.5) 布隆拦截（O3）：id 一定不存在 → 不碰 DB，直接空值缓存 + 404。
	// bloom 误判（说有其实没有）只会落到下面正常回源路径，正确性无损。
	if s.bloom != nil && !s.bloom.Test(idBytes(id)) {
		if s.cache != nil {
			env := postEnvelope{Found: false}
			_ = s.cache.SetJSON(ctx, key, env, nilCacheTTL)
		}
		return nil, apierror.ErrNotFound
	}

	// 2) 未命中 → singleflight 回源：同 key 并发只有一个请求真正进 DB，
	//    其余共享结果（防止热帖缓存过期瞬间被打穿——"缓存击穿"）
	v, err, _ := s.sf.Do(key, func() (any, error) {
		p, err := s.posts.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if s.cache != nil {
			env := postEnvelope{Found: p != nil, Post: p}
			ttl := postCacheTTL
			if p == nil {
				ttl = nilCacheTTL // 空值短缓存，防穿透
			}
			if err := s.cache.SetJSON(ctx, key, env, ttl); err != nil {
				log.Printf("[cache] set %s: %v（降级直连 DB，不影响请求）", key, err)
			}
		}
		return p, nil
	})
	if err != nil {
		return nil, err
	}
	p := v.(*model.Post)
	if p == nil {
		// 不区分"从来不存在"和"已被删除"——统一 404，不泄露删除痕迹
		return nil, apierror.ErrNotFound
	}
	author, err := s.getUserCached(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	view := NewPostView(p, author)
	s.fillLiked(ctx, &view, viewerUID)
	return &view, nil
}

// fillLiked 单帖点赞态填充（未登录跳过）。
func (s *PostService) fillLiked(ctx context.Context, view *PostView, viewerUID int64) {
	if viewerUID <= 0 || s.interactions == nil {
		return
	}
	if liked, err := s.interactions.HasLiked(ctx, viewerUID, view.ID); err == nil {
		view.Liked = liked
	}
}

// userEnvelope 用户信息缓存信封（同 postEnvelope 思路）。
type userEnvelope struct {
	Found bool        `json:"found"`
	User  *model.User `json:"user"`
}

// getUserCached 用户信息 Cache Aside（T3.2 的另一半：作者信息也是高频读）。
func (s *PostService) getUserCached(ctx context.Context, id int64) (*model.User, error) {
	key := cache.UserKey(id)
	if s.cache != nil {
		var env userEnvelope
		if found, _ := s.cache.GetJSON(ctx, key, &env); found {
			if !env.Found {
				return nil, apierror.ErrNotFound
			}
			return env.User, nil
		}
	}

	u, err := s.users.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.cache != nil {
		env := userEnvelope{Found: u != nil, User: u}
		ttl := postCacheTTL
		if u == nil {
			ttl = nilCacheTTL
		}
		if err := s.cache.SetJSON(ctx, key, env, ttl); err != nil {
			log.Printf("[cache] set %s: %v（降级，不影响请求）", key, err)
		}
	}
	return u, nil
}

// DelUserCache 供其他 service 在用户数据变化时失效缓存（关注计数变更等）。
func (s *PostService) DelUserCache(ctx context.Context, ids ...int64) {
	if s.cache == nil {
		return
	}
	keys := make([]string, 0, len(ids))
	for _, id := range ids {
		keys = append(keys, cache.UserKey(id))
	}
	if err := s.cache.Del(ctx, keys...); err != nil {
		log.Printf("[cache] del users: %v", err)
	}
}

// Delete 删帖（软删除）。权限判断在这里——service 层，不是 handler 层：
// 换个入口（内部任务、gRPC）调用时权限规则依然生效。
func (s *PostService) Delete(ctx context.Context, uid, postID int64) error {
	p, err := s.posts.GetByID(ctx, postID)
	if err != nil {
		return err
	}
	if p == nil {
		return apierror.ErrNotFound
	}
	if p.UserID != uid {
		// 别人的帖子：403 Forbidden（存在但没权限，与 404 语义不同）
		return apierror.ErrForbidden
	}
	if err := s.posts.SoftDelete(ctx, postID); err != nil {
		return err
	}
	// 写时失效：帖子没了，缓存必须同步消失（否则已删帖还能从缓存读到）
	s.invalidatePost(ctx, postID)
	return nil
}

// invalidatePost 写路径的缓存失效（帖子内容或计数变化时调用）。
func (s *PostService) invalidatePost(ctx context.Context, postID int64) {
	if s.cache == nil {
		return
	}
	if err := s.cache.Del(ctx, cache.PostKey(postID)); err != nil {
		log.Printf("[cache] del %s: %v（TTL 兜底自愈）", cache.PostKey(postID), err)
	}
}

// Search 全文搜索（O4）：ngram 分词召回 → 作者批量填充 + 点赞态填充（同 Feed 装配纪律）。
func (s *PostService) Search(ctx context.Context, viewerUID int64, keyword string, limit int) ([]PostView, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return []PostView{}, nil
	}
	if len(keyword) > 64 {
		keyword = keyword[:64] // 搜索词截断，防超长输入
	}
	if limit <= 0 {
		limit = DefaultPageSize
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}

	posts, err := s.posts.SearchByKeyword(ctx, keyword, limit)
	if err != nil {
		return nil, err
	}
	if len(posts) == 0 {
		return []PostView{}, nil
	}

	// 作者批量（防 N+1）
	ids := make([]int64, 0, len(posts))
	seen := make(map[int64]struct{}, len(posts))
	for i := range posts {
		if _, ok := seen[posts[i].UserID]; !ok {
			seen[posts[i].UserID] = struct{}{}
			ids = append(ids, posts[i].UserID)
		}
	}
	authors, err := s.users.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	views := make([]PostView, 0, len(posts))
	for i := range posts {
		views = append(views, NewPostView(&posts[i], authors[posts[i].UserID]))
	}

	// 点赞态批量填充
	if viewerUID > 0 && s.interactions != nil {
		pids := make([]int64, 0, len(views))
		for i := range views {
			pids = append(pids, views[i].ID)
		}
		if likedMap, err := s.interactions.LikedPostIDs(ctx, viewerUID, pids); err == nil {
			for i := range views {
				views[i].Liked = likedMap[views[i].ID]
			}
		}
	}
	return views, nil
}
