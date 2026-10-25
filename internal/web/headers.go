package web

import "net/http"

// Content Security Policies (docs/WEB-DESIGN.md §11.1, §11.2). Both pages
// load the Vazirmatn font from Google Fonts: the stylesheet from fontsCSS and
// the font files from fontsFiles.
const (
	fontsCSS   = "https://fonts.googleapis.com"
	fontsFiles = "https://fonts.gstatic.com"

	// The Telegram Login Widget (§3.4) on the login screen of "/": the
	// script comes from widgetScript, and it puts an iframe of widgetFrame on
	// the page. Only cspPWA allows them, not the Mini App.
	widgetScript = "https://telegram.org"
	widgetFrame  = "https://oauth.telegram.org"

	// cspPWA is the policy of "/" and of the other static files.
	cspPWA = "default-src 'none'; script-src 'self' " + widgetScript + "; style-src 'self' " + fontsCSS + "; " +
		"img-src 'self' data:; connect-src 'self'; manifest-src 'self'; worker-src 'self'; " +
		"frame-src " + widgetFrame + "; font-src 'self' " + fontsFiles + "; " +
		"base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
	// cspMiniApp is the policy of "/tg/". Telegram Web frames the Mini App
	// from web.telegram.org (the K and A versions both use this host).
	cspMiniApp = "default-src 'none'; script-src 'self' https://telegram.org; style-src 'self' " + fontsCSS + "; " +
		"img-src 'self' data:; connect-src 'self'; font-src 'self' " + fontsFiles + "; base-uri 'none'; " +
		"form-action 'self'; frame-ancestors https://web.telegram.org"
	// cspAPI is the policy of every response that is not a static file.
	cspAPI = "default-src 'none'; frame-ancestors 'none'"
)

// Cache-Control values (§5.1, §7.5).
const (
	cacheNoStore   = "no-store"
	cacheNoCache   = "no-cache"
	cacheImmutable = "public, max-age=31536000, immutable"
	cacheIcons     = "public, max-age=86400"
	cacheI18n      = "public, max-age=300"
)

// setCommonHeaders sets the headers of §11.1 that every response gets. The
// CSP and the Cache-Control are those of the API; the static handler
// replaces them for its files.
func setCommonHeaders(h http.Header) {
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
	h.Set("Strict-Transport-Security", "max-age=31536000")
	h.Set("Content-Security-Policy", cspAPI)
	h.Set("Cache-Control", cacheNoStore)
}

// setPageHeaders sets the headers of an HTML page: the PWA ("/") or the
// Mini App ("/tg/"). Only the PWA gets Cross-Origin-Opener-Policy, because
// Telegram Web opens the Mini App from its own window.
func setPageHeaders(h http.Header, miniApp bool) {
	h.Set("Cache-Control", cacheNoCache)
	if miniApp {
		h.Set("Content-Security-Policy", cspMiniApp)
		return
	}
	h.Set("Content-Security-Policy", cspPWA)
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
}
