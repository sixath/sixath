package errors

import "errors"

var (
	ErrNotFound      = errors.New("not found")
	ErrDuplicateName = errors.New("duplicate name")
	ErrConflict      = errors.New("conflict: resource in use")
)
