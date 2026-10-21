package textchannel

import (
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestAnswerDisplayUsesNativeMetadataAndKeepsUnknownPrivate(t *testing.T) {
	for _, kind := range []string{"ordinary", "secret", "async", "unknown", "retired-secret", "reused-secret-number", "stale-async-binding", "missing-async-metadata"} {
		t.Run(kind, func(t *testing.T) {
			s, _ := Open(t.TempDir(), nil)
			in := Inbound{Text: "/answer Q1 SYNTHETIC_VALUE"}
			want, private := in.Text, false
			switch kind {
			case "ordinary", "secret", "retired-secret", "reused-secret-number":
				secret := kind != "ordinary"
				_, _, err := s.Card(api.Approval{ID: "native", Status: "pending", Questions: []api.Question{{ID: "field", Type: "text", Required: true, Secret: secret}}})
				if err != nil {
					t.Fatal(err)
				}
				if secret {
					want, private = "已填写敏感字段", true
				}
				if kind == "retired-secret" || kind == "reused-secret-number" {
					delete(s.state.Prompts, "Q1")
					want = "回答内容已隐藏"
					if kind == "reused-secret-number" {
						s.state.NextQuestion = 0
						_, _ = s.AsyncCard(lifecycleQuestion(), "owner")
					}
				}
			case "async", "stale-async-binding", "missing-async-metadata":
				_, _ = s.AsyncCard(lifecycleQuestion(), "owner")
				if kind == "stale-async-binding" {
					in.expectedQuestionBinding = "old-generation"
					want, private = "回答内容已隐藏", true
				}
				if kind == "missing-async-metadata" {
					q := s.state.AsyncQuestions["Q1"]
					q.Owner = ""
					s.state.AsyncQuestions["Q1"] = q
					want, private = "回答内容已隐藏", true
				}
			case "unknown":
				want, private = "回答内容已隐藏", true
			}
			if (s.SecretPrompt(in) != "") != private || s.DisplayInput(in, false) != want {
				t.Fatal("incorrect answer privacy or display classification")
			}
		})
	}
}

func TestLostPrivateInputUsesNeutralPlaceholderWithoutMetadata(t *testing.T) {
	s, _ := Open(t.TempDir(), nil)
	if s.DisplayInput(Inbound{Text: "SYNTHETIC_PRIVATE_VALUE"}, true) != "回答内容已隐藏" {
		t.Fatal("lost metadata exposed a value or claimed a native secret marker")
	}
}
