package task

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	qzone "github.com/guohuiyuan/qzone-go"
	"github.com/guohuiyuan/qzonewall-go/internal/config"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

// KeepAlive 定期检测QQ空间Cookie有效性并自动刷新
type KeepAlive struct {
	qzoneCfg config.QzoneConfig
	botCfg   config.BotConfig
	client   *qzone.Client
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewKeepAlive 创建keepalive实例
func NewKeepAlive(qzoneCfg config.QzoneConfig, botCfg config.BotConfig, client *qzone.Client) *KeepAlive {
	ctx, cancel := context.WithCancel(context.Background())
	return &KeepAlive{
		qzoneCfg: qzoneCfg,
		botCfg:   botCfg,
		client:   client,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start 启动保活
func (k *KeepAlive) Start() {
	if k.qzoneCfg.KeepAlive <= 0 {
		log.Println("[KeepAlive] 已禁用 (keep_alive <= 0)")
		return
	}
	go k.run()
	log.Printf("[KeepAlive] 启动, 检测间隔=%v", k.qzoneCfg.KeepAlive)
}

// Stop 停止保活
func (k *KeepAlive) Stop() {
	k.cancel()
}

func (k *KeepAlive) run() {
	ticker := time.NewTicker(k.qzoneCfg.KeepAlive)
	defer ticker.Stop()

	for {
		select {
		case <-k.ctx.Done():
			log.Println("[KeepAlive] 已停止")
			return
		case <-ticker.C:
			k.check()
		}
	}
}

func (k *KeepAlive) check() {
	log.Println("[KeepAlive] 检测Cookie有效性...")

	// 用 GetMyFeeds 探测会话是否有效
	_, err := k.client.GetMyFeeds(k.ctx, &qzone.GetFeedsOption{Num: 1})
	if err == nil {
		log.Println("[KeepAlive] Cookie有效")
		return
	}
	log.Printf("[KeepAlive] Cookie可能已过期: %v", err)

	// 尝试通过 ZeroBot 的 GetCookies 刷新
	if k.tryRefreshFromBot() {
		return
	}

	// 通知管理群：需要手动扫码
	k.notifyAdmin("⚠️ QQ空间Cookie已过期，请使用 /扫码 或 /刷新cookie 重新登录")
}

// tryRefreshFromBot 尝试从ZeroBot获取Cookie
func (k *KeepAlive) tryRefreshFromBot() bool {
	var refreshed bool
	zero.RangeBot(func(id int64, ctx *zero.Ctx) bool {
		cookie := ctx.GetCookies("qzone.qq.com")
		if cookie == "" {
			return true // 继续遍历
		}
		if err := k.client.UpdateCookie(cookie); err != nil {
			log.Printf("[KeepAlive] 从Bot(%d)获取的Cookie更新失败: %v", id, err)
			return true
		}
		log.Printf("[KeepAlive] 从Bot(%d)的GetCookies刷新成功, UIN=%d", id, k.client.UIN())
		k.saveCookie(cookie)
		refreshed = true
		return false // 成功，停止遍历
	})
	return refreshed
}

// notifyAdmin 向管理群发送通知
func (k *KeepAlive) notifyAdmin(text string) {
	if k.botCfg.ManageGroup <= 0 {
		return
	}
	zero.RangeBot(func(id int64, ctx *zero.Ctx) bool {
		ctx.SendGroupMessage(k.botCfg.ManageGroup, message.Text(text))
		return false
	})
}

// saveCookie 保存Cookie到文件
func (k *KeepAlive) saveCookie(cookie string) {
	SaveCookie(k.qzoneCfg, cookie)
}

// TryGetCookie 启动时尝试各种方式获取Cookie，返回Cookie字符串
// 优先级: 配置文件Cookie > Cookie文件 > ZeroBot GetCookies > 自动登录(QR)
func TryGetCookie(qzoneCfg config.QzoneConfig) (string, error) {
	// 1. 配置中直接设置的Cookie
	if qzoneCfg.Cookie != "" {
		log.Println("[Init] 使用配置中的Cookie")
		return qzoneCfg.Cookie, nil
	}

	// 2. Cookie文件
	if qzoneCfg.CookieFile != "" {
		if data, err := os.ReadFile(qzoneCfg.CookieFile); err == nil && len(data) > 0 {
			log.Printf("[Init] 从Cookie文件加载: %s", qzoneCfg.CookieFile)
			return string(data), nil
		}
	}

	// 3. 通过 ZeroBot 的 GetCookies (需要Bot已连接)
	var cookie string
	zero.RangeBot(func(id int64, ctx *zero.Ctx) bool {
		c := ctx.GetCookies("qzone.qq.com")
		if c == "" {
			return true
		}
		log.Printf("[Init] 从Bot(%d) GetCookies获取成功", id)
		cookie = c
		return false
	})
	if cookie != "" {
		SaveCookie(qzoneCfg, cookie)
		return cookie, nil
	}

	// 4. 自动QR登录 (headless, 输出到终端)
	if qzoneCfg.AutoLogin {
		return tryQRLogin(qzoneCfg)
	}

	return "", fmt.Errorf("未能获取有效Cookie, 请通过 /扫码 命令或Web管理页面扫码登录")
}

// RefreshCookie 尝试通过ZeroBot刷新Cookie，用于 WithOnSessionExpired 回调
func RefreshCookie(qzoneCfg config.QzoneConfig, botCfg config.BotConfig) func() (string, error) {
	return func() (string, error) {
		log.Println("[SessionExpired] Cookie过期，尝试自动刷新...")
		// 优先从 ZeroBot GetCookies 获取
		var cookie string
		zero.RangeBot(func(id int64, ctx *zero.Ctx) bool {
			c := ctx.GetCookies("qzone.qq.com")
			if c == "" {
				return true
			}
			log.Printf("[SessionExpired] 从Bot(%d) GetCookies刷新成功", id)
			cookie = c
			return false
		})
		if cookie != "" {
			SaveCookie(qzoneCfg, cookie)
			return cookie, nil
		}

		// 通知管理群需要扫码
		if botCfg.ManageGroup > 0 {
			zero.RangeBot(func(id int64, ctx *zero.Ctx) bool {
				ctx.SendGroupMessage(botCfg.ManageGroup, message.Text("⚠️ QQ空间Cookie已过期，GetCookies刷新失败，请使用 /扫码 或 /刷新cookie 重新登录"))
				return false
			})
		}
		return "", fmt.Errorf("Cookie刷新失败，请手动扫码")
	}
}

// SaveCookie 保存Cookie到文件
func SaveCookie(qzoneCfg config.QzoneConfig, cookie string) {
	if qzoneCfg.CookieFile == "" {
		return
	}
	if err := os.WriteFile(qzoneCfg.CookieFile, []byte(cookie), 0600); err != nil {
		log.Printf("[Cookie] 保存失败: %v", err)
	} else {
		log.Printf("[Cookie] 已保存到 %s", qzoneCfg.CookieFile)
	}
}

// tryQRLogin 在终端中进行QR码登录，返回Cookie字符串
func tryQRLogin(qzoneCfg config.QzoneConfig) (string, error) {
	log.Println("[Init] 尝试QR码登录 (请在管理群发送 /扫码)...")

	qr, err := qzone.GetQRCode()
	if err != nil {
		return "", fmt.Errorf("获取二维码失败: %w", err)
	}

	// 保存QR码到文件供查看
	qrFile := "qrcode.png"
	if err := os.WriteFile(qrFile, qr.Image, 0644); err == nil {
		log.Printf("[Init] 二维码已保存到 %s, 请用QQ扫描", qrFile)
	}

	// 轮询等待扫码
	for i := 0; i < 120; i++ {
		time.Sleep(2 * time.Second)
		state, cookie, err := qzone.PollQRLogin(qr)
		if err != nil {
			return "", fmt.Errorf("登录轮询失败: %w", err)
		}
		switch state {
		case qzone.LoginSuccess:
			log.Println("[Init] QR登录成功")
			SaveCookie(qzoneCfg, cookie)
			_ = os.Remove(qrFile)
			return cookie, nil
		case qzone.LoginExpired:
			return "", fmt.Errorf("二维码已过期")
		case qzone.LoginScanned:
			log.Println("[Init] 已扫码，等待确认...")
		}
	}
	return "", fmt.Errorf("登录超时")
}
