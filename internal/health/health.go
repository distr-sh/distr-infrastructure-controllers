package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/distr-sh/distr-infrastructure-controllers/internal/status"
)

// Handler serves /healthz, which only tells that the process is alive, and /readyz, which fails once a
// run has failed. Docker reports the container and Kubernetes the pod as unhealthy for the latter, which
// is what the Distr Docker and Kubernetes controllers report as the status of the deployment.
func Handler(tracker *status.Tracker) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		snapshot := tracker.Snapshot()
		if !snapshot.Ready {
			writeJSON(w, http.StatusServiceUnavailable, snapshot)
			return
		}
		writeJSON(w, http.StatusOK, snapshot)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v status.Snapshot) {
	data, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(append(data, '\n'))
}

// Check calls /readyz on the server listening on addr and returns an error unless it is ready. It backs the
// healthcheck subcommand, so the image needs no curl or wget.
func Check(ctx context.Context, addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid health address %q: %w", addr, err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("http://%s/readyz", net.JoinHostPort(host, port)), nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var snapshot status.Snapshot
		if err := json.NewDecoder(resp.Body).Decode(&snapshot); err == nil && snapshot.Last != nil {
			return fmt.Errorf("run #%d failed: %s", snapshot.Last.Number, snapshot.Last.Error)
		}
		return fmt.Errorf("not ready: %s", resp.Status)
	}
	return nil
}
