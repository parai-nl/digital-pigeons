package duif

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// De uitlegvideo en haar ondertitels komen via dezelfde FileServer als de
// core-assetroute: met een eigen type, en in stukken (Range), want Safari
// speelt een video alleen af als de server byte-ranges levert.
func TestExplainerMediaServedWithTypeAndRanges(t *testing.T) {
	server := httptest.NewServer(http.FileServer(http.FS(os.DirFS("frontend/public/media"))))
	defer server.Close()
	for file, want := range map[string]string{"uitlegvideo.mp4": "video/mp4", "uitlegvideo-en.vtt": "text/vtt; charset=utf-8"} {
		if _, err := os.Stat("frontend/public/media/" + file); err != nil {
			t.Skipf("uitlegvideo niet geïnstalleerd (%v); de build draait dan zonder videosectie", err)
		}
		request, _ := http.NewRequest(http.MethodGet, server.URL+"/"+file, nil)
		request.Header.Set("Range", "bytes=0-7")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if got := response.Header.Get("Content-Type"); got != want {
			t.Errorf("%s: Content-Type = %q, want %q", file, got, want)
		}
		if response.StatusCode != http.StatusPartialContent || response.ContentLength != 8 {
			t.Errorf("%s: range request gave %d with %d bytes", file, response.StatusCode, response.ContentLength)
		}
	}
}
