package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/db"
)

type adminShortcutKey struct {
	KeyID      string     `json:"key_id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	UsageLimit int64      `json:"usage_limit"`
	Used       int64      `json:"used"`
	Remaining  int64      `json:"remaining,omitempty"`
	Unlimited  bool       `json:"unlimited"`
	Enabled    bool       `json:"enabled"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

func (s *Server) handleAdminShortcutKeys(w http.ResponseWriter, r *http.Request) {
	if s.core == nil || s.core.DB == nil {
		writeError(w, http.StatusServiceUnavailable, "数据库未配置")
		return
	}
	if r.Method == http.MethodGet {
		values, err := s.core.DB.ListShortcutAPIKeys(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		items := make([]adminShortcutKey, 0, len(values))
		for i := range values {
			items = append(items, shortcutKeyForAdmin(&values[i]))
		}
		writeJSON(w, http.StatusOK, map[string]any{"keys": items, "default_limit": 100})
		return
	}

	var body struct {
		Name      string `json:"name"`
		Limit     *int64 `json:"limit"`
		Unlimited bool   `json:"unlimited"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "JSON 格式错误")
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = "iOS 快捷指令"
	}
	if len([]rune(name)) > 80 {
		writeError(w, http.StatusBadRequest, "名称不能超过 80 个字符")
		return
	}
	limit := int64(100)
	if body.Limit != nil {
		limit = *body.Limit
	}
	if body.Unlimited {
		limit = 0
	}
	if limit < 0 || limit > 1000000000 {
		writeError(w, http.StatusBadRequest, "解析次数必须在 1 到 1000000000 之间，或选择无限")
		return
	}
	keyID, token, prefix, hash, err := generateShortcutAPIKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成 API Key 失败")
		return
	}
	model := &db.ShortcutAPIKeyModel{KeyID: keyID, Name: name, Prefix: prefix, SecretHash: hash, UsageLimit: limit, Enabled: true}
	if err := s.core.DB.CreateShortcutAPIKey(r.Context(), model); err != nil {
		writeError(w, http.StatusInternalServerError, "保存 API Key 失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"api_key": token,
		"key":     shortcutKeyForAdmin(model),
		"message": "API Key 仅显示这一次，请立即复制到快捷指令。",
	})
}

func (s *Server) handleAdminShortcutKey(w http.ResponseWriter, r *http.Request) {
	if s.core == nil || s.core.DB == nil {
		writeError(w, http.StatusServiceUnavailable, "数据库未配置")
		return
	}
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/admin/api/shortcut-keys/"), "/")
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	keyID := parts[0]
	if len(parts) == 2 && parts[1] == "reset" && r.Method == http.MethodPost {
		model, err := s.core.DB.ResetShortcutAPIKeyUsage(r.Context(), keyID)
		writeAdminShortcutKeyResult(w, model, err)
		return
	}
	if len(parts) != 1 {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodPatch:
		current, err := s.core.DB.FindShortcutAPIKey(r.Context(), keyID)
		if err != nil {
			writeAdminShortcutKeyResult(w, nil, err)
			return
		}
		var body struct {
			Name      *string `json:"name"`
			Limit     *int64  `json:"limit"`
			Unlimited *bool   `json:"unlimited"`
			Enabled   *bool   `json:"enabled"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			writeError(w, http.StatusBadRequest, "JSON 格式错误")
			return
		}
		name, limit, enabled := current.Name, current.UsageLimit, current.Enabled
		if body.Name != nil {
			name = strings.TrimSpace(*body.Name)
		}
		if body.Limit != nil {
			limit = *body.Limit
		}
		if body.Unlimited != nil && *body.Unlimited {
			limit = 0
		}
		if body.Enabled != nil {
			enabled = *body.Enabled
		}
		if name == "" || len([]rune(name)) > 80 || limit < 0 || limit > 1000000000 {
			writeError(w, http.StatusBadRequest, "名称或解析次数无效")
			return
		}
		model, err := s.core.DB.UpdateShortcutAPIKey(r.Context(), keyID, name, limit, enabled)
		writeAdminShortcutKeyResult(w, model, err)
	case http.MethodDelete:
		if err := s.core.DB.DeleteShortcutAPIKey(r.Context(), keyID); err != nil {
			writeAdminShortcutKeyResult(w, nil, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		methodNotAllowed(w)
	}
}

func writeAdminShortcutKeyResult(w http.ResponseWriter, model *db.ShortcutAPIKeyModel, err error) {
	if errors.Is(err, db.ErrShortcutAPIKeyNotFound) {
		writeError(w, http.StatusNotFound, "API Key 不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": shortcutKeyForAdmin(model)})
}

func shortcutKeyForAdmin(model *db.ShortcutAPIKeyModel) adminShortcutKey {
	if model == nil {
		return adminShortcutKey{}
	}
	remaining := int64(0)
	if model.UsageLimit > 0 && model.Used < model.UsageLimit {
		remaining = model.UsageLimit - model.Used
	}
	return adminShortcutKey{
		KeyID: model.KeyID, Name: model.Name, Prefix: model.Prefix, UsageLimit: model.UsageLimit,
		Used: model.Used, Remaining: remaining, Unlimited: model.UsageLimit == 0, Enabled: model.Enabled,
		CreatedAt: model.CreatedAt, LastUsedAt: model.LastUsedAt,
	}
}

func generateShortcutAPIKey() (keyID, token, prefix, hash string, err error) {
	idBytes := make([]byte, 6)
	secretBytes := make([]byte, 32)
	if _, err = rand.Read(idBytes); err != nil {
		return
	}
	if _, err = rand.Read(secretBytes); err != nil {
		return
	}
	keyID = hex.EncodeToString(idBytes)
	token = fmt.Sprintf("mwsk_%s_%s", keyID, hex.EncodeToString(secretBytes))
	prefix = token[:min(len(token), 22)] + "…"
	sum := sha256.Sum256([]byte(token))
	hash = hex.EncodeToString(sum[:])
	return
}
