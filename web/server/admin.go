package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/platform"
	"golang.org/x/crypto/bcrypt"
)

const adminCookieName = "musicweb_admin"

type adminPlatformStatus struct {
	Platform          string                `json:"platform"`
	DisplayName       string                `json:"display_name"`
	LoggedIn          bool                  `json:"logged_in"`
	UserID            string                `json:"user_id,omitempty"`
	Nickname          string                `json:"nickname,omitempty"`
	Summary           string                `json:"summary,omitempty"`
	AuthMode          string                `json:"auth_mode,omitempty"`
	SessionSource     string                `json:"session_source,omitempty"`
	CanCheckCookie    bool                  `json:"can_check_cookie"`
	CanRenewCookie    bool                  `json:"can_renew_cookie"`
	SupportedLogins   []string              `json:"supported_logins,omitempty"`
	SupportsCookie    bool                  `json:"supports_cookie"`
	SupportsQR        bool                  `json:"supports_qr"`
	SupportsCheck     bool                  `json:"supports_check"`
	SupportsRenew     bool                  `json:"supports_renew"`
	SupportsAutoRenew bool                  `json:"supports_auto_renew"`
	SupportsLanguage  bool                  `json:"supports_language"`
	SupportsSignIn    bool                  `json:"supports_sign_in"`
	AutoRenew         *adminAutoRenewStatus `json:"auto_renew,omitempty"`
	ExpiresAt         *time.Time            `json:"expires_at,omitempty"`
	SpotifySettings   *adminSpotifySettings `json:"spotify_settings,omitempty"`
}

// adminSpotifySettings intentionally contains only configuration presence, not
// credentials themselves. Secrets must never be returned to the browser.
type adminSpotifySettings struct {
	ClientIDConfigured     bool   `json:"client_id_configured"`
	ClientSecretConfigured bool   `json:"client_secret_configured"`
	SPDCConfigured         bool   `json:"sp_dc_configured"`
	Market                 string `json:"market,omitempty"`
}

type adminAutoRenewStatus struct {
	Enabled         bool `json:"enabled"`
	IntervalSeconds int  `json:"interval_seconds"`
}

type qrSessionState struct {
	ID        string    `json:"id"`
	Platform  string    `json:"platform"`
	State     string    `json:"state"`
	Message   string    `json:"message"`
	Caption   string    `json:"caption,omitempty"`
	Final     bool      `json:"final"`
	ImageURL  string    `json:"image_url,omitempty"`
	ImageData string    `json:"image_data,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	cancel    func()
}

func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/admin/login" && r.Method == http.MethodGet:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(adminLoginHTML))
	case r.URL.Path == "/admin/login" && r.Method == http.MethodPost:
		s.handleAdminLogin(w, r)
	case r.URL.Path == "/admin/logout" && r.Method == http.MethodPost:
		http.SetCookie(w, &http.Cookie{Name: adminCookieName, Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case r.URL.Path == "/admin" && r.Method == http.MethodGet:
		if !s.requireAdmin(w, r) {
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(adminHTML))
	case strings.HasPrefix(r.URL.Path, "/admin/api/"):
		if !s.requireAdmin(w, r) {
			return
		}
		s.handleAdminAPI(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	var username, password string
	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		username, password = body.Username, body.Password
	} else {
		_ = r.ParseForm()
		username, password = r.Form.Get("username"), r.Form.Get("password")
	}
	if !s.verifyAdminPassword(username, password) {
		writeError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	token := s.signAdminToken(username, time.Now().Add(24*time.Hour))
	http.SetCookie(w, &http.Cookie{
		Name:     adminCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(24 * time.Hour),
	})
	if strings.Contains(ct, "application/json") {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	http.Redirect(w, r, "/admin", http.StatusFound)
}

func (s *Server) verifyAdminPassword(username, password string) bool {
	cfg := s.core.Config
	wantUser := strings.TrimSpace(cfg.GetString("WebAdminUsername"))
	if wantUser == "" {
		wantUser = "admin"
	}
	if subtle.ConstantTimeCompare([]byte(username), []byte(wantUser)) != 1 {
		return false
	}
	hash := strings.TrimSpace(cfg.GetString("WebAdminPasswordHash"))
	if hash != "" {
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
	}
	wantPass := cfg.GetString("WebAdminPassword")
	if wantPass == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(password), []byte(wantPass)) == 1
}

func (s *Server) signAdminToken(username string, expires time.Time) string {
	payload := username + "|" + expires.UTC().Format(time.RFC3339)
	sig := s.sign(payload)
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + sig
}

func (s *Server) verifyAdminToken(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	payload := string(raw)
	if !hmac.Equal([]byte(s.sign(payload)), []byte(parts[1])) {
		return false
	}
	fields := strings.Split(payload, "|")
	if len(fields) != 2 {
		return false
	}
	expires, err := time.Parse(time.RFC3339, fields[1])
	return err == nil && time.Now().Before(expires)
}

func (s *Server) sign(payload string) string {
	secret := "change-me"
	if s.core != nil && s.core.Config != nil {
		if v := strings.TrimSpace(s.core.Config.GetString("WebSessionSecret")); v != "" {
			secret = v
		}
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	cookie, err := r.Cookie(adminCookieName)
	if err != nil || !s.verifyAdminToken(cookie.Value) {
		if strings.HasPrefix(r.URL.Path, "/admin/api/") {
			writeError(w, http.StatusUnauthorized, "需要管理员登录")
			return false
		}
		http.Redirect(w, r, "/admin/login", http.StatusFound)
		return false
	}
	return true
}

func (s *Server) handleAdminAPI(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/admin/api/platforms/status" && r.Method == http.MethodGet:
		s.handleAdminPlatformStatus(w, r)
	case r.URL.Path == "/admin/api/downloads" && r.Method == http.MethodGet:
		s.handleAdminDownloadJobs(w, r)
	case r.URL.Path == "/admin/api/downloads/cleanup" && r.Method == http.MethodPost:
		s.handleAdminDownloadCleanup(w, r)
	case strings.HasPrefix(r.URL.Path, "/admin/api/platforms/") && strings.HasSuffix(r.URL.Path, "/cookie") && r.Method == http.MethodPost:
		s.handleAdminCookieImport(w, r)
	case r.URL.Path == "/admin/api/platforms/spotify/settings" && r.Method == http.MethodPost:
		s.handleAdminSpotifySettings(w, r)
	case strings.HasPrefix(r.URL.Path, "/admin/api/platforms/") && strings.HasSuffix(r.URL.Path, "/check") && r.Method == http.MethodPost:
		s.handleAdminCookieCheck(w, r)
	case strings.HasPrefix(r.URL.Path, "/admin/api/platforms/") && strings.HasSuffix(r.URL.Path, "/renew") && r.Method == http.MethodPost:
		s.handleAdminCookieRenew(w, r)
	case strings.HasPrefix(r.URL.Path, "/admin/api/platforms/") && strings.HasSuffix(r.URL.Path, "/auto") && (r.Method == http.MethodGet || r.Method == http.MethodPost):
		s.handleAdminAutoRenew(w, r)
	case strings.HasPrefix(r.URL.Path, "/admin/api/platforms/") && strings.HasSuffix(r.URL.Path, "/language") && (r.Method == http.MethodGet || r.Method == http.MethodPost):
		s.handleAdminLanguage(w, r)
	case strings.HasPrefix(r.URL.Path, "/admin/api/platforms/") && strings.HasSuffix(r.URL.Path, "/signin") && r.Method == http.MethodPost:
		s.handleAdminSignIn(w, r)
	case strings.HasPrefix(r.URL.Path, "/admin/api/platforms/") && strings.HasSuffix(r.URL.Path, "/qr/start") && r.Method == http.MethodPost:
		s.handleAdminQRStart(w, r)
	case strings.HasPrefix(r.URL.Path, "/admin/api/qr/") && strings.HasSuffix(r.URL.Path, "/status") && r.Method == http.MethodGet:
		s.handleAdminQRStatus(w, r)
	case strings.HasPrefix(r.URL.Path, "/admin/api/qr/") && strings.HasSuffix(r.URL.Path, "/cancel") && r.Method == http.MethodPost:
		s.handleAdminQRCancel(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleAdminDownloadJobs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 {
		limit = 50
	}
	jobs, err := s.music.ListJobs(r.Context(), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

func (s *Server) handleAdminDownloadCleanup(w http.ResponseWriter, r *http.Request) {
	files, jobs, err := s.music.CleanupExpired(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files_removed": files, "jobs_removed": jobs})
}

func (s *Server) handleAdminPlatformStatus(w http.ResponseWriter, r *http.Request) {
	if s.core == nil || s.core.PlatformManager == nil {
		writeError(w, http.StatusInternalServerError, "platform manager not configured")
		return
	}
	statuses := make([]adminPlatformStatus, 0)
	for _, name := range s.core.PlatformManager.List() {
		plat := s.core.PlatformManager.Get(name)
		if plat == nil {
			continue
		}
		meta, _ := s.core.PlatformManager.Meta(name)
		base := adminPlatformStatus{
			Platform:          name,
			DisplayName:       fallback(meta.DisplayName, name),
			SupportsCookie:    implementsCookieImport(plat),
			SupportsQR:        implementsQRLogin(plat),
			SupportsCheck:     implementsCookieCheck(plat),
			SupportsRenew:     implementsCookieRenew(plat),
			SupportsAutoRenew: implementsAutoRenew(plat),
			SupportsLanguage:  implementsLanguage(plat),
			SupportsSignIn:    implementsSignIn(plat),
		}
		if name == "spotify" && s.core.Config != nil {
			base.SpotifySettings = spotifySettingsFromConfig(s.core.Config)
		}
		if auto, ok := plat.(platform.AutoRenewer); ok {
			if st, err := auto.GetAutoRenewStatus(r.Context()); err == nil {
				base.AutoRenew = autoRenewPayload(st)
			}
		}
		if methods, ok := plat.(platform.LoginMethodProvider); ok {
			base.SupportedLogins = methods.SupportedLoginMethods()
		}
		provider, ok := plat.(platform.AccountStatusProvider)
		if !ok {
			base.Summary = "该平台未提供账号状态接口"
			statuses = append(statuses, base)
			continue
		}
		st, err := provider.AccountStatus(context.Background())
		if err != nil {
			base.Summary = err.Error()
			statuses = append(statuses, base)
			continue
		}
		base.Platform = fallback(st.Platform, name)
		base.DisplayName = fallback(st.DisplayName, base.DisplayName)
		base.LoggedIn = st.LoggedIn
		base.UserID = st.UserID
		base.Nickname = st.Nickname
		base.Summary = st.Summary
		base.AuthMode = st.AuthMode
		base.SessionSource = st.SessionSource
		base.CanCheckCookie = st.CanCheckCookie
		base.CanRenewCookie = st.CanRenewCookie
		base.SupportsCheck = base.SupportsCheck || st.CanCheckCookie
		base.SupportsRenew = base.SupportsRenew || st.CanRenewCookie
		if len(st.SupportedLogins) > 0 {
			base.SupportedLogins = st.SupportedLogins
		}
		if st.ExpiresAt != nil {
			base.ExpiresAt = st.ExpiresAt
		}
		statuses = append(statuses, base)
	}
	writeJSON(w, http.StatusOK, map[string]any{"statuses": statuses})
}

// handleAdminSpotifySettings persists Spotify Web API credentials and the
// sp_dc cookie through the authenticated admin page. The running plugin is
// intentionally not mutated in place: its Web API and native clients are
// constructed together at startup, so a service restart applies the saved
// values atomically.
func (s *Server) handleAdminSpotifySettings(w http.ResponseWriter, r *http.Request) {
	if s.core == nil || s.core.Config == nil {
		writeError(w, http.StatusInternalServerError, "配置未加载")
		return
	}
	var body struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		SPDC         string `json:"sp_dc"`
		Market       string `json:"market"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "JSON 格式错误")
		return
	}

	pairs := make(map[string]string)
	clientID := strings.TrimSpace(body.ClientID)
	clientSecret := strings.TrimSpace(body.ClientSecret)
	if (clientID == "") != (clientSecret == "") {
		writeError(w, http.StatusBadRequest, "Client ID 和 Client Secret 需要同时填写")
		return
	}
	if clientID != "" {
		pairs["client_id"] = clientID
		pairs["client_secret"] = clientSecret
	}
	if cookie := spotifySPDCValue(body.SPDC); cookie != "" {
		pairs["sp_dc"] = cookie
	}
	if market := strings.ToUpper(strings.TrimSpace(body.Market)); market != "" {
		if len(market) != 2 || market[0] < 'A' || market[0] > 'Z' || market[1] < 'A' || market[1] > 'Z' {
			writeError(w, http.StatusBadRequest, "Market 必须是两位国家代码，例如 US 或 CN")
			return
		}
		pairs["market"] = market
	}
	if len(pairs) == 0 {
		writeError(w, http.StatusBadRequest, "请至少填写一项配置")
		return
	}
	if err := s.core.Config.PersistPluginConfig("spotify", pairs); err != nil {
		writeError(w, http.StatusInternalServerError, "保存 Spotify 配置失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"message":          "Spotify 配置已写入 config.ini；请重启 musicweb 服务后生效。",
		"restart_required": true,
		"settings":         spotifySettingsFromConfig(s.core.Config),
	})
}

func spotifySettingsFromConfig(cfg interface{ GetPluginString(string, string) string }) *adminSpotifySettings {
	if cfg == nil {
		return nil
	}
	return &adminSpotifySettings{
		ClientIDConfigured:     strings.TrimSpace(cfg.GetPluginString("spotify", "client_id")) != "",
		ClientSecretConfigured: strings.TrimSpace(cfg.GetPluginString("spotify", "client_secret")) != "",
		SPDCConfigured:         strings.TrimSpace(cfg.GetPluginString("spotify", "sp_dc")) != "",
		Market:                 strings.ToUpper(strings.TrimSpace(cfg.GetPluginString("spotify", "market"))),
	}
}

// spotifySPDCValue accepts either the cookie value itself or a full copied
// Cookie header, but persists only the sensitive sp_dc value.
func spotifySPDCValue(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "sp_dc=") {
			return strings.TrimSpace(strings.TrimPrefix(part, "sp_dc="))
		}
	}
	return raw
}

func (s *Server) handleAdminCookieImport(w http.ResponseWriter, r *http.Request) {
	plat, _, ok := s.adminPlatformFromPath(w, r, "/cookie")
	if !ok {
		return
	}
	importer, ok := plat.(platform.CookieImporter)
	if !ok {
		writeError(w, http.StatusBadRequest, "该平台不支持 Cookie 导入")
		return
	}
	var body struct {
		Cookie string `json:"cookie"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "JSON 格式错误")
		return
	}
	result, err := importer.ImportCookie(r.Context(), body.Cookie)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) adminPlatformFromPath(w http.ResponseWriter, r *http.Request, suffix string) (platform.Platform, string, bool) {
	platformName := strings.Trim(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/admin/api/platforms/"), suffix), "/")
	if platformName == "" || s.core == nil || s.core.PlatformManager == nil {
		writeError(w, http.StatusBadRequest, "platform required")
		return nil, "", false
	}
	plat := s.core.PlatformManager.Get(platformName)
	if plat == nil {
		writeError(w, http.StatusNotFound, "平台不存在")
		return nil, platformName, false
	}
	return plat, platformName, true
}

func (s *Server) handleAdminCookieCheck(w http.ResponseWriter, r *http.Request) {
	plat, _, ok := s.adminPlatformFromPath(w, r, "/check")
	if !ok {
		return
	}
	checker, ok := plat.(platform.CookieChecker)
	if !ok {
		writeError(w, http.StatusBadRequest, "该平台不支持账号检查")
		return
	}
	result, err := checker.CheckCookie(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleAdminCookieRenew(w http.ResponseWriter, r *http.Request) {
	plat, _, ok := s.adminPlatformFromPath(w, r, "/renew")
	if !ok {
		return
	}
	renewer, ok := plat.(platform.CookieRenewer)
	if !ok {
		writeError(w, http.StatusBadRequest, "该平台不支持手动续期")
		return
	}
	message, err := renewer.ManualRenew(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": message})
}

func (s *Server) handleAdminAutoRenew(w http.ResponseWriter, r *http.Request) {
	plat, _, ok := s.adminPlatformFromPath(w, r, "/auto")
	if !ok {
		return
	}
	auto, ok := plat.(platform.AutoRenewer)
	if !ok {
		writeError(w, http.StatusBadRequest, "该平台不支持自动续期")
		return
	}
	if r.Method == http.MethodGet {
		st, err := auto.GetAutoRenewStatus(r.Context())
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, autoRenewPayload(st))
		return
	}
	var body struct {
		Enabled         bool `json:"enabled"`
		IntervalSeconds int  `json:"interval_seconds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "JSON 格式错误")
		return
	}
	interval := time.Duration(body.IntervalSeconds) * time.Second
	if body.Enabled && interval <= 0 {
		current, err := auto.GetAutoRenewStatus(r.Context())
		if err == nil && current.Interval > 0 {
			interval = current.Interval
		} else {
			interval = 24 * time.Hour
		}
	}
	st, err := auto.SetAutoRenew(r.Context(), body.Enabled, interval)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, autoRenewPayload(st))
}

func (s *Server) handleAdminLanguage(w http.ResponseWriter, r *http.Request) {
	plat, _, ok := s.adminPlatformFromPath(w, r, "/language")
	if !ok {
		return
	}
	provider, ok := plat.(platform.LanguageProvider)
	if !ok {
		writeError(w, http.StatusBadRequest, "该平台不支持语言设置")
		return
	}
	if r.Method == http.MethodGet {
		message, err := provider.ShowLanguage(r.Context())
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"message": message})
		return
	}
	var body struct {
		Language string `json:"language"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "JSON 格式错误")
		return
	}
	if strings.TrimSpace(body.Language) == "" {
		writeError(w, http.StatusBadRequest, "language required")
		return
	}
	message, err := provider.SetLanguage(r.Context(), body.Language)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": message})
}

func (s *Server) handleAdminSignIn(w http.ResponseWriter, r *http.Request) {
	plat, _, ok := s.adminPlatformFromPath(w, r, "/signin")
	if !ok {
		return
	}
	provider, ok := plat.(platform.SignInProvider)
	if !ok {
		writeError(w, http.StatusBadRequest, "该平台不支持签到")
		return
	}
	message, err := provider.SignIn(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": message})
}

func autoRenewPayload(st platform.AutoRenewStatus) *adminAutoRenewStatus {
	seconds := 0
	if st.Interval > 0 {
		seconds = int(st.Interval.Round(time.Second) / time.Second)
	}
	return &adminAutoRenewStatus{Enabled: st.Enabled, IntervalSeconds: seconds}
}

func (s *Server) handleAdminQRStart(w http.ResponseWriter, r *http.Request) {
	platformName := strings.Trim(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/admin/api/platforms/"), "/qr/start"), "/")
	if platformName == "" || s.core == nil || s.core.PlatformManager == nil {
		writeError(w, http.StatusBadRequest, "platform required")
		return
	}
	plat := s.core.PlatformManager.Get(platformName)
	if plat == nil {
		writeError(w, http.StatusNotFound, "平台不存在")
		return
	}
	provider, ok := plat.(platform.QRLoginProvider)
	if !ok {
		writeError(w, http.StatusBadRequest, "该平台不支持 QR 登录")
		return
	}
	session, err := provider.StartQRLogin(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if session == nil {
		writeError(w, http.StatusBadGateway, "QR 登录会话创建失败")
		return
	}
	id := strings.TrimSpace(session.CancelID)
	if id == "" {
		id = randomID("qr_")
	}
	state := &qrSessionState{
		ID:        id,
		Platform:  platformName,
		State:     "waiting",
		Message:   fallback(session.Caption, "请扫码登录"),
		Caption:   session.Caption,
		ImageURL:  session.Image.URL,
		ImageData: qrImageData(session.Image),
		UpdatedAt: time.Now(),
		cancel:    session.Cancel,
	}
	s.qrMu.Lock()
	s.qr[id] = state
	s.qrMu.Unlock()

	if session.Poll != nil {
		timeout := session.Timeout
		if timeout <= 0 {
			timeout = 2 * time.Minute
		}
		go s.pollQRSession(id, timeout, session.Poll)
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) pollQRSession(id string, timeout time.Duration, poll func(context.Context, func(platform.QRLoginUpdate, error))) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	poll(ctx, func(update platform.QRLoginUpdate, err error) {
		s.qrMu.Lock()
		defer s.qrMu.Unlock()
		state := s.qr[id]
		if state == nil {
			return
		}
		state.UpdatedAt = time.Now()
		if err != nil {
			state.State = "error"
			state.Message = err.Error()
			state.Final = true
			if err == context.DeadlineExceeded {
				state.State = "timeout"
				state.Message = "QR 登录超时"
			}
			return
		}
		if strings.TrimSpace(update.State) != "" {
			state.State = update.State
		}
		if strings.TrimSpace(update.Message) != "" {
			state.Message = update.Message
		}
		if strings.TrimSpace(update.Caption) != "" {
			state.Caption = update.Caption
			state.Message = update.Caption
		}
		state.Final = update.Final
	})
}

func (s *Server) handleAdminQRStatus(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/admin/api/qr/"), "/status"), "/")
	s.qrMu.RLock()
	state := s.qr[id]
	if state != nil {
		clone := *state
		clone.cancel = nil
		s.qrMu.RUnlock()
		writeJSON(w, http.StatusOK, clone)
		return
	}
	s.qrMu.RUnlock()
	writeError(w, http.StatusNotFound, "QR 会话不存在")
}

func (s *Server) handleAdminQRCancel(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/admin/api/qr/"), "/cancel"), "/")
	s.qrMu.Lock()
	state := s.qr[id]
	if state == nil {
		s.qrMu.Unlock()
		writeError(w, http.StatusNotFound, "QR 会话不存在")
		return
	}
	cancel := state.cancel
	state.State = "cancelled"
	state.Message = "已取消"
	state.Final = true
	state.UpdatedAt = time.Now()
	s.qrMu.Unlock()
	if cancel != nil {
		cancel()
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func implementsCookieImport(plat platform.Platform) bool {
	_, ok := plat.(platform.CookieImporter)
	return ok
}

func implementsQRLogin(plat platform.Platform) bool {
	_, ok := plat.(platform.QRLoginProvider)
	return ok
}

func implementsCookieCheck(plat platform.Platform) bool {
	_, ok := plat.(platform.CookieChecker)
	return ok
}

func implementsCookieRenew(plat platform.Platform) bool {
	_, ok := plat.(platform.CookieRenewer)
	return ok
}

func implementsAutoRenew(plat platform.Platform) bool {
	_, ok := plat.(platform.AutoRenewer)
	return ok
}

func implementsLanguage(plat platform.Platform) bool {
	_, ok := plat.(platform.LanguageProvider)
	return ok
}

func implementsSignIn(plat platform.Platform) bool {
	_, ok := plat.(platform.SignInProvider)
	return ok
}

func qrImageData(image platform.QRLoginImage) string {
	if len(image.PNG) > 0 {
		return "data:image/png;base64," + base64.StdEncoding.EncodeToString(image.PNG)
	}
	raw := strings.TrimSpace(image.Base64)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "data:image/") {
		return raw
	}
	return "data:image/png;base64," + raw
}

func randomID(prefix string) string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err == nil {
		return prefix + hex.EncodeToString(b[:])
	}
	return prefix + strings.ReplaceAll(time.Now().UTC().Format("20060102150405.000000000"), ".", "")
}

func fallback(value, fallbackValue string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallbackValue
}

const adminLoginHTML = `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>管理员登录</title><style>body{font-family:system-ui;margin:0;background:#f6f7fb}.box{max-width:380px;margin:14vh auto;background:white;padding:28px;border-radius:18px;box-shadow:0 20px 50px #0001}input,button{width:100%;box-sizing:border-box;margin-top:12px;padding:12px;border-radius:12px;border:1px solid #ddd}button{background:#2563eb;color:white;border-color:#2563eb;font-weight:700}</style></head><body><form class="box" method="post"><h2>管理员登录</h2><input name="username" placeholder="用户名" value="admin"><input name="password" type="password" placeholder="密码"><button>登录</button><p>第一版默认读取 WebAdminUsername / WebAdminPasswordHash 或 WebAdminPassword。</p></form></body></html>`

const adminHTML = `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>管理后台</title><style>body{font-family:system-ui;margin:0;background:#f6f7fb;color:#111827}.wrap{max-width:1120px;margin:0 auto;padding:32px}.top{display:flex;justify-content:space-between;align-items:center}.grid{display:grid;gap:18px}.panel{background:white;border-radius:18px;padding:18px;box-shadow:0 10px 30px #0001}.row{border-bottom:1px solid #eee;padding:16px 0}.row:last-child{border:0}.title{font-weight:750;font-size:18px}.badge{display:inline-block;margin-left:8px;padding:3px 8px;border-radius:999px;background:#eef2ff;color:#1d4ed8;font-size:12px}.muted{color:#6b7280}.ops{display:grid;grid-template-columns:1fr auto auto;gap:8px;margin-top:10px}.actions{display:flex;flex-wrap:wrap;gap:8px;margin-top:10px}textarea,input{width:100%;min-width:0;min-height:42px;border-radius:12px;border:1px solid #ddd;padding:10px;box-sizing:border-box}textarea{min-height:74px}button{padding:9px 12px;border-radius:10px;border:1px solid #2563eb;background:#2563eb;color:white;cursor:pointer}button:disabled,textarea:disabled{opacity:.45;cursor:not-allowed}.secondary{background:#eef2ff;color:#1d4ed8;border-color:#c7d2fe}.danger{background:#dc2626;border-color:#dc2626}.qr{margin-top:12px;padding:12px;border-radius:14px;background:#f9fafb;display:none}.qr img{max-width:220px;border-radius:12px;display:block;margin-top:8px}.job{display:grid;grid-template-columns:1fr auto;gap:12px;align-items:center}.pill{padding:3px 8px;border-radius:999px;background:#f3f4f6;font-size:12px}.ready{background:#dcfce7;color:#166534}.failed{background:#fee2e2;color:#991b1b}@media(max-width:760px){.ops,.job{grid-template-columns:1fr}.top{display:block}}</style></head><body><main class="wrap"><div class="top"><h1>管理后台</h1><form method="post" action="/admin/logout"><button class="secondary">退出</button></form></div><div class="grid"><section class="panel"><h2>平台账号状态</h2><div id="statuses">加载中...</div></section><section class="panel"><div class="top"><h2>下载历史</h2><div><button class="secondary" onclick="loadDownloads()">刷新</button> <button class="danger" onclick="cleanupDownloads()">清理过期</button></div></div><div id="downloads">加载中...</div></section></div></main><script>
async function api(url, opts){const r=await fetch(url,opts);const d=await r.json().catch(()=>({}));if(!r.ok)throw new Error(d.error||r.statusText);return d}
function esc(s){return String(s||'').replace(/[&<>\"]/g,function(c){return {'&':'&amp;','<':'&lt;','>':'&gt;','\"':'&quot;'}[c]})}
function fmtTime(s){if(!s)return '';try{return new Date(s).toLocaleString()}catch(e){return s}}
function disabled(ok){return ok?'':'disabled'}
function autoText(st){if(!st.supports_auto_renew)return '自动续期：不支持';const a=st.auto_renew||{};return '自动续期：'+(a.enabled?'已开启':'已关闭')+(a.interval_seconds?' / '+a.interval_seconds+' 秒':'')}
async function loadPlatforms(){const d=await api('/admin/api/platforms/status');const box=document.getElementById('statuses');box.innerHTML='';for(const st of d.statuses||[]){const row=document.createElement('div');row.className='row';const name=st.display_name||st.platform;const login=st.logged_in?'已登录':'未登录/未知';const cookieDisabled=disabled(st.supports_cookie);const qrDisabled=disabled(st.supports_qr);const checkDisabled=disabled(st.supports_check);const renewDisabled=disabled(st.supports_renew);const autoDisabled=disabled(st.supports_auto_renew);const langDisabled=disabled(st.supports_language);const signDisabled=disabled(st.supports_sign_in);const autoLabel=(st.auto_renew&&st.auto_renew.enabled)?'关闭自动续期':'开启自动续期';row.innerHTML='<div class="title">'+esc(name)+'<span class="badge">'+esc(st.platform)+'</span></div><p class="muted">'+esc(login)+(st.nickname?' · '+esc(st.nickname):'')+(st.summary?' · '+esc(st.summary):'')+'</p><p class="muted">'+esc(autoText(st))+(st.expires_at?' · 过期：'+fmtTime(st.expires_at):'')+'</p><div class="actions"><button class="check secondary" '+checkDisabled+'>检查账号</button><button class="renew secondary" '+renewDisabled+'>手动续期</button><button class="auto secondary" '+autoDisabled+'>'+autoLabel+'</button><button class="lang secondary" '+langDisabled+'>语言设置</button><button class="signin secondary" '+signDisabled+'>签到</button></div><div class="ops"><textarea placeholder="粘贴 Cookie 后点击导入" '+cookieDisabled+'></textarea><button class="cookie" '+cookieDisabled+'>导入 Cookie</button><button class="qrbtn secondary" '+qrDisabled+'>QR 登录</button></div><div class="qr"><div class="qrmsg muted"></div><img class="qrimg"></div>';row.querySelector('.cookie').onclick=async()=>{try{await api('/admin/api/platforms/'+encodeURIComponent(st.platform)+'/cookie',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({cookie:row.querySelector('textarea').value})});alert('已提交');loadPlatforms()}catch(e){alert(e.message)}};row.querySelector('.qrbtn').onclick=()=>startQR(st.platform,row);row.querySelector('.check').onclick=()=>checkPlatform(st.platform);row.querySelector('.renew').onclick=()=>renewPlatform(st.platform);row.querySelector('.auto').onclick=()=>toggleAuto(st);row.querySelector('.lang').onclick=()=>languagePlatform(st.platform);row.querySelector('.signin').onclick=()=>signInPlatform(st.platform);if(st.platform==='spotify')addSpotifySettings(row,st);box.appendChild(row)}}
function addSpotifySettings(row,st){const cfg=st.spotify_settings||{};const box=document.createElement('div');box.style.cssText='margin-top:12px;padding:12px;border:1px solid #dbeafe;border-radius:12px;background:#f8fbff';box.innerHTML='<div class="title" style="font-size:14px">Spotify Web API / 下载配置</div><p class="muted">Client Credentials 用于搜索和元数据；sp_dc 用于网页播放器歌词和原生下载。已保存的密钥不会回显。</p><div class="ops"><input class="spid" placeholder="Client ID '+(cfg.client_id_configured?'（已配置，留空不覆盖）':'')+'"><input class="spsecret" type="password" placeholder="Client Secret '+(cfg.client_secret_configured?'（已配置，留空不覆盖）':'')+'"><input class="spdc" type="password" placeholder="sp_dc '+(cfg.sp_dc_configured?'（已配置，留空不覆盖）':'')+'"></div><div class="actions"><input class="spmarket" value="'+esc(cfg.market||'US')+'" maxlength="2" style="width:78px;padding:9px;border:1px solid #c7d2fe;border-radius:10px" title="市场区域，例如 US"><button class="spsave secondary">保存 Spotify 配置</button></div>';box.querySelector('.spsave').onclick=async()=>{const button=box.querySelector('.spsave');button.disabled=true;try{const d=await api('/admin/api/platforms/spotify/settings',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({client_id:box.querySelector('.spid').value,client_secret:box.querySelector('.spsecret').value,sp_dc:box.querySelector('.spdc').value,market:box.querySelector('.spmarket').value})});alert(d.message||'已保存');loadPlatforms()}catch(e){alert(e.message)}finally{button.disabled=false}};row.appendChild(box)}
async function checkPlatform(platform){try{const d=await api('/admin/api/platforms/'+encodeURIComponent(platform)+'/check',{method:'POST'});alert((d.ok?'检查通过：':'检查失败：')+(d.message||''));loadPlatforms()}catch(e){alert(e.message)}}
async function renewPlatform(platform){try{const d=await api('/admin/api/platforms/'+encodeURIComponent(platform)+'/renew',{method:'POST'});alert(d.message||'续期完成');loadPlatforms()}catch(e){alert(e.message)}}
async function toggleAuto(st){const current=st.auto_renew||{};const next=!current.enabled;let seconds=current.interval_seconds||86400;if(next){const input=prompt('自动续期间隔秒数', String(seconds));if(input===null)return;seconds=parseInt(input,10)||0;if(seconds<=0){alert('间隔必须大于 0 秒');return}}try{const d=await api('/admin/api/platforms/'+encodeURIComponent(st.platform)+'/auto',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({enabled:next,interval_seconds:seconds})});alert('自动续期已'+(d.enabled?'开启':'关闭')+(d.interval_seconds?'，间隔 '+d.interval_seconds+' 秒':''));loadPlatforms()}catch(e){alert(e.message)}}
async function languagePlatform(platform){try{const cur=await api('/admin/api/platforms/'+encodeURIComponent(platform)+'/language');const lang=prompt((cur.message||'请输入语言代码')+'\n\n留空只查看，不修改：','');if(lang===null||lang.trim()===''){alert(cur.message||'已查看');return}const d=await api('/admin/api/platforms/'+encodeURIComponent(platform)+'/language',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({language:lang.trim()})});alert(d.message||'语言已更新');loadPlatforms()}catch(e){alert(e.message)}}
async function signInPlatform(platform){try{const d=await api('/admin/api/platforms/'+encodeURIComponent(platform)+'/signin',{method:'POST'});alert(d.message||'签到完成')}catch(e){alert(e.message)}}
async function startQR(platform,row){const panel=row.querySelector('.qr');const msg=row.querySelector('.qrmsg');const img=row.querySelector('.qrimg');panel.style.display='block';msg.textContent='正在创建 QR 会话...';img.removeAttribute('src');try{const s=await api('/admin/api/platforms/'+encodeURIComponent(platform)+'/qr/start',{method:'POST'});if(s.image_data||s.image_url){img.src=s.image_data||s.image_url}msg.textContent=s.message||s.caption||'请扫码登录';pollQR(s.id,row)}catch(e){msg.textContent=e.message}}
async function pollQR(id,row){const msg=row.querySelector('.qrmsg');try{const s=await api('/admin/api/qr/'+encodeURIComponent(id)+'/status');msg.textContent=(s.state?s.state+'：':'')+(s.message||'等待扫码');if(s.final){setTimeout(loadPlatforms,800);return}setTimeout(()=>pollQR(id,row),1500)}catch(e){msg.textContent=e.message}}
async function loadDownloads(){const d=await api('/admin/api/downloads?limit=80');const box=document.getElementById('downloads');box.innerHTML='';const jobs=d.jobs||[];if(!jobs.length){box.innerHTML='<p class="muted">暂无下载任务。</p>';return}for(const j of jobs){const row=document.createElement('div');row.className='row job';const cls=j.status==='ready'?'ready':(j.status==='failed'?'failed':'');let action='';if(j.status==='ready') action='<a href="/api/downloads/'+encodeURIComponent(j.job_id)+'/file">下载</a>';row.innerHTML='<div><div class="title">'+esc(j.title||j.track_id)+' <span class="badge">'+esc(j.platform)+'</span> <span class="pill '+cls+'">'+esc(j.status)+' '+(j.progress||0)+'%</span></div><p class="muted">'+esc((j.artists||[]).join(" / "))+' · '+esc(j.quality||'')+' · '+esc(j.file_name||'')+'</p><p class="muted">创建：'+fmtTime(j.created_at)+'；过期：'+fmtTime(j.expires_at)+(j.error?'；错误：'+esc(j.error):'')+'</p></div><div>'+action+'</div>';box.appendChild(row)}}
async function cleanupDownloads(){try{const d=await api('/admin/api/downloads/cleanup',{method:'POST'});alert('清理完成：文件 '+d.files_removed+' 个，任务 '+d.jobs_removed+' 个');loadDownloads()}catch(e){alert(e.message)}}
loadPlatforms().catch(e=>document.getElementById('statuses').textContent=e.message);loadDownloads().catch(e=>document.getElementById('downloads').textContent=e.message)
</script></body></html>`
