package repository

import "testing"

func TestNormalizeTag(t *testing.T) {
	name, key, err := NormalizeTag("  Ａrt#演出_2026  ")
	if err != nil {
		t.Fatal(err)
	}
	if name != "Art#演出_2026" || key != "art#演出_2026" {
		t.Fatalf("NormalizeTag = %q, %q", name, key)
	}
	if _, _, err = NormalizeTag("bad tag!"); err == nil {
		t.Fatal("expected invalid tag syntax")
	}
}
