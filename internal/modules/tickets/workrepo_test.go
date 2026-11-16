package tickets

import (
	"context"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// workRepo extends fakeRepo with what the sprint 9 paths reach: field updates, assignments,
// comments, thread moves, macros and teams. Its InTx hands itself to fn and keeps the events
// only when fn succeeds.
type workRepo struct {
	*fakeRepo
	events   []OutboxEvent
	comments []*TicketComment
	moved    [][2]uuid.UUID
	macros   map[uuid.UUID]*Macro
	teams    map[uuid.UUID]bool
}

func newWorkRepo() *workRepo {
	return &workRepo{fakeRepo: newFakeRepo(), macros: map[uuid.UUID]*Macro{}, teams: map[uuid.UUID]bool{}}
}

func newWorkService(r *workRepo) *Service { return NewService(r, zap.NewNop()) }

func (r *workRepo) InTx(_ context.Context, _ uuid.UUID, fn func(Repository, *Outbox) error) error {
	out := &Outbox{}
	if err := fn(r, out); err != nil {
		return err
	}
	r.events = append(r.events, out.Events...)
	return nil
}

func (r *workRepo) eventTypes() []string {
	out := make([]string, len(r.events))
	for i, e := range r.events {
		out[i] = e.Type
	}
	return out
}

// addTicket stores a ticket of the given status and returns it.
func (r *workRepo) addTicket(number string, status TicketStatus) *Ticket {
	t := &Ticket{ID: uuid.New(), TenantID: uuid.Nil, TicketNumber: number, Subject: "Subject " + number,
		Status: status, Priority: PriorityNormal, Metadata: map[string]any{}}
	r.tickets[t.ID] = t
	return t
}

func (r *workRepo) UpdateTicket(_ context.Context, _ uuid.UUID, id uuid.UUID, u *UpdateTicketRequest) error {
	t, ok := r.tickets[id]
	if !ok {
		return ErrTicketNotFound
	}
	if u.Priority != nil {
		t.Priority = *u.Priority
	}
	if u.CategoryID != nil {
		t.CategoryID = u.CategoryID
	}
	if u.Tags != nil {
		t.Tags = u.Tags
	}
	if u.Metadata != nil {
		t.Metadata = u.Metadata
	}
	return nil
}

func (r *workRepo) AssignTicket(_ context.Context, _ uuid.UUID, id uuid.UUID, agentID, teamID *uuid.UUID) error {
	t, ok := r.tickets[id]
	if !ok {
		return ErrTicketNotFound
	}
	t.AssignedAgentID, t.AssignedTeamID = agentID, teamID
	return nil
}

func (r *workRepo) CreateComment(_ context.Context, _ uuid.UUID, c *TicketComment) error {
	if _, ok := r.tickets[c.TicketID]; !ok {
		return ErrTicketNotFound
	}
	r.comments = append(r.comments, c)
	return nil
}

func (r *workRepo) MoveThread(_ context.Context, _ uuid.UUID, from, to uuid.UUID) error {
	r.moved = append(r.moved, [2]uuid.UUID{from, to})
	return nil
}

func (r *workRepo) AgentEmails(_ context.Context, _ uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	for _, id := range ids {
		if email, ok := r.agents[id]; ok {
			out[id] = email
		}
	}
	return out, nil
}

func (r *workRepo) TeamExists(_ context.Context, _ uuid.UUID, id uuid.UUID) (bool, error) {
	return r.teams[id], nil
}

func (r *workRepo) GetMacro(_ context.Context, _ uuid.UUID, id uuid.UUID) (*Macro, error) {
	m, ok := r.macros[id]
	if !ok {
		return nil, ErrMacroNotFound
	}
	return m, nil
}
