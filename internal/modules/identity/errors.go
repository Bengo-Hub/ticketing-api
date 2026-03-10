package identity

import "errors"

var (
	// ErrTeamNotFound is returned when a team is not found.
	ErrTeamNotFound = errors.New("identity: team not found")

	// ErrTeamExists is returned when a team with the same name already exists.
	ErrTeamExists = errors.New("identity: team already exists")

	// ErrAgentNotFound is returned when an agent is not found.
	ErrAgentNotFound = errors.New("identity: agent not found")

	// ErrAgentExists is returned when an agent with the same user_id already exists.
	ErrAgentExists = errors.New("identity: agent already exists")

	// ErrInvalidRole is returned when an invalid role is provided.
	ErrInvalidRole = errors.New("identity: invalid role")

	// ErrInvalidStatus is returned when an invalid status is provided.
	ErrInvalidStatus = errors.New("identity: invalid status")

	// ErrAgentInactive is returned when trying to assign to an inactive agent.
	ErrAgentInactive = errors.New("identity: agent is inactive")

	// ErrAgentUnavailable is returned when agent is not available.
	ErrAgentUnavailable = errors.New("identity: agent is not available")

	// ErrAgentAtCapacity is returned when agent has reached max ticket capacity.
	ErrAgentAtCapacity = errors.New("identity: agent is at capacity")
)
