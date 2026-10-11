package tofu

import (
	"os"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestTranslatorApply(t *testing.T) {
	g := NewWithT(t)
	core, logs := observer.New(zapcore.DebugLevel)
	tr := newTranslator(zap.New(core), 30*time.Second)
	tr.quietPlan = true
	// Recorded from "tofu apply -json" of examples/opentofu/hello-world, shortened and with an error added.
	events, err := os.ReadFile("testdata/apply.jsonl")
	g.Expect(err).NotTo(HaveOccurred())
	for line := range strings.SplitSeq(strings.TrimSpace(string(events)), "\n") {
		tr.Line(line)
	}

	infoLogs := logs.FilterLevelExact(zapcore.InfoLevel).All()
	messages := make([]string, 0, len(infoLogs))
	for _, entry := range infoLogs {
		messages = append(messages, entry.Message)
	}
	g.Expect(messages).To(Equal([]string{
		"random_pet.cluster: creating",
		"random_pet.cluster: creation complete after 0s [id=acme-witty-otter]",
		"time_sleep.provisioning: still creating (30s elapsed)",
		"apply complete: 1 added, 0 changed, 0 destroyed",
		`output greeting = "Hello"`,
		"output password = (sensitive value)",
	}))

	errorLogs := logs.FilterLevelExact(zapcore.ErrorLevel).All()
	g.Expect(errorLogs).To(HaveLen(2))
	g.Expect(errorLogs[0].Message).To(Equal("time_sleep.provisioning: creating failed after 41s"))
	g.Expect(errorLogs[1].Message).To(Equal("error: boom"))
	g.Expect(errorLogs[1].ContextMap()).To(HaveKeyWithValue("detail", "the provider gave up"))
	g.Expect(errorLogs[1].ContextMap()).To(HaveKeyWithValue("location", "main.tf:8"))

	g.Expect(logs.FilterLevelExact(zapcore.WarnLevel).All()[0].Message).To(Equal("not json at all"))
	g.Expect(logs.FilterMessageSnippet("hunter2").Len()).To(BeZero())

	g.Expect(tr.errors()).To(HaveLen(1))
	g.Expect(tr.summary).NotTo(BeNil())
	g.Expect(tr.changes).To(HaveLen(1))
}

func TestTranslatorPlan(t *testing.T) {
	g := NewWithT(t)
	core, logs := observer.New(zapcore.InfoLevel)
	tr := newTranslator(zap.New(core), 30*time.Second)
	tr.Line(`{"type":"resource_drift","change":{"resource":{"addr":"aws_s3_object.index"},"action":"update"}}`)
	tr.Line(`{"type":"planned_change","change":{"resource":{"addr":"aws_s3_object.index"},"action":"replace",` +
		`"reason":"cannot_update"}}`)
	tr.Line(`{"type":"change_summary","changes":{"add":1,"change":0,"remove":1,"import":0,"operation":"plan"}}`)

	entries := logs.All()
	g.Expect(entries).To(HaveLen(3))
	g.Expect(entries[0].Message).To(Equal("plan: replace aws_s3_object.index (cannot_update)"))
	g.Expect(entries[1].Level).To(Equal(zapcore.WarnLevel))
	g.Expect(entries[1].Message).To(Equal("drift: aws_s3_object.index was changed outside of OpenTofu (update)"))
	g.Expect(entries[2].Message).To(Equal("plan: 1 to add, 0 to change, 1 to destroy"))
}

func TestTranslatorDriftWithoutChanges(t *testing.T) {
	g := NewWithT(t)
	core, logs := observer.New(zapcore.DebugLevel)
	tr := newTranslator(zap.New(core), 30*time.Second)
	tr.Line(`{"type":"resource_drift","change":{"resource":{"addr":"aws_s3_bucket.website"},"action":"update"}}`)
	tr.Line(`{"type":"change_summary","changes":{"add":0,"change":0,"remove":0,"import":0,"operation":"plan"}}`)

	entries := logs.All()
	g.Expect(entries).To(HaveLen(1))
	g.Expect(entries[0].Level).To(Equal(zapcore.DebugLevel))
}

func TestChangeSummaryString(t *testing.T) {
	g := NewWithT(t)
	g.Expect(ChangeSummary{Add: 1, Import: 2, Operation: "plan"}.String()).
		To(Equal("1 to add, 0 to change, 0 to destroy, 2 to import"))
	g.Expect(ChangeSummary{Remove: 3, Operation: "destroy"}.String()).
		To(Equal("0 added, 0 changed, 3 destroyed"))
}
