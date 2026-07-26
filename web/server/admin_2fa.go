package server

import (
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func (s *Server) adminUsername() string {
	if s.core != nil && s.core.Config != nil {
		if u := strings.TrimSpace(s.core.Config.GetString("WebAdminUsername")); u != "" {
			return u
		}
	}
	return "admin"
}

func (s *Server) twoFASecret() string {
	if s.core == nil || s.core.Config == nil {
		return ""
	}
	return strings.TrimSpace(s.core.Config.GetString("WebAdmin2FASecret"))
}

// twoFAEnabled reports whether admin login requires a TOTP code.
func (s *Server) twoFAEnabled() bool {
	return s.core != nil && s.core.Config != nil &&
		s.core.Config.GetBool("WebAdmin2FAEnabled") && s.twoFASecret() != ""
}

func (s *Server) handleAdmin2FAStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": s.twoFAEnabled()})
}

// handleAdmin2FASetup mints a fresh secret + QR for binding. It is NOT persisted
// yet; the client echoes the secret back to /enable together with a valid code,
// so a mistyped scan never locks the operator out.
func (s *Server) handleAdmin2FASetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if s.core == nil || s.core.Config == nil {
		writeError(w, http.StatusInternalServerError, "配置未加载")
		return
	}
	secret, err := generateTOTPSecret()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成密钥失败")
		return
	}
	uri := totpURI(secret, "MusicWeb", s.adminUsername())
	qr, err := totpQRDataURI(uri)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成二维码失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"secret": secret, "uri": uri, "qr": qr})
}

func (s *Server) handleAdmin2FAEnable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if s.core == nil || s.core.Config == nil {
		writeError(w, http.StatusInternalServerError, "配置未加载")
		return
	}
	var body struct {
		Secret string `json:"secret"`
		Code   string `json:"code"`
	}
	if !decodeJSON(w, r, 16<<10, &body) {
		return
	}
	secret := strings.TrimSpace(body.Secret)
	if secret == "" || !verifyTOTP(secret, body.Code) {
		writeError(w, http.StatusBadRequest, "验证码不正确，请重新扫码并输入验证器上的 6 位动态码")
		return
	}
	if err := s.core.Config.PersistAdminConfig(map[string]string{
		"WebAdmin2FASecret":  secret,
		"WebAdmin2FAEnabled": "true",
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "保存 2FA 配置失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "enabled": true})
}

func (s *Server) handleAdmin2FADisable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if s.core == nil || s.core.Config == nil {
		writeError(w, http.StatusInternalServerError, "配置未加载")
		return
	}
	if !s.twoFAEnabled() {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "enabled": false})
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, 16<<10, &body) {
		return
	}
	if !verifyTOTP(s.twoFASecret(), body.Code) {
		writeError(w, http.StatusBadRequest, "动态码不正确")
		return
	}
	if err := s.core.Config.PersistAdminConfig(map[string]string{
		"WebAdmin2FAEnabled": "false",
		"WebAdmin2FASecret":  "",
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "保存失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "enabled": false})
}

// handleAdminChangePassword requires 2FA to be enabled (user requirement), plus
// the current password and a valid TOTP code, before writing the new bcrypt
// hash to config.ini.
func (s *Server) handleAdminChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if s.core == nil || s.core.Config == nil {
		writeError(w, http.StatusInternalServerError, "配置未加载")
		return
	}
	if !s.twoFAEnabled() {
		writeError(w, http.StatusPreconditionRequired, "修改密码前必须先开启二次验证（2FA）")
		return
	}
	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
		Code            string `json:"code"`
	}
	if !decodeJSON(w, r, 16<<10, &body) {
		return
	}
	if !s.verifyAdminPassword(s.adminUsername(), body.CurrentPassword) {
		writeError(w, http.StatusUnauthorized, "当前密码不正确")
		return
	}
	if !verifyTOTP(s.twoFASecret(), body.Code) {
		writeError(w, http.StatusUnauthorized, "二次验证动态码不正确")
		return
	}
	if len([]rune(strings.TrimSpace(body.NewPassword))) < 8 {
		writeError(w, http.StatusBadRequest, "新密码至少 8 位")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(body.NewPassword), 12)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成密码哈希失败")
		return
	}
	if err := s.core.Config.PersistAdminConfig(map[string]string{
		"WebAdminPasswordHash": string(hash),
		"WebAdminPassword":     "", // drop any legacy plaintext
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "保存新密码失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "密码已修改，请用新密码重新登录"})
}
