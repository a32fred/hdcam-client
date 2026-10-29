package live

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestSnapshotConditionalRequestsFollowFreshGeneration(t *testing.T) {
	hub := New()
	first := []byte("first-jpeg")
	hub.Publish(first)
	server := httptest.NewServer(hub.Handler("token"))
	defer server.Close()

	resp := snapshotRequest(t, server, "")
	firstETag := resp.Header.Get("ETag")
	firstBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || firstETag != `"1"` || !bytes.Equal(firstBody, first) {
		t.Fatalf("first snapshot: status=%d etag=%q body=%q", resp.StatusCode, firstETag, firstBody)
	}

	resp = snapshotRequest(t, server, firstETag)
	conditionalBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNotModified || resp.Header.Get("ETag") != firstETag || len(conditionalBody) != 0 {
		t.Fatalf("matching ETag response: status=%d etag=%q body=%d bytes", resp.StatusCode, resp.Header.Get("ETag"), len(conditionalBody))
	}

	hub.mu.Lock()
	hub.updated = time.Now().Add(-16 * time.Second)
	hub.mu.Unlock()
	resp = snapshotRequest(t, server, firstETag)
	staleBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable || len(staleBody) == 0 {
		t.Fatalf("stale matching snapshot: status=%d body=%q, want 503 before conditional handling", resp.StatusCode, staleBody)
	}

	second := []byte("second-jpeg")
	hub.Publish(second)
	resp = snapshotRequest(t, server, firstETag)
	secondETag := resp.Header.Get("ETag")
	secondBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || secondETag == firstETag || secondETag != `"2"` || !bytes.Equal(secondBody, second) {
		t.Fatalf("new generation snapshot: status=%d etag=%q body=%q", resp.StatusCode, secondETag, secondBody)
	}
}

func TestSnapshotAuthenticationRemainsRequired(t *testing.T) {
	hub := New()
	hub.Publish([]byte("jpeg"))
	server := httptest.NewServer(hub.Handler("token"))
	defer server.Close()
	resp, err := http.Get(server.URL + "/snapshot")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized || resp.Header.Get("WWW-Authenticate") == "" {
		t.Fatalf("unauthenticated snapshot: status=%d challenge=%q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}
}

func TestViewerPageUsesAuthenticatedScriptAndDeferredVideoSource(t *testing.T) {
	server := httptest.NewServer(New().Handler("token"))
	defer server.Close()
	client := server.Client()
	page := request(t, client, http.MethodGet, server.URL+"/", "hdcam", "token")
	pageBody, err := io.ReadAll(page.Body)
	page.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	pageHTML := string(pageBody)
	if page.StatusCode != http.StatusOK {
		t.Fatalf("page status = %d", page.StatusCode)
	}
	csp := page.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "img-src 'self' blob:") || !strings.Contains(csp, "script-src 'self'") {
		t.Fatalf("CSP does not permit same-origin script and blob image playback: %q", csp)
	}
	img := regexp.MustCompile(`(?is)<img\b[^>]*>`).FindString(pageHTML)
	if !strings.Contains(img, `id="video"`) || regexp.MustCompile(`(?i)\bsrc\s*=`).MatchString(img) {
		t.Fatalf("video image must start without a source: %q", img)
	}

	resp := request(t, client, http.MethodGet, server.URL+"/viewer.js", "hdcam", "token")
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || !bytes.Equal(body, []byte(viewerJS)) {
		t.Fatalf("viewer.js response: status=%d bytes=%d", resp.StatusCode, len(body))
	}
}

func snapshotRequest(t *testing.T, server *httptest.Server, etag string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, server.URL+"/snapshot", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth("hdcam", "token")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}
