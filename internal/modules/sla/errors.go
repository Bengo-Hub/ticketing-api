package sla

import "errors"

var (
	ErrPolicyNotFound   = errors.New("sla policy not found")
	ErrPolicyInUse      = errors.New("sla policy is referenced by active tickets and cannot be deleted")
	ErrInvalidPolicy    = errors.New("invalid sla policy")
	ErrCalendarNotFound = errors.New("business calendar not found")
	ErrCalendarInUse    = errors.New("business calendar is used by an sla policy")
	ErrCalendarExists   = errors.New("a business calendar with this name already exists")
	ErrInvalidCalendar  = errors.New("invalid business calendar")
	ErrInvalidReport    = errors.New("invalid sla report request")
)
