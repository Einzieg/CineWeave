package exporter

import "testing"

func TestSafeFileNameSanitizesUnsafeCharacters(t *testing.T) {
	got := SafeFileName(`a/b\c:d*e?f"g<h>i|j`, "fallback")
	want := "a_b_c_d_e_f_g_h_i_j"
	if got != want {
		t.Fatalf("SafeFileName() = %q, want %q", got, want)
	}
	if got := SafeFileName(" . ", "fallback"); got != "fallback" {
		t.Fatalf("SafeFileName fallback = %q", got)
	}
}

func TestSafeZipPathRejectsTraversal(t *testing.T) {
	if _, err := safeZipPath("project", "../secret.txt"); err == nil {
		t.Fatal("safeZipPath accepted traversal")
	}
	if _, err := safeZipPath("project", "media/final.mp4"); err != nil {
		t.Fatalf("safeZipPath rejected valid path: %v", err)
	}
}
