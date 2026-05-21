package sla

import "errors"

var (
	ErrPolicyNotFound = errors.New("sla policy not found")
	ErrPolicyInUse    = errors.New("sla policy is referenced by active tickets and cannot be deleted")
	ErrInvalidPolicy  = errors.New("invalid sla policy")
)
