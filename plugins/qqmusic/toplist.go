package qqmusic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/liuran001/MusicBot-Go/bot/platform"
)

// QQ Music serves official charts (热歌榜 / 飙升榜 …) separately from user
// playlists: charts are keyed by a small numeric topid, playlists by a long
// disstid. Asking GetPlaylist for "26" therefore fails — 26 is a topid.
//
// The modern musicu.fcg toplist modules reject anonymous callers
// (code 500003 / subcode 860100005) regardless of module, method or comm
// variant, so this uses the legacy fcg_v8_toplist_cp.fcg endpoint, which still
// answers unauthenticated GETs and returns the full chart.
//
// Callers address charts as "toplist:<topid>" (e.g. "toplist:26"), which
// GetPlaylist routes here so charts and playlists share one entry point.
const (
	toplistIDPrefix  = "toplist:"
	toplistEndpoint  = "https://c.y.qq.com/v8/fcg-bin/fcg_v8_toplist_cp.fcg"
	toplistMaxTracks = 100
)

// toplistResponse mirrors the legacy endpoint's shape. Each songlist entry
// wraps the song under "data", which is the same layout as qqPlaylistSong.
type toplistResponse struct {
	Code    int `json:"code"`
	TopInfo struct {
		ListName string `json:"ListName"`
		Info     string `json:"info"`
		Pic      string `json:"pic"`
		HeadPic  string `json:"headPic_v12"`
	} `json:"topinfo"`
	TotalSongNum int `json:"total_song_num"`
	SongList     []struct {
		Data qqPlaylistSong `json:"data"`
	} `json:"songlist"`
}

// GetToplist fetches an official QQ Music chart by its numeric top ID.
func (c *Client) GetToplist(ctx context.Context, topID string) (*qqPlaylistData, error) {
	topID = strings.TrimSpace(topID)
	if topID == "" {
		return nil, platform.NewNotFoundError("qqmusic", "toplist", "")
	}
	id, err := strconv.Atoi(topID)
	if err != nil {
		return nil, platform.NewNotFoundError("qqmusic", "toplist", topID)
	}
	limit := platform.PlaylistLimitFromContext(ctx)
	if limit <= 0 || limit > toplistMaxTracks {
		limit = toplistMaxTracks
	}

	url := fmt.Sprintf("%s?topid=%d&format=json&tpl=3&page=detail&type=top&song_begin=0&song_num=%d",
		toplistEndpoint, id, limit)
	body, err := c.getWithHeaders(ctx, url)
	if err != nil {
		return nil, err
	}
	var resp toplistResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("qqmusic: decode toplist: %w", err)
	}
	if resp.Code != 0 {
		return nil, platform.NewUnavailableError("qqmusic", "toplist", topID)
	}
	songs := make([]qqPlaylistSong, 0, len(resp.SongList))
	for _, item := range resp.SongList {
		songs = append(songs, item.Data)
	}
	if len(songs) == 0 {
		return nil, platform.NewNotFoundError("qqmusic", "toplist", topID)
	}
	total := resp.TotalSongNum
	if total <= 0 {
		total = len(songs)
	}
	return &qqPlaylistData{
		ID:          int64(id),
		Name:        firstNonEmptyQQ(resp.TopInfo.ListName, "QQ音乐榜单"),
		Desc:        stripHTMLQQ(resp.TopInfo.Info),
		Logo:        firstNonEmptyQQ(resp.TopInfo.Pic, resp.TopInfo.HeadPic),
		CreatorName: "QQ音乐",
		Total:       total,
		Songlist:    songs,
	}, nil
}

// getWithHeaders issues a GET with the Referer/UA the legacy endpoint expects.
func (c *Client) getWithHeaders(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for key, values := range c.headers {
		for _, v := range values {
			req.Header.Add(key, v)
		}
	}
	if req.Header.Get("Referer") == "" {
		req.Header.Set("Referer", "https://y.qq.com/")
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "Mozilla/5.0")
	}
	client := c.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("qqmusic: toplist HTTP %d", resp.StatusCode)
	}
	// Cap the read so a malformed/hostile response can't exhaust memory.
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// stripHTMLQQ flattens the chart blurb, which arrives with <br> markup.
func stripHTMLQQ(s string) string {
	s = strings.ReplaceAll(s, "<br>", " ")
	s = strings.ReplaceAll(s, "<br/>", " ")
	for {
		start := strings.Index(s, "<")
		if start < 0 {
			break
		}
		end := strings.Index(s[start:], ">")
		if end < 0 {
			break
		}
		s = s[:start] + s[start+end+1:]
	}
	return strings.TrimSpace(s)
}

func firstNonEmptyQQ(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
