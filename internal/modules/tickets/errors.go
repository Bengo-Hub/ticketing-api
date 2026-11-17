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
	// ErrDuplicateMessage means a channel message (an email or WhatsApp message) is already a
	// comment; a redelivered message lands here.
	ErrDuplicateMessage = errors.New("tickets: this channel message is already on a ticket")

	// Comment errors
	ErrCommentNotFound = errors.New("tickets: comment not found")

	// Attachment errors
	ErrAttachmentNotFound = errors.New("tickets: attachment not found")

	// Category errors
	ErrCategoryNotFound    = errors.New("tickets: category not found")
	ErrCategoryExists      = errors.New("tickets: category already exists")
	ErrCategoryHasChildren = errors.New("tickets: category has child categories")
	ErrCategoryHasTickets  = errors.New("tickets: category has associated tickets")

	// Assignment errors
	ErrAgentNotAvailable = errors.New("tickets: agent is not available")
	ErrNoAgentAssigned   = errors.New("tickets: no agent assigned to ticket")
	// ErrAssigneeNotFound means the agent or team is not one of the tenant's.
	ErrAssigneeNotFound = errors.New("tickets: agent or team not found in this tenant")

	// Merge errors
	ErrMergeSelf         = errors.New("tickets: a ticket cannot be merged into itself")
	ErrAlreadyMerged     = errors.New("tickets: ticket was already merged into another ticket")
	ErrMergeTargetMerged = errors.New("tickets: the target ticket was itself merged into another ticket")
	ErrMergeTargetClosed = errors.New("tickets: cannot merge into a closed or cancelled ticket")
	ErrMergeSourceClosed = errors.New("tickets: a closed or cancelled ticket cannot be merged")

	// Link errors
	ErrLinkNotFound = errors.New("tickets: link not found")
	ErrLinkExists   = errors.New("tickets: these tickets are already linked this way")
	ErrLinkInvalid  = errors.New("tickets: invalid link")

	// View and macro errors
	ErrViewNotFound  = errors.New("tickets: view not found")
	ErrMacroNotFound = errors.New("tickets: macro not found")
	ErrMacroExists   = errors.New("tickets: a macro with this name already exists")

	// ErrForbidden means the caller may not change this record (a shared view without
	// tickets.manage, someone else's view or attachment).
	ErrForbidden = errors.New("tickets: you may not change this record")

	// ErrStorageUnavailable means file storage is not configured on this deployment.
	ErrStorageUnavailable = errors.New("tickets: file storage is not configured")
	// ErrPresenceUnavailable means the presence store (Redis) is not reachable.
	ErrPresenceUnavailable = errors.New("tickets: presence is unavailable")
)

// ValidationError is a request problem the client can fix (400); its message is safe to show.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalid(msg string) error { return &ValidationError{Msg: msg} }
