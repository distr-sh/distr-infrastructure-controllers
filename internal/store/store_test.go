package store

import (
	"testing"

	"github.com/distr-sh/distr-infrastructure-controllers/internal/status"
	. "github.com/onsi/gomega"
)

func TestPersistedRoundTrip(t *testing.T) {
	g := NewWithT(t)
	s, err := Open(t.TempDir())
	g.Expect(err).NotTo(HaveOccurred())

	p, err := s.ReadPersisted()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(p.LastRun).To(BeNil())

	g.Expect(s.WritePersisted(Persisted{
		LastRun:            &status.Run{Number: 3, Result: status.ResultSucceeded},
		LastAppliedVersion: "1.2.3",
	})).To(Succeed())
	p, err = s.ReadPersisted()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(p.LastAppliedVersion).To(Equal("1.2.3"))
	g.Expect(p.LastRun.Number).To(Equal(3))
}
