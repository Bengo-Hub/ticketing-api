package routing

import "errors"

var (
	ErrRuleNotFound  = errors.New("routing rule not found")
	ErrInvalidRule   = errors.New("invalid routing rule")
)
