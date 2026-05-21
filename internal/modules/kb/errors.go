package kb

import "errors"

var (
	ErrArticleNotFound = errors.New("kb article not found")
	ErrSlugConflict    = errors.New("kb article slug already exists")
)
