package core

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type providerMedia struct {
	credentials *providerMediaCredentials
	URL         string
	Referer     string
	Duration    time.Duration
	Playlist    string
	HLSKey      []byte
	CENCKey     []byte
	Quality     int
	Bitrate     int
	Codec       string
	Width       int
	Height      int
	Variants    []providerMedia
}

func (d *Downloader) providerBaseURL(source string) string {
	source = canonicalProviderSource(source)
	var configured, fallback string
	switch source {
	case sourceHuangguoAI:
		configured, fallback = d.cfg.HuangguoAIURL, huangguoAIBaseURL
	case sourceHuangguoVideo:
		configured, fallback = d.cfg.HuangguoVideoURL, huangguoVideoBaseURL
	case sourceHuangdou:
		configured, fallback = d.cfg.HuangdouURL, huangdouBaseURL
	case sourceHongguo:
		configured, fallback = d.cfg.HongguoURL, hongguoBaseURL
	case sourceHuangju:
		configured, fallback = d.cfg.HuangjuURL, huangjuBaseURL
	default:
		fallback = "https://d2pypzndaqisk.cloudfront.net"
	}
	return strings.TrimRight(firstNonEmpty(configured, fallback), "/")
}

func providerHLSVariants(media providerMedia, playlistURL, playlist string) []providerMedia {
	base, err := url.Parse(playlistURL)
	if err != nil {
		return nil
	}
	lines := strings.Split(playlist, "\n")
	var variants []providerMedia
	for index, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "#EXT-X-STREAM-INF:") {
			continue
		}
		attributes := nativeHLSAttributes(line)
		var reference string
		for next := index + 1; next < len(lines); next++ {
			candidate := strings.TrimSpace(lines[next])
			if candidate != "" && !strings.HasPrefix(candidate, "#") {
				reference = candidate
				break
			}
		}
		if reference == "" {
			continue
		}
		relative, err := url.Parse(reference)
		if err != nil {
			continue
		}
		resolved := base.ResolveReference(relative).String()
		if !isProviderHTTPMediaURL(resolved) {
			continue
		}
		variant := media
		variant.URL = resolved
		variant.Playlist = ""
		variant.Variants = nil
		if resolution := strings.Split(strings.ToLower(attributes["RESOLUTION"]), "x"); len(resolution) == 2 {
			variant.Width, _ = strconv.Atoi(resolution[0])
			variant.Height, _ = strconv.Atoi(resolution[1])
		}
		if variant.Height == 0 {
			name := strings.ToLower(attributes["NAME"])
			name = strings.TrimSuffix(name, "p")
			variant.Height, _ = strconv.Atoi(name)
		}
		variant.Quality = variant.Height
		variant.Bitrate, _ = strconv.Atoi(attributes["AVERAGE-BANDWIDTH"])
		if variant.Bitrate == 0 {
			variant.Bitrate, _ = strconv.Atoi(attributes["BANDWIDTH"])
		}
		variants = append(variants, variant)
	}
	return variants
}

func providerSourceForURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	switch {
	case host == "huangguoai.com" || strings.HasSuffix(host, ".ediayikma.cc") || strings.HasSuffix(host, ".agdkczeyx.cc"):
		return sourceHuangguoAI
	case host == "huangguo.video":
		return sourceHuangguoVideo
	case host == "tideember.cc" || host == "xqjurgek.top" || host == "iwgsqufx.cc" || host == "eyeonneb.cc":
		return sourceHuangdou
	case host == "hongguoduanju.com" || host == "www.hongguoduanju.com":
		return sourceHongguo
	case host == "huangju.net" || host == "www.huangju.net" || host == "api.huangju.net":
		return sourceHuangju
	default:
		return ""
	}
}

func (d *Downloader) providerURLCandidates(raw string) []string {
	source := providerSourceForURL(raw)
	if source == "" || source == sourceHuangju {
		return []string{raw}
	}
	parsed, _ := url.Parse(raw)
	d.providerMu.Lock()
	preferred := d.providerHosts[source]
	d.providerMu.Unlock()
	var candidates []string
	seen := map[string]bool{}
	add := func(candidate string) {
		if candidate != "" && !seen[candidate] {
			seen[candidate] = true
			candidates = append(candidates, candidate)
		}
	}
	add(rehostProviderURL(parsed, preferred))
	configured := d.providerBaseURL(source)
	add(rehostProviderURL(parsed, configured))
	if providerSourceForURL(configured) == "" {
		return candidates
	}
	for _, candidate := range providerURLCandidates(raw) {
		add(candidate)
	}
	return candidates
}

func (d *Downloader) resolveProviderMedia(ctx context.Context, task Task) (providerMedia, error) {
	chapter := task.Chapter
	chapter.Source = canonicalProviderSource(chapter.Source)
	if chapter.Source == "" {
		chapter.Source = sourceFromDramaID(task.DramaID)
	}
	if chapter.Source == sourceCloudFront {
		return d.resolveLegacyMedia(ctx, task)
	}
	if chapter.Source == sourceHuangju {
		return d.resolveHuangjuMedia(ctx, task)
	}
	if strings.HasPrefix(chapter.VideoURL, "hongguo-cenc://") {
		return d.resolveHongguoMedia(ctx, task)
	}
	if chapter.PageURL == "" && (chapter.Source == sourceHuangguoAI || chapter.Source == sourceHuangguoVideo) {
		if source, sourceID, valid := splitProviderDramaID(task.DramaID); valid {
			_, chapters, err := d.GetHuangguoChapters(ctx, source, sourceID)
			if err != nil {
				return providerMedia{}, fmt.Errorf("刷新旧任务播放地址失败: %w", err)
			}
			matched := false
			for _, fresh := range chapters {
				if fresh.ID == chapter.ID {
					chapter = fresh
					matched = true
					break
				}
			}
			if !matched {
				return providerMedia{}, fmt.Errorf("原章节已变化，请更新该合集后重新下载")
			}
		}
	}
	media := providerMedia{URL: chapter.VideoURL, Referer: firstNonEmpty(chapter.Referer, d.providerBaseURL(chapter.Source)+"/")}
	if chapter.Source == sourceHuangdou {
		_, sourceID, valid := splitProviderDramaID(task.DramaID)
		if valid {
			sequence, err := strconv.Atoi(chapter.EpisodeString(task.Index))
			if err != nil || sequence < 1 {
				return providerMedia{}, fmt.Errorf("黄豆集数无效")
			}
			client := newHuangdouAPIClient(d)
			media, err = d.resolveHuangdouPlayback(ctx, client, sourceID, sequence)
			if err != nil {
				return providerMedia{}, err
			}
			media.Referer = client.host + "/home"
		}
	}
	var playbackAPIErr error
	if chapter.PageURL != "" && chapter.Source == sourceHuangguoAI {
		resolved, err := d.resolveHuangguoAIPlayback(ctx, chapter.PageURL)
		if err == nil {
			media = resolved
		} else {
			playbackAPIErr = err
		}
	}
	if chapter.PageURL != "" && (chapter.Source == sourceHuangguoVideo || chapter.Source == sourceHuangguoAI && playbackAPIErr != nil) {
		responses := &playbackResponseURLs{}
		pageContext := context.WithValue(ctx, playbackResponseURLsKey{}, responses)
		body, err := d.fetchProviderText(pageContext, chapter.PageURL, media.Referer)
		if err != nil {
			return providerMedia{}, err
		}
		pageURL := chapter.PageURL
		if actual, ok := responses.values.Load(chapter.PageURL); ok {
			pageURL = actual.(string)
		}
		if chapter.Source == sourceHuangguoAI {
			media.URL = parseAIVideoURL(body, pageURL)
		} else {
			media.URL = parseDataHLS(body, pageURL)
		}
		media.Referer = pageURL
	}
	if !isProviderHTTPMediaURL(media.URL) {
		if playbackAPIErr != nil {
			return providerMedia{}, fmt.Errorf("%s 未返回有效播放地址：%w", chapter.Source, playbackAPIErr)
		}
		return providerMedia{}, fmt.Errorf("%s 未返回有效播放地址，请刷新章节或确认站点访问权限", chapter.Source)
	}
	parsed, _ := url.Parse(media.URL)
	if strings.HasSuffix(strings.ToLower(parsed.Path), ".m3u8") {
		playlistCtx := ctx
		if media.credentials != nil {
			playlistCtx = providerMediaContext(ctx, media.credentials)
		}
		playlist, finalURL, err := d.fetchMediaPlaylist(playlistCtx, media.URL, media.Referer)
		if err != nil {
			return providerMedia{}, fmt.Errorf("获取播放列表失败: %w", err)
		}
		if !strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(playlist, "\ufeff")), "#EXTM3U") {
			return providerMedia{}, fmt.Errorf("站点未返回有效 M3U8，可能需要登录或链接已失效")
		}
		if duration := m3u8Duration(playlist); duration > 0 {
			media.Duration = duration
		}
		media.Playlist = playlist
		media.URL = finalURL
		media.Variants = providerHLSVariants(media, finalURL, playlist)
	}
	return media, nil
}
