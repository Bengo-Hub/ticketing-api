package ai

import (
	"encoding/json"
	"html"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/bengobox/ticketing-service/internal/modules/kb"
	"github.com/bengobox/ticketing-service/internal/modules/tickets"
)

// Length caps for what goes to marketflow-ai, in characters. A thread keeps its newest messages.
const (
	maxSubjectChars   = 300
	maxBodyChars      = 4000
	maxCommentChars   = 1500
	maxThreadChars    = 12000
	maxThreadMessages = 40
	maxArticleChars   = 600
	maxCandidates     = 20
	maxSuggestions    = 5
)

var (
	blockTags  = regexp.MustCompile(`(?i)<\s*(br|/p|/div|/li|/h[1-6]|/tr)\s*/?>`)
	anyTag     = regexp.MustCompile(`<[^>]*>`)
	spaceRun   = regexp.MustCompile(`[ \t\f\v\r]+`)
	blankLines = regexp.MustCompile(`\n{3,}`)
)

// plainText turns stored ticket or article HTML into plain text capped at max characters: tags
// removed (block ends become line breaks), entities decoded, whitespace folded.
func plainText(s string, max int) string {
	s = blockTags.ReplaceAllString(s, "\n")
	s = anyTag.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(spaceRun.ReplaceAllString(l, " "))
	}
	s = blankLines.ReplaceAllString(strings.Join(lines, "\n"), "\n\n")
	return capChars(strings.TrimSpace(s), max)
}

// capChars cuts s to at most max characters on a rune boundary.
func capChars(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:max])) + "…"
}

// message is one thread entry as the model reads it.
type message struct {
	From     string `json:"from"`
	Internal bool   `json:"internal,omitempty"`
	At       string `json:"at"`
	Text     string `json:"text"`
}

// ticketData is the ticket as the model reads it: no ids beyond the number, no requester contact.
func ticketData(t *tickets.Ticket) map[string]any {
	return map[string]any{
		"ticket_number": t.TicketNumber,
		"subject":       plainText(t.Subject, maxSubjectChars),
		"description":   plainText(t.Description, maxBodyChars),
		"status":        string(t.Status),
		"priority":      string(t.Priority),
		"channel":       string(t.Channel),
	}
}

// threadData turns comments (oldest first) into messages, dropping system entries, and keeps the
// newest ones that fit maxThreadChars.
func threadData(comments []*tickets.TicketComment) []message {
	out := make([]message, 0, len(comments))
	for _, c := range comments {
		if c.CommentType == tickets.CommentSystem || c.AuthorType == tickets.AuthorSystem {
			continue
		}
		text := plainText(c.Content, maxCommentChars)
		if text == "" {
			continue
		}
		out = append(out, message{
			From: string(c.AuthorType), Internal: c.CommentType == tickets.CommentInternal,
			At: c.CreatedAt.UTC().Format("2006-01-02 15:04"), Text: text,
		})
	}
	total, start := 0, len(out)
	for start > 0 {
		n := utf8.RuneCountInString(out[start-1].Text)
		if total+n > maxThreadChars {
			break
		}
		total += n
		start--
	}
	return out[start:]
}

// articleData is one candidate article as the model reads it.
func articleData(a *kb.Article) map[string]any {
	body := a.Excerpt
	if strings.TrimSpace(body) == "" {
		body = a.Content
	}
	return map[string]any{"id": a.ID.String(), "title": plainText(a.Title, maxSubjectChars), "summary": plainText(body, maxArticleChars)}
}

// The task instructions. Ticket text reaches the model only as the run's data, which marketflow-ai
// fences and masks; the instructions never quote it.
const (
	summaryInstructions = `You help a support agent catch up on one ticket. Read the ticket and its messages in the data.
Write a short summary in plain text: what the customer needs, what has been tried or promised, and what is still open.
Use at most 6 short lines. Do not invent facts that are not in the data. Do not address the customer.`

	replyInstructions = `You draft a reply that a support agent will review and edit before sending it to the customer.
Read the ticket and its public messages in the data and write one reply to the customer's latest message.
Be polite, clear and brief. Use only facts found in the data. Never state a price, a discount, a refund, a deadline
or a promise that is not already in the data; if one is needed, leave a placeholder in square brackets for the agent.
Write only the reply text, in the language the customer wrote in, with no subject line and no signature.`

	kbRankInstructions = `You pick knowledge base articles that would help answer one support ticket.
The data has the ticket and a list of candidate articles, each with an id. Choose at most 5 that are relevant,
best first, and only from that list. Answer with JSON only, in this shape:
{"articles":[{"id":"<article id from the list>","reason":"<one short sentence>"}]}
Answer {"articles":[]} when none is relevant.`

	kbDraftInstructions = `You turn one resolved support ticket into a draft knowledge base article for the same business.
Read the ticket and its public messages in the data. Write a general how-to that would help the next customer with the
same problem. Leave out names, contact details, order numbers and anything about this one customer. Use only facts in the data.
Answer with JSON only, in this shape:
{"title":"<short title>","excerpt":"<one sentence>","steps":["<paragraph or step>", "..."]}`
)

// decodeJSONAnswer reads a JSON object from a model answer that may be wrapped in a code fence or
// surrounded by prose.
func decodeJSONAnswer(answer string, into any) error {
	s := strings.TrimSpace(answer)
	if i, j := strings.Index(s, "{"), strings.LastIndex(s, "}"); i >= 0 && j > i {
		s = s[i : j+1]
	}
	return json.Unmarshal([]byte(s), into)
}

// draftHTML renders the model's article steps as escaped paragraphs; the kb service sanitises
// the result again before it is stored.
func draftHTML(steps []string) string {
	var b strings.Builder
	for _, s := range steps {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		b.WriteString("<p>")
		b.WriteString(html.EscapeString(capChars(s, maxBodyChars)))
		b.WriteString("</p>")
	}
	return b.String()
}
