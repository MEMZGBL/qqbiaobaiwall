package model

import (
	"fmt"
	"strings"
	"time"
)

// ──────────────────────────────────────────
// PostStatus 稿件状态
// ──────────────────────────────────────────

type PostStatus string

const (
	StatusPending   PostStatus = "pending"   // 待审核
	StatusApproved  PostStatus = "approved"  // 已通过（等待发布）
	StatusRejected  PostStatus = "rejected"  // 已拒绝
	StatusFailed    PostStatus = "failed"    // 发布失败
	StatusPublished PostStatus = "published" // 已发布到QQ空间
)

// ──────────────────────────────────────────
// Post 投稿/说说
// ──────────────────────────────────────────

type Post struct {
	ID         int64      `json:"id"`
	TID        string     `json:"tid,omitempty"`      // QQ空间说说ID（发布后回填）
	UIN        int64      `json:"uin"`                // 投稿者QQ
	Name       string     `json:"name"`               // 投稿者昵称
	GroupID    int64      `json:"group_id,omitempty"` // 来源群号
	Text       string     `json:"text"`               // 文字内容
	Images     []string   `json:"images,omitempty"`   // 图片URL列表
	Anon       bool       `json:"anon"`               // 是否匿名
	Status     PostStatus `json:"status"`
	Reason     string     `json:"reason,omitempty"`     // 拒绝理由
	AvatarURL  string     `json:"avatar_url,omitempty"` // 头像URL
	CreateTime int64      `json:"create_time"`
	UpdateTime int64      `json:"update_time,omitempty"`
}

// ShowName 显示名称
func (p *Post) ShowName() string {
	if p.Anon {
		return "匿名用户"
	}
	return p.Name
}

// QQAvatarURL 返回QQ头像地址
func (p *Post) QQAvatarURL() string {
	if p.AvatarURL != "" {
		return p.AvatarURL
	}
	if p.UIN > 0 {
		return fmt.Sprintf("https://q1.qlogo.cn/g?b=qq&nk=%d&s=640", p.UIN)
	}
	return ""
}

// Summary 摘要信息
func (p *Post) Summary() string {
	t := time.Unix(p.CreateTime, 0).Format("01-02 15:04")
	var b strings.Builder
	fmt.Fprintf(&b, "#%d %s [%s] %s\n", p.ID, p.ShowName(), p.Status, t)
	if p.Text != "" {
		text := p.Text
		if len([]rune(text)) > 60 {
			text = string([]rune(text)[:60]) + "..."
		}
		b.WriteString(text)
		b.WriteByte('\n')
	}
	if len(p.Images) > 0 {
		fmt.Fprintf(&b, "[%d张图片]", len(p.Images))
	}
	return b.String()
}

// String 完整信息
func (p *Post) String() string {
	t := time.Unix(p.CreateTime, 0).Format("2006-01-02 15:04")
	var b strings.Builder
	fmt.Fprintf(&b, "【#%d】%s 投稿于 %s\n", p.ID, p.ShowName(), t)
	if p.Text != "" {
		b.WriteString(p.Text)
		b.WriteByte('\n')
	}
	if len(p.Images) > 0 {
		for i, img := range p.Images {
			fmt.Fprintf(&b, "[图片%d] %s\n", i+1, img)
		}
	}
	if p.Status == StatusPending {
		fmt.Fprintf(&b, "\n⏳ 待审核")
	}
	if p.Reason != "" {
		fmt.Fprintf(&b, "\n理由: %s", p.Reason)
	}
	return b.String()
}

// ──────────────────────────────────────────
// Account 网页账号
// ──────────────────────────────────────────

type Account struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"`
	Salt         string `json:"-"`
	Role         string `json:"role"` // "admin" or "user"
	CreateTime   int64  `json:"create_time"`
}

// IsAdmin 是否为管理员
func (a *Account) IsAdmin() bool {
	return a.Role == "admin"
}
