package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/sadeq-n-yazdi/todo-bot/internal/i18n"
)

type i18nResponse struct {
	Lang     string            `json:"lang"`
	Dir      string            `json:"dir"`
	Messages map[string]string `json:"messages"`
}

// webMessages returns every web.* string of lang as a raw template (T with
// no arguments). A key with no string in lang falls back to English.
func webMessages(lang i18n.Lang) map[string]string {
	loc := i18n.Locale{Lang: lang}
	out := map[string]string{}
	for _, k := range i18n.Keys(i18n.En) {
		if strings.HasPrefix(k, "web.") {
			out[k] = loc.T(k)
		}
	}
	return out
}

// handleI18n is endpoint 8, GET /api/i18n?lang=en. It is public, because
// the login screen needs it before a session exists (§5.4).
func (s *Server) handleI18n(w http.ResponseWriter, r *http.Request) {
	lang := i18n.Lang(r.URL.Query().Get("lang"))
	if !lang.Valid() {
		s.fail(w, r, errBadLang)
		return
	}
	infoOf(r).loc = i18n.Locale{Lang: lang}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(i18nResponse{Lang: string(lang), Dir: dirOf(lang), Messages: webMessages(lang)}); err != nil {
		s.fail(w, r, err)
		return
	}
	sum := sha256.Sum256(buf.Bytes())
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`
	h := w.Header()
	h.Set("Cache-Control", cacheI18n)
	h.Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}
