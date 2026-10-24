package model

import "time"

// Web UI types (#102, docs/WEB-DESIGN.md §6.2).

// WebSessionKind says how a web session signed in.
type WebSessionKind string

const (
	// WebMiniApp is a session of the Telegram Mini App (initData). Its
	// expiry does not slide.
	WebMiniApp WebSessionKind = "miniapp"
	// WebBrowser is a session of a browser or PWA (login code or Login
	// Widget). Its expiry slides, but never passes MaxAt.
	WebBrowser WebSessionKind = "browser"
)

// WebSession is a row of web_sessions. ID is the hex SHA-256 of the token;
// the token itself is never stored.
type WebSession struct {
	ID        string
	UserID    int64
	Kind      WebSessionKind
	Device    string
	CreatedAt time.Time
	SeenAt    time.Time
	ExpiresAt time.Time
	MaxAt     time.Time
}

// LoginCode is a row of web_login_codes. CodeHash is the hex SHA-256 of the
// normalised code; the code itself is never stored.
type LoginCode struct {
	CodeHash  string
	UserID    int64
	CreatedAt time.Time
	ExpiresAt time.Time
}

// MemberStatus is what the web knows about a user in a group.
type MemberStatus string

const (
	// MemberYes: getChatMember said that the user is a member.
	MemberYes MemberStatus = "member"
	// MemberLeft: getChatMember said that the user is not a member, or
	// the bot is no longer in the group.
	MemberLeft MemberStatus = "left"
	// MemberUnknown: an update from the user was seen in the group. It is
	// a hint only and never grants access.
	MemberUnknown MemberStatus = "unknown"
)

// ChatMember is a row of chat_members. SeenAt is zero if no update from the
// user was seen in the chat; CheckedAt is zero if never checked.
type ChatMember struct {
	ChatID    int64
	UserID    int64
	Status    MemberStatus
	SeenAt    time.Time
	CheckedAt time.Time
}
