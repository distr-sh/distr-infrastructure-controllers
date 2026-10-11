package health

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/distr-sh/distr-controller-contrib/internal/status"
	. "github.com/onsi/gomega"
)

func TestReadinessFollowsTheLastFinishedRun(t *testing.T) {
	g := NewWithT(t)
	tracker := &status.Tracker{}
	server := httptest.NewServer(Handler(tracker))
	defer server.Close()
	addr := server.Listener.Addr().(*net.TCPAddr).String()

	readyz := func() int {
		resp, err := http.Get(server.URL + "/readyz")
		g.Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		return resp.StatusCode
	}

	g.Expect(readyz()).To(Equal(http.StatusOK))

	tracker.Start(status.Run{Number: 1, StartedAt: time.Now()})
	g.Expect(readyz()).To(Equal(http.StatusOK))

	tracker.Finish(status.Run{Number: 1, Result: status.ResultFailed, Error: "quota exceeded"})
	g.Expect(readyz()).To(Equal(http.StatusServiceUnavailable))
	g.Expect(Check(t.Context(), addr)).To(MatchError("run #1 failed: quota exceeded"))

	tracker.Start(status.Run{Number: 2})
	g.Expect(readyz()).To(Equal(http.StatusServiceUnavailable))

	tracker.Finish(status.Run{Number: 2, Result: status.ResultSucceeded})
	g.Expect(readyz()).To(Equal(http.StatusOK))
	g.Expect(Check(t.Context(), addr)).To(Succeed())

	resp, err := http.Get(server.URL + "/healthz")
	g.Expect(err).NotTo(HaveOccurred())
	defer resp.Body.Close()
	g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
}
