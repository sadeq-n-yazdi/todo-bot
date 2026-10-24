package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// maxInitDataSize is the largest accepted initData string (WEB-DESIGN 3.2 step 1).
const maxInitDataSize = 4 << 10

// maxInitDataFuture is how far auth_date may be ahead of the clock.
const maxInitDataFuture = 60 * time.Second

// Sentinel errors of verifyInitData. The caller maps them to API codes:
// bad_request, bad_signature, expired and a bad user. None of them holds
// any part of the input.
var (
	errInitBadRequest   = errors.New("initdata: bad request")
	errInitBadSignature = errors.New("initdata: bad signature")
	errInitExpired      = errors.New("initdata: expired")
	errInitBadUser      = errors.New("initdata: bad user")
)

// initData is the verified content of a Telegram initData string.
type initData struct {
	UserID       int64
	Username     string
	FirstName    string
	LanguageCode string
	AuthDate     time.Time
	StartParam   string
}

// verifyInitData does steps 1 to 9 of WEB-DESIGN 3.2. It does not check the
// allowlist. maxAge is 1 h for a sign-in and 10 min for a login code.
func verifyInitData(raw, botToken string, now time.Time, maxAge time.Duration) (initData, error) {
	if raw == "" || len(raw) > maxInitDataSize {
		return initData{}, errInitBadRequest
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return initData{}, errInitBadRequest
	}
	for _, v := range values {
		if len(v) != 1 {
			return initData{}, errInitBadRequest
		}
	}
	hashHex := values.Get("hash")
	values.Del("hash")

	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, len(keys))
	for i, k := range keys {
		lines[i] = k + "=" + values.Get(k)
	}

	secretMac := hmac.New(sha256.New, []byte("WebAppData"))
	secretMac.Write([]byte(botToken))
	mac := hmac.New(sha256.New, secretMac.Sum(nil))
	mac.Write([]byte(strings.Join(lines, "\n")))
	want := mac.Sum(nil)

	got, err := hex.DecodeString(hashHex)
	if err != nil || !hmac.Equal(got, want) {
		return initData{}, errInitBadSignature
	}

	sec, err := strconv.ParseInt(values.Get("auth_date"), 10, 64)
	if err != nil || sec < 0 {
		return initData{}, errInitBadRequest
	}
	authDate := time.Unix(sec, 0)
	if now.Sub(authDate) > maxAge || authDate.Sub(now) > maxInitDataFuture {
		return initData{}, errInitExpired
	}

	var user struct {
		ID           json.RawMessage `json:"id"`
		Username     string          `json:"username"`
		FirstName    string          `json:"first_name"`
		LanguageCode string          `json:"language_code"`
	}
	if json.Unmarshal([]byte(values.Get("user")), &user) != nil {
		return initData{}, errInitBadUser
	}
	id, err := strconv.ParseInt(string(user.ID), 10, 64)
	if err != nil || id <= 0 {
		return initData{}, errInitBadUser
	}
	return initData{
		UserID:       id,
		Username:     user.Username,
		FirstName:    user.FirstName,
		LanguageCode: user.LanguageCode,
		AuthDate:     authDate.UTC(),
		StartParam:   values.Get("start_param"),
	}, nil
}
