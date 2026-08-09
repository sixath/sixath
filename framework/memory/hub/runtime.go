package hub

import (
	"context"
	"errors"
)

// IsTransport reports whether err is (or wraps) ErrTransport.
func IsTransport(err error) bool {
	return errors.Is(err, ErrTransport)
}

// ReadLoadout calls gov.ResolveLoadout with §3.5.1 read policy.
// On transport failure: optional fallback to defaultGov, then fail-open empty list.
func ReadLoadout(
	ctx context.Context,
	gov GovernanceProvider,
	id Identity,
	fallbackToDefault bool,
	defaultGov GovernanceProvider,
) ([]AssetRef, error) {
	refs, err := gov.ResolveLoadout(ctx, id)
	if err == nil {
		return refs, nil
	}
	if !IsTransport(err) {
		return nil, err
	}
	if fallbackToDefault && defaultGov != nil && defaultGov.Name() != gov.Name() {
		refs2, err2 := defaultGov.ResolveLoadout(ctx, id)
		if err2 == nil {
			return refs2, nil
		}
		if !IsTransport(err2) {
			return nil, err2
		}
	}
	// fail-open empty
	return nil, nil
}

// CheckAccessSafe applies §3.5.1: transport failure without successful fallback → deny (false).
func CheckAccessSafe(
	ctx context.Context,
	gov GovernanceProvider,
	id Identity,
	asset AssetRef,
	action string,
	fallbackToDefault bool,
	defaultGov GovernanceProvider,
) (bool, error) {
	ok, err := gov.CheckAccess(ctx, id, asset, action)
	if err == nil {
		return ok, nil
	}
	if !IsTransport(err) {
		return false, err
	}
	if fallbackToDefault && defaultGov != nil && defaultGov.Name() != gov.Name() {
		ok2, err2 := defaultGov.CheckAccess(ctx, id, asset, action)
		if err2 == nil {
			return ok2, nil
		}
		if !IsTransport(err2) {
			return false, err2
		}
	}
	return false, nil
}
