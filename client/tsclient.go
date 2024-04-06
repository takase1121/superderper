package client

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"tailscale.com/client/tailscale"
	"tailscale.com/types/key"
)

type tsClient struct {
	tailscale.LocalClient
	mu sync.Mutex
}

const requestTimeout = 15 * time.Second

func (c *tsClient) verify(nodeKey key.NodePublic, timeout ...time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	actualTimeout := requestTimeout
	if len(timeout) == 1 {
		actualTimeout = timeout[0]
	}

	slog.Debug("client fetch status", "socket", c.Socket)
	ctx, cancel := context.WithTimeout(context.Background(), actualTimeout)
	defer cancel()

	status, err := c.Status(ctx)
	if err != nil {
		return false, err
	}

	// self-connect
	if nodeKey == status.Self.PublicKey {
		slog.Debug("client verify", "socket", c.Socket, "type", "self-connect", "nodekey", nodeKey)
		return true, nil
	}

	// check peers
	_, exists := status.Peer[nodeKey]
	slog.Debug("client verify", "socket", c.Socket, "type", "peers", "ok", exists, "nodekey", nodeKey)
	return exists, nil
}
