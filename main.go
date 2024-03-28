package main

import (
	"context"
	"encoding/json"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jellydator/ttlcache/v3"
	"tailscale.com/client/tailscale"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

const socketsDir = "/run/superderper"

var (
	addr       = flag.String("a", "127.0.0.1:15300", "HTTP Server listen address.")
	expireTime = flag.Duration("e", time.Minute, "The time before the peer is checked from the sockets again.")

	cacheClient  = ttlcache.New[string, *tailscale.LocalClient]()
	cacheNodeKey *ttlcache.Cache[key.NodePublic, *tailscale.LocalClient]
)

func validateNodeWithClient(ctx context.Context, client *tailscale.LocalClient, nodeKey key.NodePublic) (bool, error) {
	status, err := client.Status(ctx)
	if err != nil {
		cacheClient.Delete(client.Socket)
		cacheNodeKey.Delete(nodeKey)
		return false, err
	}

	// self connect
	if nodeKey == status.Self.PublicKey {
		return true, nil
	}

	_, exists := status.Peer[nodeKey]
	return exists, nil
}

func fetchClient(ctx context.Context, nodeKey key.NodePublic) (*tailscale.LocalClient, error) {
	dirEntries, err := os.ReadDir(socketsDir)
	if err != nil {
		return nil, err
	}

	for _, entry := range dirEntries {
		if entry.Type() == fs.ModeSocket {
			item, _ := cacheClient.GetOrSet(
				entry.Name(),
				&tailscale.LocalClient{
					Socket:        filepath.Join(socketsDir, entry.Name()),
					UseSocketOnly: true,
				},
			)
			client := item.Value()

			valid, err := validateNodeWithClient(ctx, client, nodeKey)
			if valid || err != nil {
				return client, err
			}
		}
	}

	return nil, nil
}

func validateNode(ctx context.Context, nodeKey key.NodePublic) (string, error) {
	item := cacheNodeKey.Get(nodeKey)
	if item == nil {
		// fetch from the filesystem
		client, err := fetchClient(ctx, nodeKey)
		if client != nil {
			cacheNodeKey.Set(nodeKey, client, ttlcache.DefaultTTL)
			return client.Socket, err
		}
		return "", err
	}

	client := item.Value()
	valid, err := validateNodeWithClient(ctx, client, nodeKey)
	if err != nil {
		cacheNodeKey.Delete(nodeKey)
	}
	if valid {
		return client.Socket, err
	}
	return "", err
}

func postValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var jres tailcfg.DERPAdmitClientRequest
	if err := json.NewDecoder(r.Body).Decode(&jres); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// check if we have a localClient that matches the nodekey
	socket, err := validateNode(r.Context(), jres.NodePublic)
	if socket != "" {
		log.Printf("ACCESS:\tverified client %s with %s\n", jres.NodePublic, socket)
		json.NewEncoder(w).Encode(tailcfg.DERPAdmitClientResponse{Allow: true})
	}

	if err != nil {
		log.Println("ERROR:\t", err.Error())
	} else {
		log.Printf("ACCESS:\tblocked %s from %s\n", jres.Source, jres.NodePublic)
	}

	json.NewEncoder(w).Encode(tailcfg.DERPAdmitClientResponse{Allow: false})
}

func main() {
	flag.Parse()
	cacheNodeKey = ttlcache.New[key.NodePublic, *tailscale.LocalClient](
		ttlcache.WithTTL[key.NodePublic, *tailscale.LocalClient](*expireTime),
	)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	mux := http.NewServeMux()
	mux.HandleFunc("/validate", postValidate)

	server := &http.Server{
		Addr:    *addr,
		Handler: mux,
	}

	go cacheNodeKey.Start()

	go func() {
		log.Println("INFO:\tServer started on", server.Addr)
		err := server.ListenAndServe()
		if err != nil {
			log.Fatal(err)
		}
		cancel()
	}()

	<-ctx.Done()
}
