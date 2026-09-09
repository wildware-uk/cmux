package inject

import (
	"reflect"
	"strings"
	"testing"

	"github.com/wildware-uk/cmux/internal/tmux"
)

func payloads(ops []Op) []string {
	var out []string
	for _, o := range ops {
		if o.Kind == LoadBuffer {
			out = append(out, o.Payload)
		}
	}
	return out
}

func TestTextShape(t *testing.T) {
	ops := Text("/compact")
	if len(ops) != 3 {
		t.Fatalf("got %d ops, want 3", len(ops))
	}
	if ops[0].Kind != LoadBuffer || ops[1].Kind != PasteBuffer || ops[2].Kind != SendKeys {
		t.Fatalf("wrong op order: %v", ops)
	}
	if ops[2].Keys[0] != "Enter" {
		t.Errorf("last op sends %v, want Enter", ops[2].Keys)
	}
}

func TestTextAppendsTrailingSpace(t *testing.T) {
	// Without this the slash-command menu stays open and Enter picks a menu
	// entry instead of submitting.
	got := payloads(Text("/compact"))[0]
	if got != "/compact " {
		t.Errorf("payload = %q, want %q", got, "/compact ")
	}
}

func TestTextKeepsSpecialCharactersIntact(t *testing.T) {
	in := `/goal ship "the" parser — émoji 🚀 $HOME ` + "`x`"
	got := payloads(Text(in))[0]
	if got != in+" " {
		t.Errorf("payload = %q, want %q", got, in+" ")
	}
}

func TestTextCollapsesNewlines(t *testing.T) {
	got := payloads(Text("line one\nline two"))[0]
	if strings.Contains(got, "\n") {
		t.Fatalf("payload still contains a newline: %q", got)
	}
	if got != "line one line two " {
		t.Errorf("payload = %q", got)
	}
}

func TestSanitise(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"plain", "plain"},
		{"two  spaces", "two spaces"},
		{"a\nb", "a b"},
		{"a\r\nb", "a b"},
		{"a\tb", "a b"},
		{"  padded  ", "padded"},
		{"", ""},
	} {
		if got := Sanitise(tc.in); got != tc.want {
			t.Errorf("Sanitise(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestKeyIsRawWithNoSubmit(t *testing.T) {
	ops := Key("Escape")
	if len(ops) != 1 {
		t.Fatalf("got %d ops, want 1", len(ops))
	}
	if ops[0].Kind != SendKeys || !reflect.DeepEqual(ops[0].Keys, []string{"Escape"}) {
		t.Errorf("got %v, want a single Escape send-keys", ops[0])
	}
}

func TestRunIssuesTmuxCallsInOrder(t *testing.T) {
	f := &tmux.Fake{}
	if err := Run(t.Context(), f, "%12", Text("/compact")); err != nil {
		t.Fatal(err)
	}
	want := []string{
		`load-buffer "/compact "`,
		"paste-buffer -t %12",
		"send-keys -t %12 Enter",
	}
	if got := f.Ops(); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestRunKeyOnly(t *testing.T) {
	f := &tmux.Fake{}
	if err := Run(t.Context(), f, "%12", Key("Escape")); err != nil {
		t.Fatal(err)
	}
	want := []string{"send-keys -t %12 Escape"}
	if got := f.Ops(); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestOpStringForDryRun(t *testing.T) {
	ops := Text("/goal it's here")
	got := ops[0].String()
	want := `printf %s '/goal it'\''s here ' | tmux load-buffer -`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
