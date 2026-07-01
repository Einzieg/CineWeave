package sources

import "testing"

func TestCleanImportedText(t *testing.T) {
	got := CleanImportedText("\ufeff 第一段\r\n\r\n\r\n\r\n第二段  \r第三段 ")
	want := "第一段\n\n\n第二段  \n第三段"
	if got != want {
		t.Fatalf("CleanImportedText() = %q, want %q", got, want)
	}
}
