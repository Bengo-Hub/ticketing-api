package tickets

import (
	"context"
	"time"

	"github.com/bengobox/ticketing-service/internal/modules/sla/clock"
)

// SLAClock is the clock a ticket's SLA counts in. A nil Calendar counts every minute (24x7).
// pending_customer always stops the clock; pending_internal stops it when the policy says so.
type SLAClock struct {
	Calendar               *clock.Calendar
	PauseOnPendingInternal bool
}

// stops reports whether a ticket in status has its SLA clock stopped.
func (c SLAClock) stops(status TicketStatus) bool {
	return status == StatusPendingCustomer || (status == StatusPendingInternal && c.PauseOnPendingInternal)
}

// SLAUpdate is a change to a ticket's SLA columns; nil fields are left alone.
type SLAUpdate struct {
	ResponseDueAt        *time.Time
	ResolutionDueAt      *time.Time
	PausedAt             *time.Time
	Resume               bool // clears paused_at
	PausedMinutes        *int
	FirstResponseMinutes *int
	ResolutionMinutes    *int
}

func (u SLAUpdate) empty() bool {
	return u.ResponseDueAt == nil && u.ResolutionDueAt == nil && u.PausedAt == nil && !u.Resume &&
		u.PausedMinutes == nil && u.FirstResponseMinutes == nil && u.ResolutionMinutes == nil
}

// hasSLA reports whether a policy set deadlines on the ticket.
func hasSLA(t *Ticket) bool {
	return t.SLAResponseDueAt != nil || t.SLAResolutionDueAt != nil
}

func isActive(s TicketStatus) bool {
	return s != StatusResolved && s != StatusClosed && s != StatusCancelled
}

// pauseTransition stops or restarts the SLA clock for a move to status at the instant at, and
// applies the change to t. Stopping records paused_at. Restarting measures the pause in the
// policy's clock, moves each deadline not yet met by that many minutes, and adds them to the
// paused total. A ticket that leaves a pending status for resolved or closed restarts first, so
// its breach check and elapsed minutes see the moved deadline.
func pauseTransition(t *Ticket, to TicketStatus, c SLAClock, at time.Time) SLAUpdate {
	var u SLAUpdate
	if !hasSLA(t) {
		return u
	}
	stop := isActive(to) && c.stops(to)
	switch {
	case t.SLAPausedAt != nil && !stop:
		paused := c.Calendar.Minutes(*t.SLAPausedAt, at)
		if t.FirstResponseAt == nil && t.SLAResponseDueAt != nil {
			due := c.Calendar.Add(*t.SLAResponseDueAt, paused)
			t.SLAResponseDueAt, u.ResponseDueAt = &due, &due
		}
		if t.SLAResolutionDueAt != nil {
			due := c.Calendar.Add(*t.SLAResolutionDueAt, paused)
			t.SLAResolutionDueAt, u.ResolutionDueAt = &due, &due
		}
		t.SLAPausedMinutes += paused
		total := t.SLAPausedMinutes
		u.PausedMinutes = &total
		t.SLAPausedAt, u.Resume = nil, true
	case t.SLAPausedAt == nil && stop:
		start := at
		t.SLAPausedAt, u.PausedAt = &start, &start
	}
	return u
}

// netMinutes is the SLA time a ticket has used by at: the minutes since creation in the policy's
// clock, less every pause, including one still open.
func netMinutes(c SLAClock, t *Ticket, at time.Time) int {
	n := c.Calendar.Minutes(t.CreatedAt, at) - t.SLAPausedMinutes
	if t.SLAPausedAt != nil {
		n -= c.Calendar.Minutes(*t.SLAPausedAt, at)
	}
	if n < 0 {
		return 0
	}
	return n
}

// effectiveDue is a deadline as it will stand when an open pause ends at at.
func effectiveDue(c SLAClock, t *Ticket, due time.Time, at time.Time) time.Time {
	if t.SLAPausedAt == nil {
		return due
	}
	return c.Calendar.Add(due, c.Calendar.Minutes(*t.SLAPausedAt, at))
}

// slaClock loads the clock of the ticket's policy; without the SLA module every minute counts.
func (s *Service) slaClock(ctx context.Context, t *Ticket) (SLAClock, error) {
	if s.sla == nil {
		return SLAClock{}, nil
	}
	return s.sla.PolicyClock(ctx, t.TenantID, t.SLAPolicyID)
}

// slaStatusTx applies the SLA side of a status change inside the change's transaction: the pause
// or restart, and the elapsed resolution minutes when the ticket resolves. t is updated in place.
func (s *Service) slaStatusTx(ctx context.Context, repo Repository, t *Ticket, to TicketStatus, at time.Time) error {
	if !hasSLA(t) {
		return nil
	}
	c, err := s.slaClock(ctx, t)
	if err != nil {
		return err
	}
	u := pauseTransition(t, to, c, at)
	if to == StatusResolved {
		m := netMinutes(c, t, at)
		t.SLAResolutionMinutes, u.ResolutionMinutes = &m, &m
	}
	if u.empty() {
		return nil
	}
	return repo.UpdateSLA(ctx, t.TenantID, t.ID, u)
}

// slaFirstResponseTx records the elapsed first-response minutes and reports whether the reply
// came after the response deadline (moved by any open pause).
func (s *Service) slaFirstResponseTx(ctx context.Context, repo Repository, t *Ticket, at time.Time) (late bool, err error) {
	if t.FirstResponseAt != nil || !hasSLA(t) {
		return false, nil
	}
	c, err := s.slaClock(ctx, t)
	if err != nil {
		return false, err
	}
	m := netMinutes(c, t, at)
	if err := repo.UpdateSLA(ctx, t.TenantID, t.ID, SLAUpdate{FirstResponseMinutes: &m}); err != nil {
		return false, err
	}
	if t.SLAResponseDueAt == nil || t.SLAResponseBreached {
		return false, nil
	}
	return at.After(effectiveDue(c, t, *t.SLAResponseDueAt, at)), nil
}
