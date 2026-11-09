package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Since 2026-11 huangguoai.com no longer embeds play URLs in the episode page
// (videoInitialData.videoSrc / epPlaySrcs are empty). Its web player POSTs to
// /api/videos/{id}/episodes/{ep}/playback and sends CDN auth headers on .m3u8
// requests. This file mirrors that flow.

var reHuangguoAIEpisodePath = regexp.MustCompile(`/video/(\d+)(?:/ep-(\d+))?/?$`)

type huangguoAIPlaybackCDN struct {
	M3U8Enabled  bool   `json:"m3u8Enabled"`
	OtherEnabled bool   `json:"otherEnabled"`
	AuthKey      string `json:"authKey"`
	AuthValue    string `json:"authValue"`
	App          string `json:"app"`
}

type huangguoAIPlaybackPayload struct {
	Status int    `json:"status"`
	Msg    string `json:"msg"`
	Data   struct {
		Timestamp      int64                 `json:"timestamp"`
		ManifestMethod string                `json:"manifest_method"`
		CDN            huangguoAIPlaybackCDN `json:"cdn"`
		ID             json.RawMessage       `json:"id"`
		Ep             json.RawMessage       `json:"ep"`
		Streams        struct {
			Main string `json:"main"`
			HEVC string `json:"hevc"`
		} `json:"streams"`
	} `json:"data"`
}

// huangguoAIPlaybackTarget extracts the video id and episode number from an
// episode page URL such as https://huangguoai.com/video/12/ep-2/ (episode 1 is
// the bare /video/12/ page).
func huangguoAIPlaybackTarget(pageURL string) (videoID string, episode int, ok bool) {
	parsed, err := url.Parse(pageURL)
	if err != nil {
		return "", 0, false
	}
	m := reHuangguoAIEpisodePath.FindStringSubmatch(parsed.Path)
	if len(m) < 2 {
		return "", 0, false
	}
	episode = 1
	if m[2] != "" {
		episode, err = strconv.Atoi(m[2])
		if err != nil || episode < 1 {
			return "", 0, false
		}
	}
	return m[1], episode, true
}

func (d *Downloader) resolveHuangguoAIPlayback(ctx context.Context, pageURL string) (providerMedia, error) {
	videoID, episode, ok := huangguoAIPlaybackTarget(pageURL)
	if !ok {
		return providerMedia{}, fmt.Errorf("无法从章节地址识别黄果 AI 剧集: %s", pageURL)
	}
	base := d.providerBaseURL(sourceHuangguoAI)
	d.providerMu.Lock()
	if host := d.providerHosts[sourceHuangguoAI]; host != "" {
		base = host
	}
	d.providerMu.Unlock()
	referer := base + "/video/" + videoID + "/"
	if episode > 1 {
		referer += "ep-" + strconv.Itoa(episode) + "/"
	}
	endpoint := fmt.Sprintf("%s/api/videos/%s/episodes/%d/playback", base, videoID, episode)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader([]byte("{}")))
	if err != nil {
		return providerMedia{}, err
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Referer", referer)
	request.Header.Set("Origin", base)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/plain, */*")
	request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	response, err := d.doCatalogRequestWithTimeout(request, providerTimeout)
	if err != nil {
		return providerMedia{}, fmt.Errorf("黄果 AI 取流请求失败: %w", err)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	response.Body.Close()
	if err != nil {
		return providerMedia{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return providerMedia{}, fmt.Errorf("黄果 AI 取流接口返回 HTTP %d", response.StatusCode)
	}
	return huangguoAIMediaFromPlayback(body, referer)
}

func huangguoAIMediaFromPlayback(body []byte, referer string) (providerMedia, error) {
	var payload huangguoAIPlaybackPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return providerMedia{}, fmt.Errorf("黄果 AI 取流响应无法解析: %w", err)
	}
	if payload.Status != 1 {
		msg := strings.TrimSpace(payload.Msg)
		if msg == "" {
			msg = "status " + strconv.Itoa(payload.Status)
		}
		return providerMedia{}, fmt.Errorf("黄果 AI 取流失败: %s", msg)
	}
	data := payload.Data
	if data.ManifestMethod != "" && !strings.EqualFold(data.ManifestMethod, http.MethodGet) {
		return providerMedia{}, fmt.Errorf("黄果 AI 播放清单方式不受支持: %s", data.ManifestMethod)
	}
	address := strings.TrimSpace(data.Streams.Main)
	parsed, err := url.Parse(address)
	if err != nil || !isProviderHTTPMediaURL(address) || parsed.User != nil {
		return providerMedia{}, errors.New("黄果 AI 未返回有效播放地址")
	}
	credentials := &providerMediaCredentials{
		origin:    providerMediaOrigin(parsed),
		referer:   referer,
		userAgent: userAgent,
	}
	if data.Timestamp > 0 {
		if expires := time.Unix(data.Timestamp, 0); expires.After(time.Now()) {
			credentials.expires = expires
		}
	}
	if cdn := data.CDN; cdn.AuthKey != "" && cdn.AuthValue != "" && cdn.App != "" {
		headers := map[string]string{cdn.AuthKey: cdn.AuthValue, "x-cdn-app": cdn.App}
		if cdn.M3U8Enabled {
			credentials.playlistHeaders = headers
		}
		if cdn.OtherEnabled {
			credentials.segmentHeaders = headers
		}
	}
	return providerMedia{URL: address, Referer: referer, credentials: credentials}, nil
}
