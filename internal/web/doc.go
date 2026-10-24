// Package web holds the web UI of the bot: the HTTP server, the JSON API and
// the embedded front end. The design is in docs/WEB-DESIGN.md.
//
// The front end is built from the web/ directory into internal/web/dist. A
// build with the webui tag embeds that directory. A build without the tag
// embeds a small placeholder page, so that "go build" needs no Node.
package web
