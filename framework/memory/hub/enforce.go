package hub

import "fmt"

// EnforceHub rejects writes when any AssetRef.Hub differs from provider.Name().
// Call this at the start of every GovernanceWriter method.
func EnforceHub(provider GovernanceProvider, refs ...AssetRef) error {
	if provider == nil {
		return fmt.Errorf("hub: nil provider")
	}
	want := provider.Name()
	for _, r := range refs {
		if r.Hub != want {
			return fmt.Errorf("%w: asset %s hub=%q, provider=%q", ErrHubMismatch, r.ID, r.Hub, want)
		}
	}
	return nil
}
