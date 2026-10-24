package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	testToken = "123456:TEST-token"
	userAnn   = `{"id":42,"first_name":"Ann","username":"ann","language_code":"fa"}`
)

var testNow = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

// signFields builds an initData string with the steps of WEB-DESIGN 3.2.
func signFields(token string, fields map[string]string) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, k+"="+fields[k])
	}
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(token))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(strings.Join(lines, "\n")))
	v := url.Values{}
	for k, val := range fields {
		v.Set(k, val)
	}
	v.Set("hash", hex.EncodeToString(mac.Sum(nil)))
	return v.Encode()
}

func fields(authDate time.Time, user string) map[string]string {
	return map[string]string{
		"auth_date":   strconv.FormatInt(authDate.Unix(), 10),
		"query_id":    "AAHdF6IQAAAAAN0XohDhrOrc",
		"user":        user,
		"start_param": "g-100",
	}
}

func TestVerifyInitDataOK(t *testing.T) {
	for _, maxAge := range []time.Duration{time.Hour, 10 * time.Minute} {
		raw := signFields(testToken, fields(testNow.Add(-maxAge), userAnn))
		got, err := verifyInitData(raw, testToken, testNow, maxAge)
		if err != nil {
			t.Fatalf("maxAge %v: %v", maxAge, err)
		}
		if got.UserID != 42 || got.Username != "ann" || got.FirstName != "Ann" ||
			got.LanguageCode != "fa" || got.StartParam != "g-100" ||
			!got.AuthDate.Equal(testNow.Add(-maxAge)) {
			t.Fatalf("maxAge %v: unexpected result %+v", maxAge, got)
		}
	}
}

func TestVerifyInitDataAge(t *testing.T) {
	for _, maxAge := range []time.Duration{time.Hour, 10 * time.Minute} {
		old := signFields(testToken, fields(testNow.Add(-maxAge-time.Second), userAnn))
		if _, err := verifyInitData(old, testToken, testNow, maxAge); !errors.Is(err, errInitExpired) {
			t.Errorf("maxAge %v old: got %v", maxAge, err)
		}
	}
	cases := []struct {
		ahead time.Duration
		want  error
	}{
		{59 * time.Second, nil},
		{60 * time.Second, nil},
		{61 * time.Second, errInitExpired},
	}
	for _, c := range cases {
		raw := signFields(testToken, fields(testNow.Add(c.ahead), userAnn))
		_, err := verifyInitData(raw, testToken, testNow, time.Hour)
		if !errors.Is(err, c.want) {
			t.Errorf("ahead %v: got %v, want %v", c.ahead, err, c.want)
		}
	}
}

func TestVerifyInitDataSignature(t *testing.T) {
	good := signFields(testToken, fields(testNow, userAnn))
	v, _ := url.ParseQuery(good)
	hash := v.Get("hash")

	mutate := func(f func(url.Values)) string {
		c, _ := url.ParseQuery(good)
		f(c)
		return c.Encode()
	}
	flipped := "0" + hash[1:]
	if flipped == hash {
		flipped = "1" + hash[1:]
	}
	cases := map[string]string{
		"wrong hash":   mutate(func(c url.Values) { c.Set("hash", flipped) }),
		"no hash":      mutate(func(c url.Values) { c.Del("hash") }),
		"empty hash":   mutate(func(c url.Values) { c.Set("hash", "") }),
		"non-hex hash": mutate(func(c url.Values) { c.Set("hash", strings.Repeat("zz", 32)) }),
		"short hash":   mutate(func(c url.Values) { c.Set("hash", hash[:10]) }),
		"changed user": mutate(func(c url.Values) { c.Set("user", strings.Replace(userAnn, "42", "43", 1)) }),
		"other token":  signFields("999:other", fields(testNow, userAnn)),
	}
	for name, raw := range cases {
		if _, err := verifyInitData(raw, testToken, testNow, time.Hour); !errors.Is(err, errInitBadSignature) {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestVerifyInitDataDuplicateKey(t *testing.T) {
	raw := signFields(testToken, fields(testNow, userAnn)) + "&auth_date=1"
	if _, err := verifyInitData(raw, testToken, testNow, time.Hour); !errors.Is(err, errInitBadRequest) {
		t.Fatalf("got %v", err)
	}
	raw = signFields(testToken, fields(testNow, userAnn))
	raw += "&hash=" + strings.Repeat("0", 64)
	if _, err := verifyInitData(raw, testToken, testNow, time.Hour); !errors.Is(err, errInitBadRequest) {
		t.Fatalf("hash twice: got %v", err)
	}
}

func TestVerifyInitDataSignatureFieldCounts(t *testing.T) {
	f := fields(testNow, userAnn)
	f["signature"] = "dGVzdC1zaWduYXR1cmU"
	raw := signFields(testToken, f)
	if _, err := verifyInitData(raw, testToken, testNow, time.Hour); err != nil {
		t.Fatalf("signature in check string: %v", err)
	}
	// A hash made without the signature field must fail.
	without := signFields(testToken, fields(testNow, userAnn))
	wv, _ := url.ParseQuery(without)
	v, _ := url.ParseQuery(raw)
	v.Set("hash", wv.Get("hash"))
	if _, err := verifyInitData(v.Encode(), testToken, testNow, time.Hour); !errors.Is(err, errInitBadSignature) {
		t.Fatalf("hash without signature field: got %v", err)
	}
}

func TestVerifyInitDataBadUser(t *testing.T) {
	for _, u := range []string{
		`{"id":0}`, `{"id":-5}`, `{"id":"abc"}`, `{"id":1.5}`, `{"id":"42"}`,
		`{}`, `not json`, ``,
	} {
		raw := signFields(testToken, fields(testNow, u))
		if _, err := verifyInitData(raw, testToken, testNow, time.Hour); !errors.Is(err, errInitBadUser) {
			t.Errorf("user %q: got %v", u, err)
		}
	}
	f := fields(testNow, userAnn)
	delete(f, "user")
	if _, err := verifyInitData(signFields(testToken, f), testToken, testNow, time.Hour); !errors.Is(err, errInitBadUser) {
		t.Errorf("no user: got %v", err)
	}
}

func TestVerifyInitDataBadAuthDate(t *testing.T) {
	for _, d := range []string{"", "abc", "-1"} {
		f := fields(testNow, userAnn)
		f["auth_date"] = d
		_, err := verifyInitData(signFields(testToken, f), testToken, testNow, time.Hour)
		if !errors.Is(err, errInitBadRequest) {
			t.Errorf("auth_date %q: got %v", d, err)
		}
	}
	f := fields(testNow, userAnn)
	delete(f, "auth_date")
	if _, err := verifyInitData(signFields(testToken, f), testToken, testNow, time.Hour); !errors.Is(err, errInitBadRequest) {
		t.Errorf("no auth_date: got %v", err)
	}
}

func TestVerifyInitDataBadRequest(t *testing.T) {
	big := "a=" + strings.Repeat("x", 4097)
	exact := "a=" + strings.Repeat("x", 4094)
	cases := map[string]string{
		"empty":      "",
		"too large":  big,
		"unparsable": "a=%zz&hash=00",
	}
	for name, raw := range cases {
		if _, err := verifyInitData(raw, testToken, testNow, time.Hour); !errors.Is(err, errInitBadRequest) {
			t.Errorf("%s: got %v", name, err)
		}
	}
	// At exactly 4 KiB the size check passes; the signature check then fails.
	if len(exact) != 4096 {
		t.Fatal("test setup: size")
	}
	if _, err := verifyInitData(exact, testToken, testNow, time.Hour); !errors.Is(err, errInitBadSignature) {
		t.Errorf("4 KiB: got %v", err)
	}
}

func TestVerifyInitDataErrorsHideInput(t *testing.T) {
	raw := signFields(testToken, fields(testNow, `{"id":0}`))
	_, err := verifyInitData(raw, testToken, testNow, time.Hour)
	if err == nil || strings.Contains(err.Error(), "query_id") || strings.Contains(err.Error(), testToken) {
		t.Fatalf("error leaks input: %v", err)
	}
}

// The vector below does not use signFields. It was computed with a separate
// Python script (hmac and hashlib):
//
//	secret = hmac.new(b"WebAppData", b"123456:TEST-token", sha256).digest()
//	want   = hmac.new(secret, dcs.encode(), sha256).hexdigest()
//
// where dcs is the three lines below, joined with LF, with no final LF.
func TestVerifyInitDataIndependentVector(t *testing.T) {
	const hash = "52407c82c620cc43263aa26aaad1a36a526424d10a760a4adb02c8b5caefe70d"
	v := url.Values{}
	v.Set("auth_date", "1700000000")
	v.Set("query_id", "AAHdF6IQAAAAAN0XohDhrOrc")
	v.Set("user", `{"id":42,"first_name":"Ann"}`)
	v.Set("hash", hash)
	now := time.Unix(1700000000, 0).Add(time.Minute)
	got, err := verifyInitData(v.Encode(), testToken, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if got.UserID != 42 || got.FirstName != "Ann" {
		t.Fatalf("result %+v", got)
	}
}

// TestVerifyInitDataReal checks one real initData of the development bot.
// It runs only when both variables are set. It never prints them.
func TestVerifyInitDataReal(t *testing.T) {
	raw := os.Getenv("TODO_REAL_INITDATA")
	token := os.Getenv("TODO_REAL_BOT_TOKEN")
	if raw == "" || token == "" {
		t.Skip("TODO_REAL_INITDATA and TODO_REAL_BOT_TOKEN are not set")
	}
	v, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal("TODO_REAL_INITDATA does not parse")
	}
	sec, err := strconv.ParseInt(v.Get("auth_date"), 10, 64)
	if err != nil {
		t.Fatal("auth_date is not a number")
	}
	// Use the time of auth_date, so that an old sample still passes the age check.
	got, err := verifyInitData(raw, token, time.Unix(sec, 0), time.Hour)
	if err != nil {
		t.Fatalf("real initData failed: %v", err)
	}
	if got.UserID <= 0 {
		t.Fatal("no user id")
	}
	t.Log("real initData verified")
}
