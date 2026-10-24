package tg

import "context"

// MemberStatus is the status field of a Telegram ChatMember.
type MemberStatus string

// Member statuses, as getChatMember reports them.
const (
	MemberCreator       MemberStatus = "creator"
	MemberAdministrator MemberStatus = "administrator"
	MemberMember        MemberStatus = "member"
	MemberRestricted    MemberStatus = "restricted"
	MemberLeft          MemberStatus = "left"
	MemberKicked        MemberStatus = "kicked"
)

// ChatMember is the part of a getChatMember answer that the web needs.
type ChatMember struct {
	Status   MemberStatus
	IsMember bool // only meaningful for "restricted"
}

// MemberChecker calls getChatMember. *tgbot.Bot implements it. Errors are
// *APIError where Telegram answered, with the code and the description,
// so that internal/web can classify them (docs/WEB-DESIGN.md §4.4).
type MemberChecker interface {
	ChatMember(ctx context.Context, chatID, userID int64) (ChatMember, error)
}
