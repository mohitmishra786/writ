package canon

import (
	"errors"
	"testing"
)

func TestKeyOrderingAndWhitespace(t *testing.T) {
	got, err := Marshal(map[string]any{"b": 1, "a": []any{true, nil, "x"}, "c": map[string]any{"z": 0, "y": -0}})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":[true,null,"x"],"b":1,"c":{"y":0,"z":0}}`
	if string(got) != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestStructsAreCanonicalisedLikeMaps(t *testing.T) {
	type q struct {
		Total int64  `json:"total"`
		Payee string `json:"payee"`
	}
	a, _ := Marshal(q{Total: 18600, Payee: "supplier:bean-co"})
	b, _ := Marshal(map[string]any{"payee": "supplier:bean-co", "total": 18600})
	if string(a) != string(b) {
		t.Fatalf("%s != %s", a, b)
	}
}

func TestStringEscapingMatchesJSONStringify(t *testing.T) {
	got, _ := Marshal("<a&b> \n\x01é")
	want := "\"<a&b> \\n\\u0001é\""
	if string(got) != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// RFC 8785 §3.2.3: keys sort by UTF-16 code units, so U+FB33 (one unit,
// 0xFB33) sorts after U+1F600 (surrogate pair starting 0xD83D).
func TestUTF16KeyOrder(t *testing.T) {
	got, _ := Marshal(map[string]any{"דּ": 1, "\U0001F600": 2})
	want := "{\"\U0001F600\":2,\"דּ\":1}"
	if string(got) != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestRejectsFloats(t *testing.T) {
	if _, err := Marshal(map[string]any{"price": 19.99}); !errors.Is(err, ErrNonInteger) {
		t.Fatalf("expected ErrNonInteger, got %v", err)
	}
}
