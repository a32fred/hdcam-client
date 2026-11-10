package status

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"time"

	"github.com/a32fred/hdcam-client/internal/archivefeed"
	"github.com/a32fred/hdcam-client/internal/live"
)

type Options struct {
	URL, Username, Password string
	Timeout                 time.Duration
}

func Read(ctx context.Context, opts Options) (live.Health, error) {
	var health live.Health
	if archivefeed.Validate(archivefeed.Options{Source: opts.URL, Username: opts.Username, Password: opts.Password}) != nil || opts.Timeout <= 0 || opts.Timeout > 60*time.Second {
		return health, errors.New("invalid status options")
	}
	u, err := url.Parse(opts.URL)
	if err != nil {
		return health, errors.New("invalid status options")
	}
	u.Path = "/health"
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return health, errors.New("invalid status request")
	}
	req.SetBasicAuth(opts.Username, opts.Password)
	req.Header.Set("Cache-Control", "no-store")
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return health, errors.New("status request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return health, errors.New("status authentication failed")
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusServiceUnavailable {
		return health, errors.New("unexpected status response")
	}
	kind, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" {
		return health, errors.New("invalid status response")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
	if err != nil || len(body) > 64<<10 {
		return health, errors.New("invalid status response")
	}
	var required struct {
		Online         *bool      `json:"online"`
		Frames         *uint64    `json:"frames"`
		LastFrame      *time.Time `json:"last_frame"`
		LastFrameAgeMS *int64     `json:"last_frame_age_ms"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	if dec.Decode(&required) != nil || required.Online == nil || required.Frames == nil || required.LastFrame == nil || dec.Decode(new(any)) != io.EOF || json.Unmarshal(body, &health) != nil {
		return live.Health{}, errors.New("invalid status response")
	}
	if required.LastFrameAgeMS == nil {
		health.LastFrameAgeMS = -1
	}
	if health.Online != (resp.StatusCode == http.StatusOK) || (health.Online && (health.Frames == 0 || health.LastFrame.IsZero())) || math.IsNaN(health.SourceFPS) || math.IsInf(health.SourceFPS, 0) || health.SourceFPS < 0 || health.LastFrameAgeMS < -1 {
		return live.Health{}, errors.New("invalid status response")
	}
	switch health.CapturePhase {
	case "", "idle", "connecting", "streaming", "retrying", "stalled":
	default:
		return live.Health{}, errors.New("invalid status response")
	}
	switch health.LastFailureCode {
	case "", "timeout", "stalled", "startup_failed", "stream_failed", "runtime_missing", "invalid_media", "no_video", "capture_error", "session_completed":
	default:
		return live.Health{}, errors.New("invalid status response")
	}
	return health, nil
}
