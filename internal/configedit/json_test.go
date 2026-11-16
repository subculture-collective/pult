package configedit

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tailscale/hujson"
)

func TestJSONKeys(t *testing.T) {
	for _, tt := range []struct {
		name, src, fragment string
		paths               []string
		comments            bool
		keep                []string
	}{
		{"replace literal dotted keys", "{\n  // keep my font\n  \"editor.fontSize\": 14,\n  \"workbench.colorCustomizations\": {\"editor.background\": \"old\"}, // colors\n  \"files.autoSave\": \"off\",\n}\n", `{"workbench.colorCustomizations":{"editor.background":"#100b19"}}`, []string{"/workbench.colorCustomizations"}, true, []string{"  // keep my font\n  \"editor.fontSize\": 14,\n", ", // colors\n  \"files.autoSave\": \"off\",\n}\n"}},
		{"insert before closing comment", "{\n  \"font\": 14 // font comment\n  // footer\n}\n", `{"colors":{"background":"black"}}`, []string{"/colors"}, true, []string{"\"font\": 14", "// font comment", "// footer"}},
		{"nested insertion", "{\n  \"ui\": {\n    \"font\": 14\n  }\n}\n", `{"ui":{"colors":{"a":"red","b":"blue"}}}`, []string{"/ui/colors/a", "/ui/colors/b"}, false, []string{"\"font\": 14"}},
		{"missing ancestors", `{"other":true}`, `{"ui":{"colors":{"a":"red"}}}`, []string{"/ui/colors/a"}, false, []string{`"other":true`}},
		{"escaped keys", `{}`, `{"a/b":{"~key":"red"}}`, []string{"/a~1b/~0key"}, false, nil},
		{"empty config", "", `{"theme":"Pult"}`, []string{"/theme"}, false, nil},
		{"CRLF and tabs", "{\r\n\t\"font\": 14,\r\n\t\"colors\": {}\r\n}\r\n", `{"colors":{"a":"red","b":"blue"},"theme":"Pult"}`, []string{"/colors", "/theme"}, false, []string{"\r\n\t\"font\": 14,\r\n", "\r\n\t\t\"a\": \"red\""}},
		{"missing optional fragment path", "{\"font\":14}\n", `{}`, []string{"/colors"}, false, []string{"{\"font\":14}\n"}},
		{"equivalent object keeps original formatting", "{\"colors\": {\"b\": 2, /* mine */ \"a\": 1}}", `{"colors":{"a":1,"b":2}}`, []string{"/colors"}, true, []string{"{\"colors\": {\"b\": 2, /* mine */ \"a\": 1}}"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := JSONKeys([]byte(tt.src), []byte(tt.fragment), tt.paths, tt.comments)
			if err != nil {
				t.Fatal(err)
			}
			for _, keep := range tt.keep {
				if !strings.Contains(string(got), keep) {
					t.Errorf("lost %q in\n%s", keep, got)
				}
			}
			doc, err := hujson.Parse(got)
			if err != nil {
				t.Fatal(err)
			}
			frag, err := hujson.Parse([]byte(tt.fragment))
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range tt.paths {
				if want := frag.Find(p); want != nil {
					actual := doc.Find(p)
					if actual == nil || !jsonEqual(actual, want) {
						t.Errorf("%s not applied: %s", p, got)
					}
				}
			}
			if !tt.comments && !doc.IsStandard() {
				t.Errorf("non-standard JSON: %s", got)
			}
			again, err := JSONKeys(got, []byte(tt.fragment), tt.paths, tt.comments)
			if err != nil || !bytes.Equal(got, again) {
				t.Errorf("second apply changed the document: %s, %v", again, err)
			}
			if strings.Contains(tt.src, "\r\n") && bytes.Contains(bytes.ReplaceAll(got, []byte("\r\n"), nil), []byte("\n")) {
				t.Errorf("introduced LF in CRLF config: %q", got)
			}
		})
	}
}

func TestJSONKeysRejectsUnsafeEdits(t *testing.T) {
	for _, tt := range []struct {
		name, src, fragment string
		paths               []string
		comments            bool
	}{
		{"malformed source", `{,}`, `{"color":"red"}`, []string{"/color"}, true},
		{"malformed fragment", `{}`, `{"color":}`, []string{"/color"}, true},
		{"duplicate source", `{"color":1,"color":2}`, `{"color":"red"}`, []string{"/color"}, false},
		{"escaped duplicate source", `{"color":1,"\u0063olor":2}`, `{"color":"red"}`, []string{"/color"}, false},
		{"duplicate fragment", `{}`, `{"color":1,"color":2}`, []string{"/color"}, false},
		{"unowned key", `{}`, `{"color":"red","font":14}`, []string{"/color"}, false},
		{"unowned nested key", `{}`, `{"ui":{"color":"red","font":14}}`, []string{"/ui/color"}, false},
		{"scalar parent", `{"ui":false}`, `{"ui":{"color":"red"}}`, []string{"/ui/color"}, false},
		{"later scalar parent", "{\n  // mine\n  \"ui\": false\n}\n", `{"color":"red","ui":{"color":"red"}}`, []string{"/color", "/ui/color"}, true},
		{"array parent", `{"ui":[{}]}`, `{"ui":{"0":{"color":"red"}}}`, []string{"/ui/0/color"}, false},
		{"scalar fragment parent", `{}`, `{"ui":null}`, []string{"/ui/color"}, false},
		{"array root", `[]`, `{"color":"red"}`, []string{"/color"}, false},
		{"root pointer", `{}`, `{"color":"red"}`, []string{""}, false},
		{"dotted path", `{}`, `{"color":"red"}`, []string{"color"}, false},
		{"bad escape", `{}`, `{"color":"red"}`, []string{"/col~2or"}, false},
		{"overlapping paths", `{}`, `{"ui":{"color":"red"}}`, []string{"/ui", "/ui/color"}, false},
		{"duplicate paths", `{}`, `{"color":"red"}`, []string{"/color", "/color"}, false},
		{"strict comments", `{/* keep */}`, `{"color":"red"}`, []string{"/color"}, false},
		{"strict trailing comma", `{"font":14,}`, `{"color":"red"}`, []string{"/color"}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			src := []byte(tt.src)
			got, err := JSONKeys(src, []byte(tt.fragment), tt.paths, tt.comments)
			if err == nil || got != nil {
				t.Fatalf("unsafe edit accepted: %s, %v", got, err)
			}
			if string(src) != tt.src {
				t.Fatal("mutated caller's source on error")
			}
		})
	}
}
