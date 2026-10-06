package wirejson

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestHugeToolPayloadDoesNotConsumeProjectionOrFollowingApproval(t *testing.T) {
	image := strings.Repeat("x", 14<<20)
	frame := `{"method":"item/completed","params":{"threadId":"owner","turnId":"turn","item":{"id":"tool","type":"mcpToolCall","result":{"content":[{"type":"image","data":"` + image + `"}]},"status":"completed"}}}`
	approval := `{"id":"approval-original","method":"mcpServer/elicitation/request","params":{"requestedSchema":{"properties":{"result":{"type":"string"},"url":{"type":"string"}}},"_meta":{"persist":["once","session","always","deny"]}}}`
	r := bufio.NewReaderSize(strings.NewReader(frame+"\n"+approval+"\n"), 64<<10)
	out, header, err := ReadLine(r)
	if err != nil || header.Method != "item/completed" || len(out) > 1024 || bytes.Contains(out, []byte("content")) {
		t.Fatal("tool bytes entered projection", len(out), err)
	}
	next, h, err := ReadLine(r)
	if err != nil || string(h.ID) != `"approval-original"` || string(next) != approval {
		t.Fatal("native approval authority changed", err, string(next))
	}
}

func TestOversizedProseIsDrainedAndOriginalIDRetainedInAnyOrder(t *testing.T) {
	for _, tail := range []bool{false, true} {
		payload := `"result":{"text":"` + strings.Repeat("x", maxWireFrame+100) + `"}`
		original := `"id":"original"`
		if tail {
			payload += "," + original
		} else {
			payload = original + "," + payload
		}
		r := bufio.NewReader(strings.NewReader("{" + payload + "}\n" + `{"id":"next","result":true}`))
		_, h, err := ReadLine(r)
		if !errors.Is(err, ErrTooLarge) || string(h.ID) != `"original"` {
			t.Fatal("lost correlation", h, err)
		}
		out, h, err := ReadLine(r)
		if err != nil || string(h.ID) != `"next"` || !json.Valid(out) {
			t.Fatal("oversize poisoned next frame", h, err)
		}
	}
}

func TestNativeHumanTextSurvivesImageOmissionAndEscapes(t *testing.T) {
	data := `{"result":{"data":[{"id":"turn","items":[{"id":"user","type":"userMessage","content":[{"type":"text","text":"hello \\"},{"type":"image","url":"data:image/png;base64,` + strings.Repeat("x", 10<<20) + `"}]}]}]},"id":1}`
	out, _, err := ReadLine(bufio.NewReader(strings.NewReader(data)))
	if err != nil || len(out) > 512 || !bytes.Contains(out, []byte("hello")) || bytes.Contains(out, []byte("base64,")) {
		t.Fatal(len(out), err)
	}
}

func TestMalformedJSONLDoesNotWaitForAnotherFrame(t *testing.T) {
	_, _, err := ReadLine(bufio.NewReader(strings.NewReader("{\"id\":1,\"result\":\n{\"id\":2,\"result\":true}\n")))
	if !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

func TestCaelisCommandEvidenceIsRetainedWhileToolBytesAreOmitted(t *testing.T) {
	data := `{"update":{"sessionUpdate":"tool_call_update","rawInput":{"action":"wait","command":"private"},"rawOutput":{"handle":"original-command","state":"working","tasks":[{"id":"original-task"}],"stdout":"` + strings.Repeat("x", 14<<20) + `"},"content":[{"type":"text","text":"private tool bytes"}]}}`
	out, _, err := Read(bufio.NewReader(strings.NewReader(data)))
	if err != nil || len(out) > 512 || !bytes.Contains(out, []byte("original-command")) || !bytes.Contains(out, []byte("original-task")) || bytes.Contains(out, []byte("private")) {
		t.Fatal(len(out), string(out), err)
	}
}

func TestLargeQuotedToolResultDoesNotLoseFollowingCommandEvidence(t *testing.T) {
	data := `{"update":{"sessionUpdate":"tool_call_update","rawOutput":{"result":"` + strings.Repeat(`escaped \"output\" `, 1<<20) + `","handle":"original-command","state":"completed"}}}`
	out, _, err := Read(bufio.NewReader(strings.NewReader(data)))
	if err != nil || len(out) > 512 || !json.Valid(out) || !bytes.Contains(out, []byte("original-command")) || !bytes.Contains(out, []byte("completed")) {
		t.Fatal("lost original command evidence", len(out), err)
	}
}
