package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStableExecutable(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "npm", "garcon"), filepath.Join(dir, "service", "garcon")
	os.MkdirAll(filepath.Dir(src), 0700)
	os.WriteFile(src, []byte("v1"), 0755)
	if err := copyExecutable(src, dst); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(src, []byte("v2"), 0755)
	if err := copyExecutable(src, dst); err != nil {
		t.Fatal(err)
	}
	os.Remove(src)
	data, err := os.ReadFile(dst)
	if err != nil || string(data) != "v2" {
		t.Fatalf("%s %v", data, err)
	}
	if err := copyExecutable(dst, dst); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(dst)
	if info.Mode().Perm() != 0755 {
		t.Fatal(info.Mode())
	}
	if err := copyExecutable(src, dst); err == nil {
		t.Fatal("missing source accepted")
	}
	data, _ = os.ReadFile(dst)
	if string(data) != "v2" {
		t.Fatal("failed copy damaged service")
	}
}

func TestServiceEscaping(t *testing.T) {
	if got := systemdQuote("/a b/100%/garcon"); got != `"/a b/100%%/garcon"` {
		t.Fatal(got)
	}
	if got := xmlText("/a&b/<garcon>"); got != "/a&amp;b/&lt;garcon&gt;" {
		t.Fatal(got)
	}
}
