package archivefeed

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/a32fred/hdcam-client/internal/archive"
	"github.com/a32fred/hdcam-client/internal/export"
)

type Options struct {
	Source, Username, Password string
	Audio                      bool
}
type Report struct {
	VideoFrames  uint64 `json:"video_frames"`
	AudioSamples uint64 `json:"audio_samples"`
}
type event struct {
	data  []byte
	audio bool
	err   error
}

func sourceURL(opts Options) (*url.URL, error) {
	u, err := url.Parse(opts.Source)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(opts.Source, "#") || (u.Path != "" && u.Path != "/") || u.RawPath != "" {
		return nil, errors.New("invalid viewer source")
	}
	if u.Scheme == "http" {
		ip := net.ParseIP(u.Hostname())
		if ip == nil || !ip.IsLoopback() {
			return nil, errors.New("HTTP viewer source must use a loopback IP")
		}
	}
	if len(opts.Username) == 0 || len(opts.Username) > 128 || strings.ContainsAny(opts.Username, ":\r\n\x00") || len(opts.Password) == 0 || len(opts.Password) > 1024 || strings.ContainsAny(opts.Password, "\r\n\x00") {
		return nil, errors.New("invalid viewer credentials")
	}
	return u, nil
}

func Validate(opts Options) error {
	_, err := sourceURL(opts)
	return err
}

func Run(ctx context.Context, opts Options, writer *archive.Writer) (Report, error) {
	var report Report
	u, err := sourceURL(opts)
	if err != nil {
		return report, err
	}
	if writer == nil {
		return report, errors.New("archive writer is required")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	events := make(chan event, 4)
	var wg sync.WaitGroup
	send := func(e event) bool {
		select {
		case events <- e:
			return true
		case <-ctx.Done():
			return false
		}
	}
	start := func(audio bool) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := readStream(ctx, client, u, opts, audio, send)
			if err != nil {
				send(event{err: err})
			}
		}()
	}
	start(false)
	if opts.Audio {
		start(true)
	}
	defer func() { cancel(); wg.Wait() }()
	for {
		select {
		case <-ctx.Done():
			return report, ctx.Err()
		case e := <-events:
			if e.err != nil {
				if ctx.Err() != nil {
					return report, ctx.Err()
				}
				return report, e.err
			}
			if e.audio {
				if err := writer.WriteAudio(e.data); err != nil {
					return report, errors.New("archive audio write failed")
				}
				report.AudioSamples += uint64(len(e.data) / 2)
			} else {
				if err := writer.WriteVideo(e.data); err != nil {
					return report, errors.New("archive video write failed")
				}
				report.VideoFrames++
			}
		}
	}
}

func readStream(ctx context.Context, client *http.Client, base *url.URL, opts Options, audio bool, send func(event) bool) error {
	u := *base
	u.Path = "/stream"
	if audio {
		u.Path = "/audio"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return errors.New("invalid viewer request")
	}
	req.SetBasicAuth(opts.Username, opts.Password)
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("viewer request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("viewer returned unexpected status")
	}
	mediaType, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil {
		return errors.New("invalid viewer content type")
	}
	if audio {
		if mediaType != "application/octet-stream" || resp.Header.Get("X-Audio-Format") != "pcm_s16le;rate=8000;channels=1" {
			return errors.New("invalid viewer audio format")
		}
		for {
			b := make([]byte, 1024)
			n, err := io.ReadFull(resp.Body, b)
			if n%2 != 0 {
				return errors.New("invalid viewer audio length")
			}
			if n > 0 && !send(event{data: b[:n], audio: true}) {
				return ctx.Err()
			}
			if err != nil {
				return errors.New("viewer audio stream ended")
			}
		}
	}
	boundary := params["boundary"]
	if mediaType != "multipart/x-mixed-replace" || len(boundary) < 1 || len(boundary) > 70 || strings.ContainsAny(boundary, "\r\n") {
		return errors.New("invalid viewer video format")
	}
	reader := multipart.NewReader(resp.Body, boundary)
	for {
		part, err := reader.NextPart()
		if err != nil {
			return errors.New("viewer video stream ended")
		}
		kind, _, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if err != nil || kind != "image/jpeg" {
			return errors.New("invalid viewer frame type")
		}
		data, err := io.ReadAll(io.LimitReader(part, (4<<20)+1))
		if err != nil || len(data) > 4<<20 {
			return errors.New("invalid viewer frame size")
		}
		data, err = export.CanonicalJPEG(data)
		if err != nil {
			return errors.New("invalid viewer JPEG")
		}
		if !send(event{data: data}) {
			return ctx.Err()
		}
	}
}
