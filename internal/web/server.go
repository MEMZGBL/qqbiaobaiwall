package web

import (
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	qzone "github.com/guohuiyuan/qzone-go"
	"github.com/guohuiyuan/qzonewall-go/internal/config"
	"github.com/guohuiyuan/qzonewall-go/internal/model"
	"github.com/guohuiyuan/qzonewall-go/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

// Server 网页服务器
type Server struct {
	cfg       config.WebConfig
	wallCfg   config.WallConfig
	store     *store.Store
	qzClient  *qzone.Client
	tmpl      *template.Template
	server    *http.Server
	uploadDir string

	// QR code login state
	qrMu      sync.Mutex
	qrCode    *qzone.QRCode
	qrStatus  string // "", "waiting", "scanned", "success", "expired", "error"
	qrMessage string
}

// NewServer 创建网页服务器
func NewServer(cfg config.WebConfig, wallCfg config.WallConfig, st *store.Store, qzClient *qzone.Client) *Server {
	return &Server{
		cfg:       cfg,
		wallCfg:   wallCfg,
		store:     st,
		qzClient:  qzClient,
		uploadDir: "uploads",
	}
}

// Start 启动 HTTP 服务
func (s *Server) Start() error {
	// 解析模板
	funcMap := template.FuncMap{
		"formatTime": func(ts int64) string {
			return time.Unix(ts, 0).Format("2006-01-02 15:04")
		},
		"statusText": func(st model.PostStatus) string {
			m := map[model.PostStatus]string{
				model.StatusPending:   "待审核",
				model.StatusApproved:  "已通过",
				model.StatusRejected:  "已拒绝",
				model.StatusFailed:    "失败",
				model.StatusPublished: "已发布",
			}
			if v, ok := m[st]; ok {
				return v
			}
			return string(st)
		},
		"statusClass": func(st model.PostStatus) string {
			m := map[model.PostStatus]string{
				model.StatusPending:   "pending",
				model.StatusApproved:  "approved",
				model.StatusRejected:  "rejected",
				model.StatusFailed:    "failed",
				model.StatusPublished: "published",
			}
			return m[st]
		},
		"hasImages": func(imgs []string) bool { return len(imgs) > 0 },
	}

	var err error
	s.tmpl, err = template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return fmt.Errorf("parse templates: %w", err)
	}

	// 确保上传目录存在
	if err := os.MkdirAll(s.uploadDir, 0755); err != nil {
		return fmt.Errorf("create upload dir: %w", err)
	}

	// 初始化默认管理员账号
	if err := s.initAdmin(); err != nil {
		log.Printf("[Web] 初始化管理员账号失败: %v", err)
	}

	mux := http.NewServeMux()

	// 页面路由
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/logout", s.handleLogout)
	mux.HandleFunc("/submit", s.handleSubmitPage)
	mux.HandleFunc("/admin", s.handleAdminPage)

	// API 路由
	mux.HandleFunc("/api/submit", s.handleAPISubmit)
	mux.HandleFunc("/api/approve", s.handleAPIApprove)
	mux.HandleFunc("/api/reject", s.handleAPIReject)
	mux.HandleFunc("/api/qrcode", s.handleAPIQRCode)
	mux.HandleFunc("/api/qrcode/status", s.handleAPIQRStatus)
	mux.HandleFunc("/api/health", s.handleAPIHealth)

	// 静态文件
	mux.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir(s.uploadDir))))

	s.server = &http.Server{
		Addr:    s.cfg.Addr,
		Handler: mux,
	}

	go func() {
		log.Printf("[Web] 监听 %s", s.cfg.Addr)
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[Web] 服务异常: %v", err)
		}
	}()
	return nil
}

// Stop 停止
func (s *Server) Stop() {
	if s.server != nil {
		_ = s.server.Close()
		log.Println("[Web] 已停止")
	}
}

// ──────────────────────────────────────────
// 初始化
// ──────────────────────────────────────────

func (s *Server) initAdmin() error {
	count, err := s.store.AccountCount()
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	salt := randomHex(16)
	hash := hashPassword(s.cfg.AdminPass, salt)
	return s.store.CreateAccount(s.cfg.AdminUser, hash, salt, "admin")
}

// ──────────────────────────────────────────
// 页面处理
// ──────────────────────────────────────────

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	account := s.currentAccount(r)
	if account == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	if account.IsAdmin() {
		http.Redirect(w, r, "/admin", http.StatusFound)
	} else {
		http.Redirect(w, r, "/submit", http.StatusFound)
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.tmpl.ExecuteTemplate(w, "login.html", nil)
		return
	}

	// POST: 登录
	username := r.FormValue("username")
	password := r.FormValue("password")

	account, err := s.store.GetAccount(username)
	if err != nil || account == nil {
		s.tmpl.ExecuteTemplate(w, "login.html", map[string]string{"Error": "用户名或密码错误"})
		return
	}
	if hashPassword(password, account.Salt) != account.PasswordHash {
		s.tmpl.ExecuteTemplate(w, "login.html", map[string]string{"Error": "用户名或密码错误"})
		return
	}

	// 创建会话
	token := randomHex(32)
	expire := time.Now().Add(24 * time.Hour).Unix()
	if err := s.store.CreateSession(token, account.ID, expire); err != nil {
		s.tmpl.ExecuteTemplate(w, "login.html", map[string]string{"Error": "登录失败"})
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		MaxAge:   86400,
		HttpOnly: true,
	})

	if account.IsAdmin() {
		http.Redirect(w, r, "/admin", http.StatusFound)
	} else {
		http.Redirect(w, r, "/submit", http.StatusFound)
	}
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("session"); err == nil {
		_ = s.store.DeleteSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:   "session",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	http.Redirect(w, r, "/login", http.StatusFound)
}

func (s *Server) handleSubmitPage(w http.ResponseWriter, r *http.Request) {
	account := s.currentAccount(r)
	if account == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	data := map[string]interface{}{
		"Account":   account,
		"MaxImages": s.wallCfg.MaxImages,
		"Message":   r.URL.Query().Get("msg"),
	}
	s.tmpl.ExecuteTemplate(w, "user.html", data)
}

func (s *Server) handleAdminPage(w http.ResponseWriter, r *http.Request) {
	account := s.currentAccount(r)
	if account == nil || !account.IsAdmin() {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	// 获取投稿列表
	statusFilter := r.URL.Query().Get("status")
	var posts []*model.Post
	var err error
	if statusFilter != "" {
		posts, err = s.store.ListByStatus(model.PostStatus(statusFilter))
	} else {
		posts, err = s.store.ListAll(100, 0)
	}
	if err != nil {
		log.Printf("[Web] 查询投稿失败: %v", err)
	}

	pendingCount, _ := s.store.CountByStatus(model.StatusPending)

	data := map[string]interface{}{
		"Account":      account,
		"Posts":        posts,
		"PendingCount": pendingCount,
		"StatusFilter": statusFilter,
		"CookieValid":  s.qzClient != nil && s.qzClient.UIN() > 0,
		"QzoneUIN":     int64(0),
		"Message":      r.URL.Query().Get("msg"),
	}
	if s.qzClient != nil {
		data["QzoneUIN"] = s.qzClient.UIN()
	}

	s.tmpl.ExecuteTemplate(w, "admin.html", data)
}

// ──────────────────────────────────────────
// API 处理
// ──────────────────────────────────────────

func (s *Server) handleAPISubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResp(w, 405, false, "仅支持POST")
		return
	}

	account := s.currentAccount(r)
	if account == nil {
		jsonResp(w, 401, false, "请先登录")
		return
	}

	// 解析 multipart form
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		jsonResp(w, 400, false, "请求过大")
		return
	}

	text := r.FormValue("text")
	name := r.FormValue("name")
	anon := r.FormValue("anon") == "on" || r.FormValue("anon") == "true"

	if name == "" {
		name = account.Username
	}

	// 处理上传图片
	var images []string
	files := r.MultipartForm.File["images"]
	for _, fh := range files {
		if len(images) >= s.wallCfg.MaxImages {
			break
		}
		f, err := fh.Open()
		if err != nil {
			continue
		}
		defer f.Close()

		// 保存到 uploads/
		ext := filepath.Ext(fh.Filename)
		if ext == "" {
			ext = ".jpg"
		}
		filename := fmt.Sprintf("%d_%s%s", time.Now().UnixNano(), randomHex(8), ext)
		dst, err := os.Create(filepath.Join(s.uploadDir, filename))
		if err != nil {
			continue
		}
		_, _ = io.Copy(dst, f)
		dst.Close()

		images = append(images, "/uploads/"+filename)
	}

	if text == "" && len(images) == 0 {
		jsonResp(w, 400, false, "内容不能为空")
		return
	}

	post := &model.Post{
		Name:       name,
		Text:       text,
		Images:     images,
		Anon:       anon,
		Status:     model.StatusPending,
		CreateTime: time.Now().Unix(),
	}
	if err := s.store.SavePost(post); err != nil {
		jsonResp(w, 500, false, "保存失败")
		return
	}

	log.Printf("[Web] 收到投稿 #%d from %s", post.ID, name)
	jsonRespData(w, 200, true, fmt.Sprintf("投稿成功！编号 #%d，等待审核", post.ID), post.ID)
}

func (s *Server) handleAPIApprove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResp(w, 405, false, "仅支持POST")
		return
	}
	account := s.currentAccount(r)
	if account == nil || !account.IsAdmin() {
		jsonResp(w, 403, false, "无权限")
		return
	}

	idStr := r.FormValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		jsonResp(w, 400, false, "编号格式错误")
		return
	}
	post, err := s.store.GetPost(id)
	if err != nil || post == nil {
		jsonResp(w, 404, false, "稿件不存在")
		return
	}
	post.Status = model.StatusApproved
	if err := s.store.SavePost(post); err != nil {
		jsonResp(w, 500, false, "更新失败")
		return
	}
	jsonResp(w, 200, true, fmt.Sprintf("稿件 #%d 已通过", id))
}

func (s *Server) handleAPIReject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonResp(w, 405, false, "仅支持POST")
		return
	}
	account := s.currentAccount(r)
	if account == nil || !account.IsAdmin() {
		jsonResp(w, 403, false, "无权限")
		return
	}

	idStr := r.FormValue("id")
	reason := r.FormValue("reason")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		jsonResp(w, 400, false, "编号格式错误")
		return
	}
	post, err := s.store.GetPost(id)
	if err != nil || post == nil {
		jsonResp(w, 404, false, "稿件不存在")
		return
	}
	post.Status = model.StatusRejected
	post.Reason = reason
	if err := s.store.SavePost(post); err != nil {
		jsonResp(w, 500, false, "更新失败")
		return
	}
	jsonResp(w, 200, true, fmt.Sprintf("稿件 #%d 已拒绝", id))
}

// handleAPIQRCode 获取QQ空间登录二维码
func (s *Server) handleAPIQRCode(w http.ResponseWriter, r *http.Request) {
	account := s.currentAccount(r)
	if account == nil || !account.IsAdmin() {
		jsonResp(w, 403, false, "无权限")
		return
	}

	qr, err := qzone.GetQRCode()
	if err != nil {
		jsonResp(w, 500, false, "获取二维码失败: "+err.Error())
		return
	}

	s.qrMu.Lock()
	s.qrCode = qr
	s.qrStatus = "waiting"
	s.qrMessage = ""
	s.qrMu.Unlock()

	// 后台轮询
	go s.pollQRLogin()

	// 返回二维码图片为 PNG
	w.Header().Set("Content-Type", "image/png")
	w.Write(qr.Image)
}

func (s *Server) pollQRLogin() {
	s.qrMu.Lock()
	qr := s.qrCode
	s.qrMu.Unlock()
	if qr == nil {
		return
	}

	for i := 0; i < 120; i++ {
		time.Sleep(2 * time.Second)
		state, cookie, err := qzone.PollQRLogin(qr)
		if err != nil {
			s.qrMu.Lock()
			s.qrStatus = "error"
			s.qrMessage = err.Error()
			s.qrMu.Unlock()
			return
		}
		switch state {
		case qzone.LoginSuccess:
			if err := s.qzClient.UpdateCookie(cookie); err != nil {
				s.qrMu.Lock()
				s.qrStatus = "error"
				s.qrMessage = "Cookie更新失败: " + err.Error()
				s.qrMu.Unlock()
				return
			}
			s.qrMu.Lock()
			s.qrStatus = "success"
			s.qrMessage = fmt.Sprintf("登录成功, UIN=%d", s.qzClient.UIN())
			s.qrMu.Unlock()
			return
		case qzone.LoginExpired:
			s.qrMu.Lock()
			s.qrStatus = "expired"
			s.qrMessage = "二维码已过期"
			s.qrMu.Unlock()
			return
		case qzone.LoginScanned:
			s.qrMu.Lock()
			s.qrStatus = "scanned"
			s.qrMu.Unlock()
		}
	}

	s.qrMu.Lock()
	s.qrStatus = "expired"
	s.qrMessage = "登录超时"
	s.qrMu.Unlock()
}

// handleAPIQRStatus 返回二维码登录状态（AJAX轮询）
func (s *Server) handleAPIQRStatus(w http.ResponseWriter, r *http.Request) {
	s.qrMu.Lock()
	status := s.qrStatus
	msg := s.qrMessage
	s.qrMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  status,
		"message": msg,
	})
}

func (s *Server) handleAPIHealth(w http.ResponseWriter, r *http.Request) {
	jsonResp(w, 200, true, "ok")
}

// ──────────────────────────────────────────
// 认证辅助
// ──────────────────────────────────────────

func (s *Server) currentAccount(r *http.Request) *model.Account {
	c, err := r.Cookie("session")
	if err != nil {
		return nil
	}
	accountID, err := s.store.GetSession(c.Value)
	if err != nil || accountID == 0 {
		return nil
	}
	account, err := s.store.GetAccountByID(accountID)
	if err != nil {
		return nil
	}
	return account
}

// ──────────────────────────────────────────
// 工具函数
// ──────────────────────────────────────────

func hashPassword(password, salt string) string {
	h := sha256.New()
	h.Write([]byte(salt + password))
	return hex.EncodeToString(h.Sum(nil))
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func jsonResp(w http.ResponseWriter, status int, ok bool, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      ok,
		"message": msg,
	})
}

func jsonRespData(w http.ResponseWriter, status int, ok bool, msg string, postID int64) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      ok,
		"message": msg,
		"post_id": postID,
	})
}

// RegisterUser 注册新用户（供外部调用）
func (s *Server) RegisterUser(username, password string) error {
	existing, _ := s.store.GetAccount(username)
	if existing != nil {
		return fmt.Errorf("用户名已存在")
	}
	salt := randomHex(16)
	hash := hashPassword(password, salt)
	return s.store.CreateAccount(username, hash, salt, "user")
}

// CookieFile 配置中的cookie文件路径, 暴露给QR login
func (s *Server) SetCookieFile(cookieFile string) {
	// 用于QR登录成功后保存cookie
	// 在 pollQRLogin 中已经通过 qzClient.UpdateCookie 更新了内存
	// 如果需要持久化, 外部在创建Server时设置
}

// GetUploadDir 返回上传目录路径
func (s *Server) GetUploadDir() string {
	return s.uploadDir
}

// SplitStatusFilter 辅助模板使用
func splitStatusFilter(s string) []string {
	return strings.Split(s, ",")
}
