package dctx

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestFixturesUseLFEndings guards the byte counts the golden files record.
//
// Every fixture file's size is reported by a walk and summed into the golden
// totals, so a CRLF checkout silently adds one byte per line and every golden
// test fails with a confusing arithmetic diff. .gitattributes marks testdata as
// binary to prevent it. This test names the cause instead of leaving the next
// reader to derive it.
func TestFixturesUseLFEndings(t *testing.T) {
	root := filepath.Join("..", "testdata")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(data, []byte("\r\n")) {
			t.Errorf("%s has CRLF line endings, which inflates every recorded size by "+
				"one byte per line. Check that .gitattributes marks testdata as -text, "+
				"then re-clone or run: git add --renormalize .", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
