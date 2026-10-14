package tofu

import (
	"fmt"
	"os"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

func TestMain(m *testing.M) {
	if os.Getenv("FAKE_TOFU") != "" {
		fmt.Println("before")
		fmt.Println(strings.Repeat("a", maxLineLength+1))
		fmt.Println("after")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestRunFailsWhenTheOutputIsCutOff(t *testing.T) {
	g := NewWithT(t)
	runner := &Runner{Binary: os.Args[0], Dir: t.TempDir(), Env: []string{"FAKE_TOFU=1"}, Logger: zap.NewNop()}

	var lines []string
	_, err := runner.run(t.Context(), []string{"apply"}, func(line string) { lines = append(lines, line) }, nil)

	g.Expect(err).To(MatchError(ContainSubstring("could not read the output of tofu apply: a line is longer than 4 MiB")))
	g.Expect(lines).To(Equal([]string{"before"}))
}
