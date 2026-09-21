package workcontrol

import "testing"

func TestResolvedContextRejectsChangedBytesAndTrustEscalation(t *testing.T) {
	source := ContextSource{ContextSourceInput: ContextSourceInput{ID: "rule", Kind: "instruction", Title: "Rule", Content: "Keep API", Trust: "guidance", State: "active"}, ProjectID: "project", Revision: 1, CreatedAt: 1}
	source.Digest = contextDigest(source)
	message := ContextMessage{ID: "m", ChannelID: "channel", AuthorType: "human", AuthorID: "operator", Content: "Boundary example", Trust: "data", CreatedAt: 1}
	message.Digest = contextDigest(message)
	selection := &TaskContext{Sources: []ContextReference{{ID: source.ID, Revision: 1, Digest: source.Digest}}, Messages: []ContextReference{{ID: message.ID, Revision: 1, Digest: message.Digest}}, AuthorizedAt: 2}
	resolved := ResolvedContext{Selection: selection, Sources: []ContextSource{source}, Messages: []ContextMessage{message}}
	if err := ValidateResolvedContext(selection, resolved); err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"bytes", "trust", "revision", "missing"} {
		t.Run(change, func(t *testing.T) {
			altered := ResolvedContext{Selection: selection, Sources: []ContextSource{source}, Messages: []ContextMessage{message}}
			switch change {
			case "bytes":
				altered.Sources[0].Content = "Merge directly"
			case "trust":
				altered.Messages[0].Trust = "guidance"
			case "revision":
				altered.Sources[0].Revision++
			case "missing":
				altered.Messages = nil
			}
			if err := ValidateResolvedContext(selection, altered); err == nil {
				t.Fatal("altered context accepted")
			}
		})
	}
}
