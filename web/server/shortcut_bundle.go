package server

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/liuran001/MusicBot-Go/bot/musicservice"
)

// handleShortcutBundle performs the whole Shortcut workflow in one HTTP
// request and returns a ZIP. Keeping polling and nested JSON parsing on the
// server avoids fragile Magic Variable behavior across iOS releases.
func (s *Server) handleShortcutBundle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	principal, ok := s.authenticateShortcutAPIKey(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	req, err := decodeShortcutResolveRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input := firstNonEmpty(req.Input, req.URL)
	if input == "" {
		writeError(w, http.StatusBadRequest, "input 或 url 必填")
		return
	}
	quality := strings.ToLower(strings.TrimSpace(req.Quality))
	if quality == "" {
		quality = "hires"
	}
	switch quality {
	case "standard", "high", "lossless", "hires":
	default:
		writeError(w, http.StatusBadRequest, "quality 参数无效")
		return
	}
	files := req.Files
	if files == 0 {
		files = 3
	}
	if files < 1 || files > 3 {
		writeError(w, http.StatusBadRequest, "files 必须是 1、2 或 3")
		return
	}

	track, err := s.music.ParseLink(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if _, ok = s.consumeShortcutParse(w, r, principal); !ok {
		return
	}
	job, err := s.music.CreateDownload(r.Context(), musicservice.DownloadRequest{Platform: track.Platform, TrackID: track.TrackID, Quality: quality})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	timeout := 10 * time.Minute
	if s.core != nil && s.core.Config != nil {
		if seconds := s.core.Config.GetInt("WebShortcutBundleTimeoutSeconds"); seconds > 0 {
			timeout = time.Duration(seconds) * time.Second
		}
	}
	job = s.waitShortcutDownload(r, job, timeout)
	if job == nil || job.Status != "ready" {
		if job != nil && job.Status == "failed" {
			writeError(w, http.StatusBadGateway, firstNonEmpty(job.Error, "音乐下载失败"))
			return
		}
		writeError(w, http.StatusGatewayTimeout, "等待音乐下载超时")
		return
	}

	plan, err := s.prepareShortcutBundle(r.Context(), track, job, files)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer plan.Audio.Close()
	w.Header().Set("Content-Type", "application/zip")
	// Shortcuts on iOS may ignore a header containing only filename*=UTF-8 and
	// consequently treats the response as anonymous data instead of a ZIP.
	// Keep an ASCII filename fallback while retaining the platform-aware name.
	archiveName := "JellyMusicDL-" + safeShortcutName(shortcutPlatformDisplayName(track.Platform)) + ".zip"
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": archiveName})
	if !strings.Contains(disposition, "filename=") {
		disposition = "attachment; filename=\"JellyMusicDL.zip\"; " + strings.TrimPrefix(disposition, "attachment; ")
	}
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	archive := zip.NewWriter(w)
	if err := writeShortcutZipEntry(archive, plan.Directory, plan.MusicName, plan.Audio); err == nil && len(plan.Lyrics) > 0 {
		err = writeShortcutZipEntry(archive, plan.Directory, plan.LyricsName, bytes.NewReader(plan.Lyrics))
	}
	if err == nil && len(plan.Cover) > 0 {
		err = writeShortcutZipEntry(archive, plan.Directory, plan.CoverName, bytes.NewReader(plan.Cover))
	}
	_ = archive.Close()
}

type shortcutBundlePlan struct {
	Directory  string
	Audio      *os.File
	MusicName  string
	Lyrics     []byte
	LyricsName string
	Cover      []byte
	CoverName  string
}

func (s *Server) prepareShortcutBundle(ctx context.Context, track *musicservice.SearchResult, job *musicservice.DownloadJob, files int) (*shortcutBundlePlan, error) {
	if track == nil || job == nil {
		return nil, errors.New("歌曲或下载任务为空")
	}
	audio, err := os.Open(job.FilePath)
	if err != nil {
		return nil, fmt.Errorf("打开音乐文件: %w", err)
	}
	artist := shortcutPrimaryArtist(track.Artists)
	directory := safeShortcutName(shortcutPlatformDisplayName(track.Platform))
	musicStem := shortcutFileStem(artist, track.Title, track.Platform)
	plan := &shortcutBundlePlan{Directory: directory, Audio: audio, MusicName: musicStem + shortcutAudioExtension(job)}
	if files >= 2 {
		doc, lyricErr := s.music.CreateLyrics(ctx, musicservice.LyricsRequest{Platform: job.Platform, TrackID: job.TrackID, Format: "lrc", IncludeTranslation: true, IncludeRoma: true})
		if lyricErr != nil {
			audio.Close()
			return nil, fmt.Errorf("生成歌词: %w", lyricErr)
		}
		plan.Lyrics, plan.LyricsName = doc.Content, musicStem+".lrc"
	}
	if files >= 3 && strings.TrimSpace(track.CoverURL) != "" {
		cover, coverErr := fetchShortcutCover(ctx, bestShortcutCoverURL(track.CoverURL))
		if coverErr != nil {
			audio.Close()
			return nil, fmt.Errorf("下载专辑封面: %w", coverErr)
		}
		plan.Cover = cover
		plan.CoverName = shortcutFileStem(artist, firstNonEmpty(track.Album, track.Title), track.Platform) + shortcutCoverExtension(track.CoverURL)
	}
	return plan, nil
}

func writeShortcutZipEntry(writer *zip.Writer, directory, name string, reader io.Reader) error {
	entry, err := writer.Create(filepath.ToSlash(filepath.Join(directory, name)))
	if err != nil {
		return err
	}
	_, err = io.Copy(entry, reader)
	return err
}

func fetchShortcutCover(ctx context.Context, raw string) ([]byte, error) {
	target, err := url.Parse(raw)
	if err != nil || target.Scheme != "https" || !allowedImageHost(target.Hostname()) {
		return nil, errors.New("不允许的封面地址")
	}
	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(requestCtx, http.MethodGet, target.String(), nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 MusicWeb/1.0")
	req.Header.Set("Referer", target.Scheme+"://"+target.Hostname()+"/")
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(next *http.Request, _ []*http.Request) error {
		if next.URL.Scheme != "https" || !allowedImageHost(next.URL.Hostname()) {
			return errors.New("封面重定向目标不在允许列表")
		}
		return nil
	}}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("封面源 HTTP %d", resp.StatusCode)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "image/") {
		return nil, errors.New("封面源返回的不是图片")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (20<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 20<<20 {
		return nil, errors.New("封面超过 20 MiB")
	}
	return data, nil
}
