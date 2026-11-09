package core

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestHuangguoAIPlaybackTarget(t *testing.T) {
	cases := []struct {
		url     string
		id      string
		episode int
		ok      bool
	}{
		{"https://huangguoai.com/video/12/", "12", 1, true},
		{"https://huangguoai.com/video/12", "12", 1, true},
		{"https://huangguoai.com/video/12/ep-7/", "12", 7, true},
		{"https://mirror.example/video/10425/ep-21", "10425", 21, true},
		{"https://huangguoai.com/topics/", "", 0, false},
		{"https://huangguoai.com/video/abc/", "", 0, false},
	}
	for _, c := range cases {
		id, ep, ok := huangguoAIPlaybackTarget(c.url)
		if id != c.id || ep != c.episode || ok != c.ok {
			t.Errorf("%s: got (%q,%d,%v) want (%q,%d,%v)", c.url, id, ep, ok, c.id, c.episode, c.ok)
		}
	}
}

func TestHuangguoAIMediaFromPlayback(t *testing.T) {
	expiry := time.Now().Add(2 * time.Hour).Unix()
	body := []byte(`{"data":{"mode":0,"timestamp":` + strconv.FormatInt(expiry, 10) + `,"manifest_method":"GET",` +
		`"cdn":{"m3u8Enabled":true,"otherEnabled":false,"authKey":"x-cdn-auth","authValue":"secret","app":"150"},` +
		`"id":"12","ep":2,"streams":{"main":"https://cdn.example/videos/a/a.m3u8?auth_key=1","hevc":""}},"status":1,"msg":""}`)
	media, err := huangguoAIMediaFromPlayback(body, "https://huangguoai.com/video/12/ep-2/")
	if err != nil {
		t.Fatal(err)
	}
	if media.URL != "https://cdn.example/videos/a/a.m3u8?auth_key=1" {
		t.Fatalf("url = %q", media.URL)
	}
	c := media.credentials
	if c == nil || c.origin != "https://cdn.example" || c.expires.Unix() != expiry {
		t.Fatalf("credentials = %+v", c)
	}
	if c.playlistHeaders["x-cdn-auth"] != "secret" || c.playlistHeaders["x-cdn-app"] != "150" || c.segmentHeaders != nil {
		t.Fatalf("headers = %+v / %+v", c.playlistHeaders, c.segmentHeaders)
	}

	playlist := httptest.NewRequest(http.MethodGet, "https://cdn.example/videos/a/a.m3u8", nil)
	if err := c.apply(playlist); err != nil {
		t.Fatal(err)
	}
	if playlist.Header.Get("x-cdn-auth") != "secret" || playlist.Header.Get("Cookie") != "" {
		t.Fatalf("playlist headers = %v", playlist.Header)
	}
	segment := httptest.NewRequest(http.MethodGet, "https://cdn.example/videos/a/0001.ts", nil)
	if err := c.apply(segment); err != nil {
		t.Fatal(err)
	}
	if segment.Header.Get("x-cdn-auth") != "" {
		t.Fatalf("segment should not carry CDN header when otherEnabled=false")
	}
}

func TestHuangguoAIMediaFromPlaybackErrors(t *testing.T) {
	for name, body := range map[string]string{
		"status":  `{"status":0,"msg":"请登录","data":{}}`,
		"empty":   `{"status":1,"data":{"manifest_method":"GET","streams":{"main":""}}}`,
		"method":  `{"status":1,"data":{"manifest_method":"POST","streams":{"main":"https://cdn.example/a.m3u8"}}}`,
		"garbage": `<html>blocked</html>`,
	} {
		if _, err := huangguoAIMediaFromPlayback([]byte(body), "https://huangguoai.com/video/1/"); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
