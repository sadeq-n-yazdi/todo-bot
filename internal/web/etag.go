package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

// listETag is the weak ETag of a list (docs/WEB-DESIGN.md §5.1): the first
// 16 hex characters of the SHA-256 of the JSON of items, page and pages.
// rendered_at is left out on purpose. It is the time of the response, so it
// differs on each request; with it, no ETag would ever match and a client
// could never get a 304.
func listETag(items []itemDTO, page, pages int) (string, error) {
	b, err := json.Marshal(struct {
		Items []itemDTO `json:"items"`
		Page  int       `json:"page"`
		Pages int       `json:"pages"`
	}{items, page, pages})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return `W/"` + hex.EncodeToString(sum[:])[:16] + `"`, nil
}

// etagMatches reports whether an If-None-Match header lists etag. The
// comparison is the weak one of RFC 9110: a W/ prefix is ignored.
func etagMatches(header, etag string) bool {
	want := strings.TrimPrefix(etag, "W/")
	for _, p := range strings.Split(header, ",") {
		p = strings.TrimSpace(p)
		if p == "*" || strings.TrimPrefix(p, "W/") == want {
			return true
		}
	}
	return false
}

// writeList sends a list response, or 304 if the client has it already.
func writeList(w http.ResponseWriter, r *http.Request, l listDTO) error {
	tag, err := listETag(l.Items, l.Page, l.Pages)
	if err != nil {
		return err
	}
	h := w.Header()
	h.Set("Cache-Control", "private, no-cache")
	h.Set("Vary", "Authorization, Cookie")
	h.Set("ETag", tag)
	if inm := r.Header.Get("If-None-Match"); inm != "" && etagMatches(inm, tag) {
		w.WriteHeader(http.StatusNotModified)
		return nil
	}
	writeJSON(w, http.StatusOK, l)
	return nil
}
