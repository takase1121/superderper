package client

import (
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jellydator/ttlcache/v3"
	"tailscale.com/client/tailscale"
	"tailscale.com/types/key"
)

type TSClientList struct {
	list       []*tsClient
	mu         sync.RWMutex
	expireTime time.Duration
	expiry     time.Time

	nodeKeys    *ttlcache.Cache[key.NodePublic, *tsClient]
	socketsPath string
}

func New(socketsPath string, expireTime time.Duration) *TSClientList {
	clientList := &TSClientList{
		expireTime:  expireTime,
		socketsPath: socketsPath,
		nodeKeys: ttlcache.New(
			ttlcache.WithTTL[key.NodePublic, *tsClient](expireTime),
		),
	}
	return clientList
}

func (t *TSClientList) StartPurge() {
	t.nodeKeys.Start()
}

func (t *TSClientList) StopPurge() {
	t.nodeKeys.Stop()
}

func (t *TSClientList) Verify(nodeKey key.NodePublic) (string, error) {
	item := t.nodeKeys.Get(nodeKey)
	if item == nil {
		client, err := t.fetch(nodeKey)
		if err != nil {
			return "", err
		}
		if client == nil {
			return "", err
		}
		item = t.nodeKeys.Set(nodeKey, client, ttlcache.DefaultTTL)
	}
	return item.Value().Socket, nil
}

func (t *TSClientList) isExpired() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return time.Now().After(t.expiry)
}

func (t *TSClientList) update() error {
	// don't clear list before expiry
	if !t.isExpired() {
		return nil
	}

	dirEntries, err := os.ReadDir(t.socketsPath)
	if err != nil {
		return err
	}

	var newList []*tsClient
	for _, entry := range dirEntries {
		if entry.Type() == fs.ModeSocket {
			slog.Debug("create client", "socket", entry.Name())
			client := &tsClient{LocalClient: tailscale.LocalClient{
				Socket:        filepath.Join(t.socketsPath, entry.Name()),
				UseSocketOnly: true,
			}}
			newList = append(newList, client)
		}
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.list = newList
	t.expiry = time.Now().Add(t.expireTime)
	return nil
}

func (t *TSClientList) fetch(nodeKey key.NodePublic) (*tsClient, error) {
	err := t.update()
	if err != nil {
		return nil, err
	}

	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, client := range t.list {
		if ok, err := client.verify(nodeKey); err != nil {
			return nil, err
		} else if ok {
			return client, nil
		}
	}
	return nil, nil
}
