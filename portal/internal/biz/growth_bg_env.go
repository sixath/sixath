package biz

import (
	"os"
	"strings"
	"time"
)

const (
	defaultBgReviewDedupeWindow = 10 * time.Minute
	defaultBgReviewInFlightTTL  = 15 * time.Minute
)

// BgReviewDedupeWindow is growth.background_review.dedupe_window (env stand-in).
//
//	SATH_BG_REVIEW_DEDUPE_WINDOW — Go duration; default 10m. Independent of IdleCheckInterval.
func BgReviewDedupeWindow() time.Duration {
	return parsePositiveDurationEnv("SATH_BG_REVIEW_DEDUPE_WINDOW", defaultBgReviewDedupeWindow)
}

// BgReviewInFlightTTL is growth.background_review.in_flight_ttl (env stand-in).
//
//	SATH_BG_REVIEW_IN_FLIGHT_TTL — Go duration; default 15m.
func BgReviewInFlightTTL() time.Duration {
	return parsePositiveDurationEnv("SATH_BG_REVIEW_IN_FLIGHT_TTL", defaultBgReviewInFlightTTL)
}

func parsePositiveDurationEnv(key string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

// RecentlyBackgroundReviewed reports whether a C3 BackgroundReview completed within
// dedupe_window. Used by Worker claim gates and TrySessionEnd* skip.
func RecentlyBackgroundReviewed(st *ChatGrowthState, window time.Duration, now time.Time) bool {
	if st == nil || st.LastBackgroundReviewAt == nil || window <= 0 {
		return false
	}
	return now.Sub(*st.LastBackgroundReviewAt) < window
}

// HasNewerPendingThanLastBG is true when pending flags are set and state was updated
// after LastBackgroundReviewAt (new pending placed after the recent C3 review).
func HasNewerPendingThanLastBG(st *ChatGrowthState, stateUpdatedAt time.Time) bool {
	if st == nil || st.LastBackgroundReviewAt == nil {
		return false
	}
	if !st.PendingSkillReview && !st.PendingMemoryReview {
		return false
	}
	return stateUpdatedAt.After(*st.LastBackgroundReviewAt)
}
