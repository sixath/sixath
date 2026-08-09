package hub

import "errors"

var (
	// ErrHubMismatch is returned when AssetRef.Hub does not match the resolved provider Name().
	ErrHubMismatch = errors.New("hub: asset.Hub does not match resolved provider")
	// ErrNotSupported is returned for optional Writer/Call operations a provider does not implement.
	ErrNotSupported = errors.New("hub: not supported")
	// ErrTransport is a sentinel for runtime transport failures (timeout / connection / 5xx).
	// Callers may wrap underlying errors with fmt.Errorf("%w: %v", ErrTransport, err).
	ErrTransport = errors.New("hub: transport error")
)
