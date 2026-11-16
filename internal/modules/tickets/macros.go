package tickets

import (
	"context"
	"strings"
	"time"

	"github.com/Bengo-Hub/httpware/richtext"
	"github.com/google/uuid"
)

// MacroActions are the field changes a macro makes. Every field is optional.
type MacroActions struct {
	Status      *TicketStatus   `json:"status,omitempty"`
	Priority    *TicketPriority `json:"priority,omitempty"`
	CategoryID  *uuid.UUID      `json:"category_id,omitempty"`
	TeamID      *uuid.UUID      `json:"team_id,omitempty"`
	AgentID     *uuid.UUID      `json:"agent_id,omitempty"`
	AddTags     []string        `json:"add_tags,omitempty"`
	CommentType CommentType     `json:"comment_type,omitempty"`
}

// Macro is a canned reply with field changes.
type Macro struct {
	ID        uuid.UUID    `json:"id"`
	TenantID  uuid.UUID    `json:"-"`
	Name      string       `json:"name"`
	Body      string       `json:"body"`
	Actions   MacroActions `json:"actions"`
	IsActive  bool         `json:"is_active"`
	CreatedBy uuid.UUID    `json:"created_by"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

// MacroInput is the body of POST /macros and PUT /macros/{id} (a PUT replaces the macro).
type MacroInput struct {
	Name     string       `json:"name"`
	Body     string       `json:"body"`
	Actions  MacroActions `json:"actions"`
	IsActive *bool        `json:"is_active,omitempty"`
}

// MacroApplied is what POST /tickets/{id}/macros/{macroId}/apply answers.
type MacroApplied struct {
	Ticket  *Ticket        `json:"ticket"`
	Comment *TicketComment `json:"comment,omitempty"`
}

// maxMacros bounds GET /macros.
const maxMacros = 200

// Validate checks a macro's name, body and actions.
func (in *MacroInput) Validate() error {
	in.Name = strings.TrimSpace(in.Name)
	a := in.Actions
	switch {
	case in.Name == "" || len([]rune(in.Name)) > 100:
		return invalid("name is required (at most 100 characters)")
	case a.Status != nil && !validStatus(*a.Status):
		return invalid("actions.status is not a ticket status")
	case a.Priority != nil && !validPriority(*a.Priority):
		return invalid("actions.priority is not a ticket priority")
	case a.CommentType != "" && a.CommentType != CommentPublic && a.CommentType != CommentInternal:
		return invalid("actions.comment_type must be public or internal")
	case strings.TrimSpace(in.Body) == "" && a.empty():
		return invalid("a macro needs a body or at least one action")
	}
	return nil
}

// empty reports whether the actions change nothing.
func (a MacroActions) empty() bool {
	return a.Status == nil && a.Priority == nil && a.CategoryID == nil && a.TeamID == nil &&
		a.AgentID == nil && len(NormalizeTags(a.AddTags)) == 0
}

// changesFor is what the macro changes on t.
func (a MacroActions) changesFor(t *Ticket) TicketChanges {
	c := TicketChanges{Status: a.Status, CategoryID: a.CategoryID}
	if a.Priority != nil && *a.Priority != t.Priority {
		c.Priority = a.Priority
	}
	if a.AgentID != nil || a.TeamID != nil {
		c.Assign, c.AgentID, c.TeamID = true, t.AssignedAgentID, t.AssignedTeamID
		if a.AgentID != nil {
			c.AgentID = a.AgentID
		}
		if a.TeamID != nil {
			c.TeamID = a.TeamID
		}
	}
	if add := NormalizeTags(a.AddTags); len(add) > 0 {
		c.Tags = MergeTags(t.Tags, add)
	}
	return c
}

// RenderMacroBody fills the placeholders a macro body may use: {{ticket_number}}, {{subject}}
// and {{requester_name}} (blank when unknown).
func RenderMacroBody(body string, t *Ticket) string {
	return strings.NewReplacer(
		"{{ticket_number}}", t.TicketNumber,
		"{{subject}}", t.Subject,
		"{{requester_name}}", RequesterOf(t).Name,
	).Replace(body)
}

// CreateMacro stores a macro.
func (s *Service) CreateMacro(ctx context.Context, tenantID, actorID uuid.UUID, in MacroInput) (*Macro, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	m := &Macro{ID: uuid.New(), TenantID: tenantID, Name: in.Name, Body: richtext.Sanitize(in.Body), Actions: in.Actions,
		IsActive: in.IsActive == nil || *in.IsActive, CreatedBy: actorID}
	m.Actions.AddTags = NormalizeTags(m.Actions.AddTags)
	if err := s.repo.CreateMacro(ctx, tenantID, m); err != nil {
		return nil, err
	}
	return m, nil
}

// UpdateMacro replaces a macro's name, body, actions and active flag.
func (s *Service) UpdateMacro(ctx context.Context, tenantID, macroID uuid.UUID, in MacroInput) (*Macro, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	m, err := s.repo.GetMacro(ctx, tenantID, macroID)
	if err != nil {
		return nil, err
	}
	m.Name, m.Body, m.Actions = in.Name, richtext.Sanitize(in.Body), in.Actions
	m.Actions.AddTags = NormalizeTags(m.Actions.AddTags)
	if in.IsActive != nil {
		m.IsActive = *in.IsActive
	}
	if err := s.repo.UpdateMacro(ctx, tenantID, m); err != nil {
		return nil, err
	}
	return m, nil
}

// GetMacro, ListMacros and DeleteMacro read and remove macros of the tenant.
func (s *Service) GetMacro(ctx context.Context, tenantID, macroID uuid.UUID) (*Macro, error) {
	return s.repo.GetMacro(ctx, tenantID, macroID)
}

func (s *Service) ListMacros(ctx context.Context, tenantID uuid.UUID, activeOnly bool) ([]*Macro, error) {
	return s.repo.ListMacros(ctx, tenantID, activeOnly, maxMacros)
}

func (s *Service) DeleteMacro(ctx context.Context, tenantID, macroID uuid.UUID) error {
	return s.repo.DeleteMacro(ctx, tenantID, macroID)
}

// ApplyMacro applies a macro to a ticket in one transaction: its field changes (with the same
// events as single changes), then its body as a reply from the agent, public unless the macro
// says internal.
func (s *Service) ApplyMacro(ctx context.Context, tenantID, ticketID, macroID, actorID uuid.UUID) (*MacroApplied, error) {
	m, err := s.repo.GetMacro(ctx, tenantID, macroID)
	if err != nil {
		return nil, err
	}
	if !m.IsActive {
		return nil, ErrMacroNotFound
	}
	res := &MacroApplied{}
	err = s.repo.InTx(ctx, tenantID, func(repo Repository, out *Outbox) error {
		t, err := repo.GetTicket(ctx, tenantID, ticketID)
		if err != nil {
			return err
		}
		c := m.Actions.changesFor(t)
		if err := c.Check(t); err != nil {
			return err
		}
		if err := s.applyChangesTx(ctx, repo, out, t, c); err != nil {
			return err
		}
		if body := strings.TrimSpace(RenderMacroBody(m.Body, t)); body != "" {
			commentType := m.Actions.CommentType
			if commentType == "" {
				commentType = CommentPublic
			}
			res.Comment, err = s.commentTx(ctx, repo, out, &AddCommentRequest{
				TenantID: tenantID, TicketID: ticketID, AuthorID: actorID, AuthorType: AuthorAgent,
				CommentType: commentType, Content: body, Metadata: map[string]any{"macro_id": m.ID.String()},
			})
			if err != nil {
				return err
			}
		}
		res.Ticket, err = repo.GetTicket(ctx, tenantID, ticketID)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.broadcast(tenantID, "ticket.updated", map[string]any{"ticket_id": ticketID})
	if res.Comment != nil {
		s.broadcast(tenantID, "comment.created", map[string]any{"ticket_id": ticketID, "comment_id": res.Comment.ID})
	}
	return res, nil
}
