package archive

import (
	"embed"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

//go:embed recordings.html recordings.js
var browserFiles embed.FS

type privateResponse struct{ http.ResponseWriter }

func (w privateResponse) WriteHeader(code int) {
	w.Header().Set("Cache-Control", "no-store")
	w.ResponseWriter.WriteHeader(code)
}

func (w privateResponse) Write(b []byte) (int, error) {
	err := http.NewResponseController(w.ResponseWriter).SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err != nil && !errors.Is(err, http.ErrNotSupported) {
		return 0, err
	}
	return w.ResponseWriter.Write(b)
}

func (w privateResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Handler serves only completed archive records. Authentication belongs to its parent.
func Handler(reader *Reader) http.Handler {
	busy := make(chan struct{}, 8)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w = privateResponse{w}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' blob:; media-src 'self'; connect-src 'self'; script-src 'self'; style-src 'unsafe-inline'; frame-ancestors 'none'")
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(5 * time.Second))
		defer func() { _ = http.NewResponseController(w).SetWriteDeadline(time.Time{}) }()
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
			return
		}
		select {
		case busy <- struct{}{}:
			defer func() { <-busy }()
		default:
			http.Error(w, "Arquivo ocupado", http.StatusServiceUnavailable)
			return
		}
		fail := func(err error) {
			if errors.Is(err, os.ErrNotExist) {
				http.NotFound(w, r)
			} else {
				http.Error(w, "Não foi possível ler a gravação", http.StatusInternalServerError)
			}
		}
		writeJSON := func(v any) { w.Header().Set("Content-Type", "application/json"); _ = json.NewEncoder(w).Encode(v) }
		switch r.URL.Path {
		case "/recordings", "/recordings/app.js":
			name, ct := "recordings.html", "text/html; charset=utf-8"
			if r.URL.Path == "/recordings/app.js" {
				name, ct = "recordings.js", "text/javascript; charset=utf-8"
			}
			b, _ := browserFiles.ReadFile(name)
			w.Header().Set("Content-Type", ct)
			_, _ = w.Write(b)
			return
		case "/recordings/list":
			list, err := reader.List()
			if err != nil {
				fail(err)
				return
			}
			// Frame arrays are loaded only for the selected segment.
			for i := range list {
				list[i].VideoMS = nil
				list[i].VideoSizes = nil
			}
			writeJSON(list)
			return
		}
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) < 4 || parts[0] != "" || parts[1] != "recordings" || !ValidID(parts[2]) {
			http.NotFound(w, r)
			return
		}
		id := parts[2]
		if len(parts) == 4 && parts[3] == "index.json" {
			m, err := reader.Segment(id)
			if err != nil {
				fail(err)
				return
			}
			writeJSON(m)
			return
		}
		if len(parts) == 4 && parts[3] == "audio.wav" {
			f, _, err := reader.OpenAudio(id)
			if err != nil {
				fail(err)
				return
			}
			defer f.Close()
			st, err := f.Stat()
			if err != nil {
				fail(err)
				return
			}
			w.Header().Set("Content-Type", "audio/wav")
			http.ServeContent(w, r, "audio.wav", st.ModTime(), f)
			return
		}
		if len(parts) == 5 && parts[3] == "frame" {
			n, err := strconv.Atoi(parts[4])
			if err != nil || n < 0 || strconv.Itoa(n) != parts[4] {
				http.NotFound(w, r)
				return
			}
			b, err := reader.Frame(id, n)
			if err != nil {
				fail(err)
				return
			}
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(b)
			return
		}
		http.NotFound(w, r)
	})
}
