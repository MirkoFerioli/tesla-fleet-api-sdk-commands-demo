package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResetTokenFilePreservesFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "tesla-tokens.json")
	if err := os.WriteFile(path, []byte(`{"access_token":"old"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, filepath.Join(directory, "token-link")); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := resetTokenFile(path); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("reset replaced the token file; Docker bind mounts require truncation in place")
	}
	if after.Size() != 0 {
		t.Fatal("token file was not emptied")
	}
}

func TestResetTokenFileCreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tesla-tokens.json")
	if err := resetTokenFile(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 || info.Mode().Perm() != 0600 {
		t.Fatalf("unexpected token file: size=%d mode=%v", info.Size(), info.Mode().Perm())
	}
}

func TestResetTokenFileRejectsDirectory(t *testing.T) {
	directory := t.TempDir()
	marker := filepath.Join(directory, "keep")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := resetTokenFile(directory); err == nil {
		t.Fatal("expected an error for a directory token mount")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("reset removed directory contents: %v", err)
	}
}
