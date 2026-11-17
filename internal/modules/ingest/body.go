package ingest

import (
	"html"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Bengo-Hub/httpware/richtext"
	"github.com/google/uuid"

	"github.com/bengobox/ticketing-service/internal/modules/tickets"
)

// Ticket field limits (the ticket schema bounds the subject at 255 bytes).
const (
	maxSubjectBytes = 250
	maxBodyBytes    = 60 << 10
)

// messageBody is what a message stores: its plain text as paragraphs (the reply part only when it
// continues a ticket), or its sanitised HTML when it has no text part. The tickets service
// sanitises again before storing.
func messageBody(m Message, reply bool) string {
	text := strings.TrimSpace(m.Text)
	if reply {
		text = stripQuoted(text)
	}
	body := ""
	switch {
	case text != "":
		body = textToHTML(clip(text, maxBodyBytes))
	case strings.TrimSpace(m.HTML) != "":
		body = richtext.Sanitize(m.HTML)
		if body == "" {
			body = textToHTML(clip(richtext.PlainTextBlocks(m.HTML), maxBodyBytes))
		}
	}
	if strings.TrimSpace(richtext.PlainText(body)) != "" {
		return body
	}
	if len(m.Attachments) > 0 {
		return "<p>(attachment only)</p>"
	}
	return "<p>(no message text)</p>"
}

// ticketSubject is the email subject without reply prefixes and tags, or for WhatsApp the first
// line of the message.
func ticketSubject(m Message) string {
	s := strings.TrimSpace(subjectTag.ReplaceAllString(m.Subject, ""))
	for {
		lower := strings.ToLower(s)
		trimmed := false
		for _, p := range []string{"re:", "fw:", "fwd:", "aw:"} {
			if strings.HasPrefix(lower, p) {
				s, trimmed = strings.TrimSpace(s[len(p):]), true
				break
			}
		}
		if !trimmed {
			break
		}
	}
	if s == "" && m.Channel == tickets.ChannelWhatsApp {
		if first, _, _ := strings.Cut(strings.TrimSpace(m.Text), "\n"); first != "" {
			s = clip(first, 80)
		} else {
			who := m.Requester.Name
			if who == "" {
				who = m.Requester.Phone
			}
			s = "WhatsApp message from " + who
		}
	}
	if s == "" {
		s = "(no subject)"
	}
	return clip(s, maxSubjectBytes)
}

// textToHTML escapes plain text and keeps its layout: blank lines separate paragraphs, single
// line breaks stay breaks.
func textToHTML(text string) string {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	var b strings.Builder
	for _, para := range strings.Split(text, "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		lines := strings.Split(para, "\n")
		for i := range lines {
			lines[i] = html.EscapeString(strings.TrimRight(lines[i], " \t"))
		}
		b.WriteString("<p>" + strings.Join(lines, "<br>") + "</p>")
	}
	return b.String()
}

var (
	// "On Mon, 17 Nov 2026 at 10:00, Jane <jane@x.com> wrote:" may wrap onto a second line.
	wroteLine     = regexp.MustCompile(`(?i)^\s*(on\s.+|.+\s)wrote:\s*$`)
	onLineStart   = regexp.MustCompile(`(?i)^\s*on\s`)
	separatorLine = regexp.MustCompile(`(?i)^\s*(-{2,}\s*original message\s*-{2,}|_{10,}|from:\s.+)$`)
)

// stripQuoted keeps the new part of a reply: everything before the quoted history ("On ... wrote:",
// an Outlook "Original Message" or From: block, or ">" quoted lines). When the whole message is
// quoted it is kept as it is.
func stripQuoted(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	cut := len(lines)
	for i, l := range lines {
		joined := l
		if i+1 < len(lines) && onLineStart.MatchString(l) {
			joined = l + " " + lines[i+1]
		}
		if wroteLine.MatchString(l) || (onLineStart.MatchString(l) && wroteLine.MatchString(joined)) || separatorLine.MatchString(l) {
			cut = i
			break
		}
	}
	kept := make([]string, 0, cut)
	for _, l := range lines[:cut] {
		if strings.HasPrefix(strings.TrimSpace(l), ">") {
			continue
		}
		kept = append(kept, l)
	}
	out := strings.TrimSpace(strings.Join(kept, "\n"))
	if out == "" {
		return strings.TrimSpace(text)
	}
	return out
}

// clip shortens s to at most n bytes on a rune boundary.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return strings.TrimSpace(s)
}

var subjectTag = regexp.MustCompile(`(?i)\[\s*TKT-(\d{1,12})\s*\]`)

// SubjectTicketNumber reads a [TKT-n] tag from a subject as a ticket number (TKT-000042).
func SubjectTicketNumber(subject string) string {
	m := subjectTag.FindStringSubmatch(subject)
	if m == nil {
		return ""
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || n <= 0 {
		return ""
	}
	return tickets.FormatTicketNumber(n)
}

var messageIDPattern = regexp.MustCompile(`<([^<>\s]{1,500})>`)

// ParseMessageIDs reads Message-IDs from header values, in order, without angle brackets. A
// value with no brackets counts as one bare id.
func ParseMessageIDs(values ...string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" || len(id) > 500 || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	for _, v := range values {
		found := messageIDPattern.FindAllStringSubmatch(v, -1)
		if len(found) == 0 {
			if f := strings.Fields(v); len(f) == 1 {
				add(f[0])
			}
			continue
		}
		for _, m := range found {
			add(m[1])
		}
	}
	return out
}

// OwnToken reads the ticket or comment id from a Message-ID ticketing gave notifications-api to
// send with: its local part is that id (email_message_token on the event).
func OwnToken(messageID string) (uuid.UUID, bool) {
	local, _, ok := strings.Cut(strings.Trim(strings.TrimSpace(messageID), "<>"), "@")
	if !ok {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(local)
	return id, err == nil && id != uuid.Nil
}
