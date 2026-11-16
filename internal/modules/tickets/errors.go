package tickets

import "errors"

var (
	// Ticket errors
	ErrTicketNotFound      = errors.New("tickets: ticket not found")
	ErrTicketAlreadyClosed = errors.New("tickets: ticket is already closed")
	ErrTicketCancelled     = errors.New("tickets: ticket is cancelled")
	ErrInvalidTransition   = errors.New("tickets: invalid status transition")
	// ErrDuplicateSourceRef means another create for the same internal source record won the race.
	ErrDuplicateSourceRef = errors.New("tickets: a ticket already exists for this source record")

	// Comment errors
	ErrCommentNotFound = errors.New("tickets: comment not found")

	// Attachment errors
	ErrAttachmentNotFound = errors.New("tickets: attachment not found")
	ErrFileTooLarge       = errors.New("tickets: file size exceeds maximum allowed")

	// Category errors
	ErrCategoryNotFound    = errors.New("tickets: category not found")
	ErrCategoryExists      = errors.New("tickets: category already exists")
	ErrCategoryHasChildren = errors.New("tickets: category has child categories")
	ErrCategoryHasTickets  = errors.New("tickets: category has associated tickets")

	// Assignment errors
	ErrAgentNotAvailable = errors.New("tickets: agent is not available")
	ErrNoAgentAssigned   = errors.New("tickets: no agent assigned to ticket")
)
