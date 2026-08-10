package session

import (
	"context"
	"sync"
	"time"

	"github.com/sixath/gateway/internal/runtimeclient"
)

type cacheKey struct {
	channelID string
	peerID    string
}

type cacheEntry struct {
	reply     *runtimeclient.ResolveReply
	expiresAt time.Time
}

// Router resolves channel+peer sessions with a short in-memory cache.
type Router struct {
	client *runtimeclient.Client
	ttl    time.Duration

	mu    sync.Mutex
	cache map[cacheKey]cacheEntry
}

// NewRouter builds a SessionRouter. ttl <= 0 defaults to 30s.
func NewRouter(client *runtimeclient.Client, ttl time.Duration) *Router {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &Router{
		client: client,
		ttl:    ttl,
		cache:  make(map[cacheKey]cacheEntry),
	}
}

// Resolve looks up or creates a session for channel+peer.
// userID is optional on the resolve call; the returned reply.UserID should be used for turns.
// ForceNew always bypasses the peer cache so Portal rebind is not short-circuited.
func (r *Router) Resolve(ctx context.Context, userID string, req runtimeclient.ResolveRequest) (*runtimeclient.ResolveReply, error) {
	key := cacheKey{channelID: req.ChannelID, peerID: req.PeerID}
	now := time.Now()

	if !req.ForceNew {
		r.mu.Lock()
		if e, ok := r.cache[key]; ok && now.Before(e.expiresAt) {
			out := *e.reply
			r.mu.Unlock()
			return &out, nil
		}
		r.mu.Unlock()
	}

	reply, err := r.client.ResolveSession(ctx, userID, req)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	r.cache[key] = cacheEntry{reply: reply, expiresAt: now.Add(r.ttl)}
	r.mu.Unlock()
	return reply, nil
}

// Invalidate drops the cached resolve result for channel+peer.
func (r *Router) Invalidate(channelID, peerID string) {
	r.mu.Lock()
	delete(r.cache, cacheKey{channelID: channelID, peerID: peerID})
	r.mu.Unlock()
}
