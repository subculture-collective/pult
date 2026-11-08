package apply

import "testing"

func TestDiff(t *testing.T) {
	tests := []struct{ name, a, b, want string }{
		{"equal", "x\n", "x\n", ""},
		{"append", "a\nb\n", "a\nb\nc\n", "--- f\n+++ f (Pult)\n@@ -1,2 +1,3 @@\n a\n b\n+c\n"},
		{"new file", "", "a\nb\n", "--- f\n+++ f (Pult)\n@@ -1,0 +1,2 @@\n+a\n+b\n"},
		{"change in the middle", "1\n2\n3\n4\n5\n6\n7\n8\n9\n", "1\n2\n3\n4\nX\n6\n7\n8\n9\n",
			"--- f\n+++ f (Pult)\n@@ -2,7 +2,7 @@\n 2\n 3\n 4\n-5\n+X\n 6\n 7\n 8\n"},
		{"two hunks", "a\n1\n2\n3\n4\n5\n6\n7\n8\nb\n", "A\n1\n2\n3\n4\n5\n6\n7\n8\nB\n",
			"--- f\n+++ f (Pult)\n@@ -1,4 +1,4 @@\n-a\n+A\n 1\n 2\n 3\n@@ -7,4 +7,4 @@\n 6\n 7\n 8\n-b\n+B\n"},
	}
	for _, tt := range tests {
		if got := Diff("f", []byte(tt.a), []byte(tt.b)); got != tt.want {
			t.Errorf("%s:\n%s\nwant\n%s", tt.name, got, tt.want)
		}
	}
}
