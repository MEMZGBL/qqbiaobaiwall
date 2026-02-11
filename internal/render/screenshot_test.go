package render

import (
	"os"
	"testing"
	"time"

	"github.com/guohuiyuan/qzonewall-go/internal/model"
)

// TestRenderPost 测试图文渲染功能
// 运行方法: go test -v ./internal/render/ -run TestRenderPost
func TestRenderPost(t *testing.T) {
	// 1. 初始化渲染器
	// 确保 internal/render/font.ttf 存在 (推荐使用 微软雅黑 msyh.ttc 改名而来)
	r := NewRenderer("", 32.0)

	if !r.Available() {
		t.Fatal("❌ 渲染器不可用，请检查 font.ttf 是否正确嵌入")
	}

	// 2. 构造模拟投稿数据
	// ★★★ 修改点：使用 QQ 头像作为图片源，保证下载成功 ★★★
	stableImgURL := "https://q1.qlogo.cn/g?b=qq&nk=10001&s=640"

	post := &model.Post{
		ID:      10086,
		UIN:     10001,
		Name:    "测试用户(Test)",
		GroupID: 123456,
		// 测试 Emoji (注意：需使用微软雅黑等支持Emoji的字体，且显示为黑白)
		Text: "这是一条测试内容。\nHello World! 👋\nEmoji测试：🚀 😄 🐛\n下面应该是两张一模一样的头像图片 👇",
		Images: []string{
			stableImgURL, // 图1：头像
			stableImgURL, // 图2：头像
		},
		Anon:       false,
		Status:     model.StatusPending,
		CreateTime: time.Now().Unix(),
	}

	t.Logf("开始渲染稿件 #%d...", post.ID)

	// 3. 执行渲染
	startTime := time.Now()
	data, err := r.RenderPost(post)
	duration := time.Since(startTime)

	// 4. 验证结果
	if err != nil {
		t.Fatalf("❌ 渲染失败: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("❌ 渲染结果为空 (0 bytes)")
	}

	// 5. 保存图片到本地
	outputFile := "test_render_result.jpg"
	err = os.WriteFile(outputFile, data, 0644)
	if err != nil {
		t.Fatalf("❌ 保存测试图片失败: %v", err)
	}

	t.Logf("✅ 渲染成功！")
	t.Logf("⏱️ 耗时: %v", duration)
	t.Logf("📂 图片已保存为: %s/%s", "internal/render", outputFile)
	t.Logf("👉 请务必使用「微软雅黑」作为 font.ttf 以支持 Emoji 显示。")
}
