package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkerMetadataUsesActualOwnerLimitsWithoutBytes(t *testing.T) {
	caps := WorkCapabilities{InputMaxFiles: 4, InputMaxFileBytes: 8 << 20}
	for _, files := range [][]WorkFileMetadata{{{Name: "a.csv", Bytes: 9 << 20}}, {{Name: "a", Bytes: -1}}, {{Name: "..", Bytes: 1}}, make([]WorkFileMetadata, 5)} {
		if ValidateWorkFileMetadata(files, caps) == nil {
			t.Fatal("metadata bypassed actual owner limits", files)
		}
	}
	if ValidateWorkFileMetadata([]WorkFileMetadata{{Name: "a.csv", Bytes: 8 << 20}}, caps) != nil || ValidateWorkFileMetadata(nil, WorkCapabilities{}) == nil {
		t.Fatal("missing limits advertised as compatible")
	}
	path := filepath.Join(t.TempDir(), "selected.csv")
	data := []byte("a,b\n1,2\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	input := []InputFile{{Name: "selected.csv", Path: path}}
	meta, err := WorkFileMetadataFor(input)
	if err != nil || len(meta) != 1 || meta[0].Bytes != int64(len(data)) {
		t.Fatal(meta, err)
	}
	expected, err := WorkInputDigests(input)
	if err != nil {
		t.Fatal(err)
	}
	actual := WorkContentDigests([]WorkFileContent{{Name: "selected.csv", Data: data}})
	if len(actual) != 1 || actual[0] != expected[0] {
		t.Fatal("wire bytes do not share native intent digest")
	}
}
