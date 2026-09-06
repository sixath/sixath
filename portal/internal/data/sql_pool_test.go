package data

import (
	"testing"
	"time"
)

func TestConfigureSQLPoolDefaults(t *testing.T) {
	configureSQLPool(nil) // no panic

	if defaultConnMaxLifetime != 5*time.Minute {
		t.Fatalf("ConnMaxLifetime=%v", defaultConnMaxLifetime)
	}
	if defaultConnMaxIdleTime != 3*time.Minute {
		t.Fatalf("ConnMaxIdleTime=%v", defaultConnMaxIdleTime)
	}
	if defaultMaxOpenConns != 25 || defaultMaxIdleConns != 5 {
		t.Fatalf("open=%d idle=%d", defaultMaxOpenConns, defaultMaxIdleConns)
	}
}
