package netx

import (
	"errors"
	"fmt"
	"strings"
)

// AnnotateError prefixes err with a proxy identity. Never includes Password.
func AnnotateError(spec Spec, err error) error {
	if err == nil {
		return nil
	}
	if pw := spec.Password; pw != "" {
		if msg := err.Error(); strings.Contains(msg, pw) {
			err = errors.New(strings.ReplaceAll(msg, pw, "***"))
		}
	}
	label := strings.TrimSpace(spec.ID)
	if label == "" {
		label = strings.TrimSpace(spec.Host)
	}
	if label == "" {
		return err
	}
	return fmt.Errorf("proxy %s: %w", label, err)
}
