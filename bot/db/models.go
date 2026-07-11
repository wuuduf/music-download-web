package db

import (
	"time"

	"github.com/liuran001/MusicBot-Go/bot"
	"gorm.io/gorm"
)

// SongInfoModel mirrors the song_infos schema with multi-platform support.
type SongInfoModel struct {
	gorm.Model
	Platform        string `gorm:"not null;default:'netease';index:idx_platform_track_quality,unique"`
	TrackID         string `gorm:"not null;default:'';index:idx_platform_track_quality,unique"`
	Quality         string `gorm:"not null;default:'hires';index:idx_platform_track_quality,unique"`
	QualityVerified bool   `gorm:"not null;default:false"`
	MusicID         int    // Deprecated: Legacy NetEase music ID (kept for backward compatibility)
	SongName        string
	SongArtists     string
	SongArtistsIDs  string
	SongAlbum       string
	AlbumID         int
	TrackURL        string
	AlbumURL        string
	SongArtistsURLs string
	FileExt         string
	MusicSize       int
	PicSize         int
	EmbPicSize      int
	BitRate         int
	Duration        int
	FileID          string
	ThumbFileID     string
	FromUserID      int64
	FromUserName    string
	FromChatID      int64
	FromChatName    string
	LyricsAvailable *bool
}

func (SongInfoModel) TableName() string {
	return "song_infos"
}

// BotStatModel stores aggregated bot statistics.
type BotStatModel struct {
	gorm.Model
	Key   string `gorm:"uniqueIndex;not null"`
	Value int64
}

func (BotStatModel) TableName() string {
	return "bot_stats"
}

// WebDownloadJobModel stores website download jobs and local file cache
// metadata. It intentionally lives in data.db (not cache.db) so browser clients
// can resume status/file links after a process restart.
type WebDownloadJobModel struct {
	gorm.Model
	JobID     string `gorm:"uniqueIndex;not null"`
	CacheKey  string `gorm:"index;not null"`
	Platform  string `gorm:"index;not null"`
	TrackID   string `gorm:"index;not null"`
	Quality   string `gorm:"index;not null"`
	Status    string `gorm:"index;not null"`
	Progress  int    `gorm:"not null;default:0"`
	Error     string `gorm:"type:text"`
	Title     string
	Artists   string `gorm:"type:text"`
	Album     string
	FilePath  string `gorm:"type:text"`
	FileName  string
	FileSize  int64
	Format    string
	Bitrate   int
	ExpiresAt time.Time `gorm:"index"`
}

func (WebDownloadJobModel) TableName() string {
	return "web_download_jobs"
}

// StudioProjectModel is the durable root record for an AMLL TTML editing
// project. Large lyric revisions are stored separately so restoring history
// never rewrites the project row.
type StudioProjectModel struct {
	gorm.Model
	ProjectID       string `gorm:"uniqueIndex;not null"`
	Platform        string `gorm:"index;not null"`
	TrackID         string `gorm:"index;not null"`
	Quality         string
	PlaybackSession string `gorm:"index"`
	MetadataJSON    string `gorm:"type:text"`
	CurrentRevision int    `gorm:"not null;default:1"`
	Status          string `gorm:"index;not null;default:'active'"`
}

func (StudioProjectModel) TableName() string { return "studio_projects" }

// StudioRevisionModel contains immutable revision snapshots. ProjectID and
// Revision form a unique key used for optimistic conflict detection.
type StudioRevisionModel struct {
	gorm.Model
	ProjectID    string `gorm:"uniqueIndex:idx_studio_project_revision;not null"`
	Revision     int    `gorm:"uniqueIndex:idx_studio_project_revision;not null"`
	Content      string `gorm:"type:text;not null"`
	MetadataJSON string `gorm:"type:text"`
}

func (StudioRevisionModel) TableName() string { return "studio_revisions" }

// ShortcutAPIKeyModel stores hashed API credentials and their lifetime parse
// quota. UsageLimit=0 means unlimited. The plaintext secret is returned only
// once when an administrator creates the key and is never stored.
type ShortcutAPIKeyModel struct {
	gorm.Model
	KeyID      string     `gorm:"uniqueIndex;not null" json:"key_id"`
	Name       string     `gorm:"not null" json:"name"`
	SecretHash string     `gorm:"uniqueIndex;not null" json:"-"`
	Prefix     string     `gorm:"not null" json:"prefix"`
	UsageLimit int64      `gorm:"not null;default:100" json:"usage_limit"`
	Used       int64      `gorm:"not null;default:0" json:"used"`
	Enabled    bool       `gorm:"not null;default:true" json:"enabled"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

func (ShortcutAPIKeyModel) TableName() string { return "shortcut_api_keys" }

func toInternal(model SongInfoModel) *bot.SongInfo {
	return &bot.SongInfo{
		ID:              model.ID,
		CreatedAt:       model.CreatedAt,
		UpdatedAt:       model.UpdatedAt,
		DeletedAt:       deletedAtPtr(model.DeletedAt),
		Platform:        model.Platform,
		TrackID:         model.TrackID,
		Quality:         model.Quality,
		QualityVerified: model.QualityVerified,
		MusicID:         model.MusicID,
		SongName:        model.SongName,
		SongArtists:     model.SongArtists,
		SongArtistsIDs:  model.SongArtistsIDs,
		SongAlbum:       model.SongAlbum,
		AlbumID:         model.AlbumID,
		TrackURL:        model.TrackURL,
		AlbumURL:        model.AlbumURL,
		SongArtistsURLs: model.SongArtistsURLs,
		FileExt:         model.FileExt,
		MusicSize:       model.MusicSize,
		PicSize:         model.PicSize,
		EmbPicSize:      model.EmbPicSize,
		BitRate:         model.BitRate,
		Duration:        model.Duration,
		FileID:          model.FileID,
		ThumbFileID:     model.ThumbFileID,
		FromUserID:      model.FromUserID,
		FromUserName:    model.FromUserName,
		FromChatID:      model.FromChatID,
		FromChatName:    model.FromChatName,
		LyricsAvailable: model.LyricsAvailable,
	}
}

func toModel(info *bot.SongInfo) *SongInfoModel {
	if info == nil {
		return &SongInfoModel{}
	}

	model := &SongInfoModel{
		Platform:        info.Platform,
		TrackID:         info.TrackID,
		Quality:         info.Quality,
		QualityVerified: info.QualityVerified,
		MusicID:         info.MusicID,
		SongName:        info.SongName,
		SongArtists:     info.SongArtists,
		SongArtistsIDs:  info.SongArtistsIDs,
		SongAlbum:       info.SongAlbum,
		AlbumID:         info.AlbumID,
		TrackURL:        info.TrackURL,
		AlbumURL:        info.AlbumURL,
		SongArtistsURLs: info.SongArtistsURLs,
		FileExt:         info.FileExt,
		MusicSize:       info.MusicSize,
		PicSize:         info.PicSize,
		EmbPicSize:      info.EmbPicSize,
		BitRate:         info.BitRate,
		Duration:        info.Duration,
		FileID:          info.FileID,
		ThumbFileID:     info.ThumbFileID,
		FromUserID:      info.FromUserID,
		FromUserName:    info.FromUserName,
		FromChatID:      info.FromChatID,
		FromChatName:    info.FromChatName,
		LyricsAvailable: info.LyricsAvailable,
	}

	if info.ID != 0 {
		model.ID = info.ID
	}
	if !info.CreatedAt.IsZero() {
		model.CreatedAt = info.CreatedAt
	}
	if !info.UpdatedAt.IsZero() {
		model.UpdatedAt = info.UpdatedAt
	}
	if info.DeletedAt != nil {
		model.DeletedAt = gorm.DeletedAt{Time: *info.DeletedAt, Valid: true}
	}

	return model
}

func deletedAtPtr(value gorm.DeletedAt) *time.Time {
	if value.Valid {
		return &value.Time
	}
	return nil
}

// UserSettingsModel stores user preferences for the bot.
type UserSettingsModel struct {
	gorm.Model
	UserID             int64  `gorm:"uniqueIndex;not null"`
	DefaultPlatform    string `gorm:"not null;default:'netease'"`
	DefaultQuality     string `gorm:"not null;default:'hires'"`
	AutoDeleteList     bool   `gorm:"not null;default:false"`
	AutoLinkDetect     bool   `gorm:"not null;default:true"`
	DefaultLyricFormat string `gorm:"not null;default:'lrc'"`
	// Language is the persisted UI-language override (2-letter ISO 639-1, e.g.
	// "zh"/"en"/"ja"). Empty means "auto-detect from the Telegram client".
	Language string `gorm:"not null;default:''"`
	// Nullable side-track defaults: NULL means "unset" (use the per-format
	// default); a non-NULL value is the user's explicit choice.
	DefaultLyricIncludeTranslation *bool
	DefaultLyricIncludeRoma        *bool
}

func (UserSettingsModel) TableName() string {
	return "user_settings"
}

// GroupSettingsModel stores group preferences for the bot.
type GroupSettingsModel struct {
	gorm.Model
	ChatID             int64  `gorm:"uniqueIndex;not null"`
	DefaultPlatform    string `gorm:"not null;default:'netease'"`
	DefaultQuality     string `gorm:"not null;default:'hires'"`
	AutoDeleteList     bool   `gorm:"not null;default:true"`
	AutoLinkDetect     bool   `gorm:"not null;default:true"`
	DefaultLyricFormat string `gorm:"not null;default:'lrc'"`
	// Language is the persisted UI-language override (2-letter ISO 639-1, e.g.
	// "zh"/"en"/"ja"). Empty means "auto-detect from the Telegram client".
	Language string `gorm:"not null;default:''"`
	// Nullable side-track defaults: NULL means "unset" (use the per-format
	// default); a non-NULL value is the group's explicit choice.
	DefaultLyricIncludeTranslation *bool
	DefaultLyricIncludeRoma        *bool
}

func (GroupSettingsModel) TableName() string {
	return "group_settings"
}

type PluginSettingModel struct {
	gorm.Model
	ScopeType    string `gorm:"uniqueIndex:idx_plugin_scope_key,priority:1;not null"`
	ScopeID      int64  `gorm:"uniqueIndex:idx_plugin_scope_key,priority:2;not null"`
	Plugin       string `gorm:"uniqueIndex:idx_plugin_scope_key,priority:3;not null"`
	SettingKey   string `gorm:"uniqueIndex:idx_plugin_scope_key,priority:4;not null"`
	SettingValue string `gorm:"type:text;not null"`
}

func (PluginSettingModel) TableName() string {
	return "plugin_settings"
}

// FavoriteModel stores a favorited track for a user or a group. The four-column
// unique index guarantees a track is favorited at most once per scope; a second
// "add" is an idempotent upsert. It lives in data.db (durable user data), never
// in the volatile song cache, so song metadata is denormalized here.
type FavoriteModel struct {
	gorm.Model
	ScopeType       string `gorm:"uniqueIndex:idx_fav_scope_track,priority:1;not null"`
	ScopeID         int64  `gorm:"uniqueIndex:idx_fav_scope_track,priority:2;not null"`
	Platform        string `gorm:"uniqueIndex:idx_fav_scope_track,priority:3;not null"`
	TrackID         string `gorm:"uniqueIndex:idx_fav_scope_track,priority:4;not null"`
	AddedByUserID   int64  `gorm:"index"`
	AddedByName     string
	SongName        string
	SongArtists     string
	SongAlbum       string
	TrackURL        string
	SongArtistsURLs string
}

func (FavoriteModel) TableName() string {
	return "favorites"
}

func toFavorite(model FavoriteModel) *bot.Favorite {
	return &bot.Favorite{
		ID:              model.ID,
		CreatedAt:       model.CreatedAt,
		UpdatedAt:       model.UpdatedAt,
		DeletedAt:       deletedAtPtr(model.DeletedAt),
		ScopeType:       model.ScopeType,
		ScopeID:         model.ScopeID,
		Platform:        model.Platform,
		TrackID:         model.TrackID,
		AddedByUserID:   model.AddedByUserID,
		AddedByName:     model.AddedByName,
		SongName:        model.SongName,
		SongArtists:     model.SongArtists,
		SongAlbum:       model.SongAlbum,
		TrackURL:        model.TrackURL,
		SongArtistsURLs: model.SongArtistsURLs,
	}
}

func toFavoriteModel(fav *bot.Favorite) *FavoriteModel {
	if fav == nil {
		return &FavoriteModel{}
	}
	model := &FavoriteModel{
		ScopeType:       fav.ScopeType,
		ScopeID:         fav.ScopeID,
		Platform:        fav.Platform,
		TrackID:         fav.TrackID,
		AddedByUserID:   fav.AddedByUserID,
		AddedByName:     fav.AddedByName,
		SongName:        fav.SongName,
		SongArtists:     fav.SongArtists,
		SongAlbum:       fav.SongAlbum,
		TrackURL:        fav.TrackURL,
		SongArtistsURLs: fav.SongArtistsURLs,
	}
	if fav.ID != 0 {
		model.ID = fav.ID
	}
	return model
}
