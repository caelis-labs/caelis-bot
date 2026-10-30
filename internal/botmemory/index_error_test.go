package botmemory

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

func TestIndexFailureRetainsCauseWithoutConfirmingReceipt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index.json")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	s := &Store{dir: dir}
	err := s.rememberIndex("original-receipt")
	var writeError *localstate.WriteError
	if !errors.As(err, &writeError) || writeError.Elapsed < 0 {
		t.Fatalf("underlying write cause was lost: %v", err)
	}
	if len(s.index.Receipts) != 0 {
		t.Fatal("failed index write was confirmed")
	}
	if strings.Contains(err.Error(), dir) || strings.Contains(err.Error(), "stage=") {
		t.Fatal("technical failure escaped into user message")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := s.rememberIndex("original-receipt"); err != nil {
		t.Fatal(err)
	}
	if err := s.rememberIndex("original-receipt"); err != nil {
		t.Fatal(err)
	}
	if len(s.index.Receipts) != 1 || s.index.Receipts[0] != "original-receipt" {
		t.Fatal("same receipt recovery duplicated index identity")
	}
}
