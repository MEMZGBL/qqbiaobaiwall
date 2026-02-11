package task

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	qzone "github.com/guohuiyuan/qzone-go"
	"github.com/guohuiyuan/qzonewall-go/internal/config"
	"github.com/guohuiyuan/qzonewall-go/internal/model"
	"github.com/guohuiyuan/qzonewall-go/internal/render"
	"github.com/guohuiyuan/qzonewall-go/internal/store"
)

// Worker 从 SQLite 轮询已通过的投稿并发布到QQ空间
type Worker struct {
	cfg         config.WorkerConfig
	wallCfg     config.WallConfig
	client      *qzone.Client
	store       *store.Store
	renderer    *render.Renderer
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	lastPublish time.Time
	mu          sync.Mutex
}

// NewWorker 创建工作者
func NewWorker(
	cfg config.WorkerConfig,
	wallCfg config.WallConfig,
	client *qzone.Client,
	st *store.Store,
	renderer *render.Renderer,
) *Worker {
	ctx, cancel := context.WithCancel(context.Background())
	return &Worker{
		cfg:      cfg,
		wallCfg:  wallCfg,
		client:   client,
		store:    st,
		renderer: renderer,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start 启动 worker goroutine
func (w *Worker) Start() {
	for i := 0; i < w.cfg.Workers; i++ {
		w.wg.Add(1)
		go w.run(i)
	}
	log.Printf("[Worker] 启动 %d 个工作协程, 轮询间隔=%v", w.cfg.Workers, w.cfg.PollInterval)
}

// Stop 优雅停止
func (w *Worker) Stop() {
	w.cancel()
	w.wg.Wait()
	log.Println("[Worker] 已停止")
}

func (w *Worker) run(id int) {
	defer w.wg.Done()
	log.Printf("[Worker-%d] 开始轮询", id)

	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			log.Printf("[Worker-%d] 收到停止信号", id)
			return
		case <-ticker.C:
			w.pollAndPublish(id)
		}
	}
}

func (w *Worker) pollAndPublish(workerID int) {
	// 拉取已通过但未发布的投稿 (tid='')
	posts, err := w.store.GetApprovedPosts(1)
	if err != nil {
		log.Printf("[Worker-%d] 查询失败: %v", workerID, err)
		return
	}
	if len(posts) == 0 {
		return
	}

	post := posts[0]
	log.Printf("[Worker-%d] 处理稿件 #%d", workerID, post.ID)

	// 频率限制
	w.waitRateLimit()

	// 发布（带重试）
	var lastErr error
	for retry := 0; retry <= w.cfg.RetryCount; retry++ {
		if retry > 0 {
			log.Printf("[Worker-%d] 重试第 %d 次...", workerID, retry)
			time.Sleep(w.cfg.RetryDelay)
		}

		err := w.publish(post)
		if err == nil {
			log.Printf("[Worker-%d] 稿件 #%d 发布成功, tid=%s", workerID, post.ID, post.TID)
			return
		}
		lastErr = err
		log.Printf("[Worker-%d] 发布失败: %v", workerID, err)
	}

	// 所有重试失败
	post.Status = model.StatusFailed
	post.Reason = fmt.Sprintf("发布失败: %v", lastErr)
	if err := w.store.SavePost(post); err != nil {
		log.Printf("[Worker-%d] 更新状态失败: %v", workerID, err)
	}
	log.Printf("[Worker-%d] 稿件 #%d 最终发布失败: %v", workerID, post.ID, lastErr)
}

// publish 发布到QQ空间
func (w *Worker) publish(post *model.Post) error {
	// 构建说说文本
	text := post.Text
	if w.wallCfg.ShowAuthor && !post.Anon {
		text = fmt.Sprintf("【来自 %s 的投稿】\n\n%s", post.ShowName(), text)
	}

	// 尝试渲染截图作为首图
	var imageBytes [][]byte
	if w.renderer.Available() {
		if screenshot, err := w.renderer.RenderPost(post); err == nil {
			imageBytes = append(imageBytes, screenshot)
		}
	}

	var opt *qzone.PublishOption
	if len(imageBytes) > 0 || len(post.Images) > 0 {
		opt = &qzone.PublishOption{}
		if len(imageBytes) > 0 {
			opt.ImageBytes = imageBytes
		}
		// 同时附带原图URL
		if len(post.Images) > 0 {
			opt.ImageURLs = post.Images
		}
	}

	resp, err := w.client.Publish(w.ctx, text, opt)
	if err != nil {
		return fmt.Errorf("publish: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("publish failed: code=%d, msg=%s", resp.Code, resp.Message)
	}

	// 回填 TID
	if tid := resp.GetString("tid"); tid != "" {
		post.TID = tid
	} else if tid := resp.GetString("t1_tid"); tid != "" {
		post.TID = tid
	} else {
		// 没有获取到TID, 用占位符标记已发布
		post.TID = fmt.Sprintf("published_%d", time.Now().Unix())
	}

	post.Status = model.StatusPublished
	if err := w.store.SavePost(post); err != nil {
		log.Printf("[Worker] 回填TID失败: %v", err)
	}

	// 记录发布时间
	w.mu.Lock()
	w.lastPublish = time.Now()
	w.mu.Unlock()

	return nil
}

// waitRateLimit 等待频率限制
func (w *Worker) waitRateLimit() {
	w.mu.Lock()
	last := w.lastPublish
	w.mu.Unlock()

	if last.IsZero() {
		return
	}
	elapsed := time.Since(last)
	if elapsed < w.cfg.RateLimit {
		wait := w.cfg.RateLimit - elapsed
		log.Printf("[Worker] 频率限制，等待 %v", wait)
		time.Sleep(wait)
	}
}
