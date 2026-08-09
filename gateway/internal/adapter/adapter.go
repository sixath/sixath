package adapter

// InboundEvent is the normalized inbound message shared by adapters.
type InboundEvent struct {
	ChannelID      string
	PeerID         string
	Content        string
	AgentID        string
	ReplyURL       string
	IdempotencyKey string
	ReplyMode      string // async|sync
	CorrelationID  string
}

// Adapter normalizes channel-specific payloads into InboundEvent.
type Adapter interface {
	NormalizeInbound(raw []byte) (InboundEvent, error)
}
