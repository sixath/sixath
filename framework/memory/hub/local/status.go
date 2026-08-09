package local

import (
	"strings"

	"github.com/sixath/framework/memory/hub"
)

const (
	metaHubStatus = "hub_status"
	// UnitDBActive / UnitDBSuperseded / UnitDBDeleted mirror memory_units.status values.
	UnitDBActive      = "active"
	UnitDBSuperseded  = "superseded"
	UnitDBDeleted     = "deleted"
)

// MapUnitToAssetStatus maps DB unit status + metadata to provider AssetStatus (design §4.1).
func MapUnitToAssetStatus(dbStatus string, metadata map[string]any) hub.AssetStatus {
	dbStatus = strings.ToLower(strings.TrimSpace(dbStatus))
	switch dbStatus {
	case UnitDBSuperseded:
		return hub.AssetSuperseded
	case UnitDBDeleted:
		return hub.AssetArchived
	default:
		// active or unknown → check hub_status metadata
		hs := hubStatusFromMeta(metadata)
		switch hs {
		case string(hub.AssetDraft):
			return hub.AssetDraft
		case string(hub.AssetStale):
			return hub.AssetStale
		case string(hub.AssetActive), "":
			return hub.AssetActive
		default:
			return hub.AssetActive
		}
	}
}

// LoadoutEligible reports whether an asset may enter ResolveLoadout / Prefetch filters.
func LoadoutEligible(st hub.AssetStatus) bool {
	return st == hub.AssetActive
}

func hubStatusFromMeta(metadata map[string]any) string {
	if metadata == nil {
		return ""
	}
	v, ok := metadata[metaHubStatus]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return strings.ToLower(strings.TrimSpace(s))
}

// ApplyHubStatusMeta sets metadata.hub_status for draft/stale; clears for active.
func ApplyHubStatusMeta(metadata map[string]any, st hub.AssetStatus) map[string]any {
	if metadata == nil {
		metadata = map[string]any{}
	}
	switch st {
	case hub.AssetDraft, hub.AssetStale:
		metadata[metaHubStatus] = string(st)
	case hub.AssetActive:
		delete(metadata, metaHubStatus)
	}
	return metadata
}
