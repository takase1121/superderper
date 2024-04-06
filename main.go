package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/takase1121/superderper/client"
	"tailscale.com/tailcfg"
)

const socketsDir = "/run/superderper"

var (
	addr       = flag.String("a", "127.0.0.1:15300", "HTTP Server listen address.")
	expireTime = flag.Duration("e", time.Minute, "The time before the peer is checked from the sockets again.")
	debug      = flag.Bool("d", false, "Turns on extra logging for debugging purposes.")

	clients *client.TSClientList
)

func postValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var jres tailcfg.DERPAdmitClientRequest
	if err := json.NewDecoder(r.Body).Decode(&jres); err != nil {
		slog.Error("error parsing JSON", "error", err)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	socket, err := clients.Verify(jres.NodePublic)
	slog.Info("client verified", "admitted", socket != "", "nodekey", jres.NodePublic, "socket", socket)
	if err != nil {
		slog.Error("error verifying client", "error", err)
	}

	response, err := json.Marshal(tailcfg.DERPAdmitClientResponse{Allow: socket != ""})
	if err != nil {
		slog.Error("error writing JSON", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Add("Content-Type", "application/json")
	_, _ = w.Write(response)
}

func main() {
	flag.Parse()
	if *debug {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}

	clients = client.New(socketsDir, *expireTime)

	mux := http.NewServeMux()
	mux.HandleFunc("/validate", postValidate)

	server := &http.Server{
		Addr:    *addr,
		Handler: mux,
	}

	go clients.StartPurge()
	go func() {
		slog.Info("HTTP server started", "addr", server.Addr)
		err := server.ListenAndServe()
		if !errors.Is(err, http.ErrServerClosed) {
			slog.Error("error running HTTP server", "error", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		slog.Error("error shutting down", "error", err)
	}

	slog.Info("HTTP server closed")
	clients.StopPurge()
}
