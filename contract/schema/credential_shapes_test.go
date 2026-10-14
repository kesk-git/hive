package schema

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

// Every string of a listed shape in these tests is built at run time from its
// prefix and a repeated character, so that no file holds one.

// tokens are the six token shapes with what a string of each is made of.
var tokens = []struct {
	name    string
	prefix  string
	lengths []int
	extra   byte // a byte the body admits besides letters and digits, or 0
}{
	{"github.pat-classic", "ghp_", []int{36}, 0},
	{"github.oauth-access", "gho_", []int{36}, 0},
	{"github.app-user-access", "ghu_", []int{36}, 0},
	{"github.app-installation-opaque", "ghs_", []int{36, 255}, 0},
	{"github.app-refresh-scanner40", "ghr_", []int{36}, 0},
	{"github.pat-fine-grained", "github_pat_", []int{82}, '_'},
}

var markers = map[string]string{
	"private-key.pkcs8":           "PRIVATE KEY",
	"private-key.pkcs8-encrypted": "ENCRYPTED PRIVATE KEY",
	"private-key.rsa":             "RSA PRIVATE KEY",
	"private-key.dsa":             "DSA PRIVATE KEY",
	"private-key.ec":              "EC PRIVATE KEY",
	"private-key.sm2":             "SM2 PRIVATE KEY",
	"private-key.openssh":         "OPENSSH PRIVATE KEY",
	"private-key.openpgp":         "PGP PRIVATE KEY BLOCK",
	"private-key.pgp2-secret":     "PGP SECRET KEY BLOCK",
}

func marker(what string) string { return "-----BEGIN " + what + "-----" }

func tokenOf(prefix string, n int) string { return prefix + strings.Repeat("a", n) }

func is(t *testing.T, s, want string) {
	t.Helper()
	if got := shapeOf(s); got != want {
		t.Errorf("shapeOf(%d bytes beginning %.4q) = %q, want %q", len(s), s, got, want)
	}
}

func TestTheListIsFifteenNamedShapes(t *testing.T) {
	names := ShapeNames()
	if len(names) != 15 {
		t.Fatalf("%d shapes", len(names))
	}
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			t.Errorf("%s is named twice", n)
		}
		seen[n] = true
	}
	for _, tok := range tokens {
		if !seen[tok.name] {
			t.Errorf("%s is not on the list", tok.name)
		}
	}
	for name := range markers {
		if !seen[name] {
			t.Errorf("%s is not on the list", name)
		}
	}
}

// The contract states the exact list: every name of it is in spec/records.md.
func TestContractNamesEveryShape(t *testing.T) {
	data, err := os.ReadFile("../../spec/records.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range ShapeNames() {
		if !strings.Contains(string(data), "`"+name+"`") {
			t.Errorf("spec/records.md does not name %s", name)
		}
	}
}

func TestTokenShapesMatchAtTheirExactLength(t *testing.T) {
	for _, tok := range tokens {
		t.Run(tok.name, func(t *testing.T) {
			for _, n := range tok.lengths {
				is(t, tokenOf(tok.prefix, n), tok.name)
				is(t, tokenOf(tok.prefix, n-1), "")
				is(t, tokenOf(tok.prefix, n+1), "")
			}
			n := tok.lengths[0]
			is(t, tokenOf(tok.prefix, n/2)+strings.Repeat("Z9", n/4+1), "") // wrong length
			// Every letter case and digit is in the alphabet.
			is(t, tok.prefix+strings.Repeat("aZ9", n)[:n], tok.name)
			// A byte outside the alphabet anywhere in the body is no match.
			for _, bad := range []string{" ", "+", "/", "=", "é", "\x00"} {
				body := tokenOf("", n)
				for _, at := range []int{0, n / 2, n - len(bad)} {
					is(t, tok.prefix+body[:at]+bad+body[at+len(bad):], "")
				}
			}
			underscore := tok.prefix + tokenOf("", n-1) + "_"
			if tok.extra == '_' {
				is(t, underscore, tok.name)
			} else {
				is(t, underscore, "")
			}
			// The prefix is case-sensitive.
			is(t, strings.ToUpper(tok.prefix)+tokenOf("", n), "")
			// A prefix alone, or in a sentence, is a mention.
			is(t, tok.prefix, "")
			is(t, "tokens begin with "+tok.prefix+" and go on", "")
			is(t, "tokens begin with "+tok.prefix+tokenOf("", 5)+"...", "")
		})
	}
}

func TestInstallationTokenHasTwoLengthsAndNoOther(t *testing.T) {
	const name = "github.app-installation-opaque"
	for n := 0; n <= 300; n++ {
		want := ""
		if n == 36 || n == 255 {
			want = name
		}
		is(t, tokenOf("ghs_", n), want)
	}
}

func TestTokenBoundaries(t *testing.T) {
	for _, tok := range tokens {
		t.Run(tok.name, func(t *testing.T) {
			s := tokenOf(tok.prefix, tok.lengths[0])
			// Outside the set on either side, or the string's end: a match.
			for _, around := range []string{" ", "\n", "\t", "\"", "'", "/", ":", "=", ",", ";", "(", ")", "<", ">", "é", " ", "\\", "`"} {
				is(t, around+s, tok.name)
				is(t, s+around, tok.name)
				is(t, around+s+around, tok.name)
				is(t, "x"+around+s+around+"y", tok.name)
			}
			// A byte of the token set on either side is part of a longer run.
			for _, joined := range []string{".", "-", "_", "a", "Z", "0"} {
				is(t, joined+s, "")
				is(t, s+joined, "")
				is(t, joined+s+joined, "")
			}
			// No fixed-length slice is taken from a longer run.
			is(t, "x"+s, "")
			is(t, s+"x", "")
			is(t, s+s, "")
			is(t, s+"."+s[:3], "")
			// Two candidates beside each other are separate when something
			// outside the set lies between them.
			is(t, "nothing here "+s+" nor there", tok.name)
			is(t, s+" "+s, tok.name)
			// A run that is not a candidate does not hide a later one.
			is(t, tokenOf(tok.prefix, 5)+" "+s, tok.name)
		})
	}
}

func TestLineShapes(t *testing.T) {
	for name, what := range markers {
		t.Run(name, func(t *testing.T) {
			m := marker(what)
			for _, ok := range []string{
				m, m + "\n", m + "\r\n", m + "\nMIIE\n", m + "\r\nMIIE\r\n",
				"\n" + m, "text\n" + m, "text\r\n" + m + "\r\n", "a\nb\n" + m + "\nc",
				"\n\n" + m + "\n\n",
			} {
				is(t, ok, name)
			}
			for _, no := range []string{
				" " + m, "\t" + m, m + " ", m + "\t", " " + m + " ", "x " + m, m + " x",
				"-" + m, m + "-", "-----" + m, m + "-----",
				m + "\r", m + "\r\r\n", m + "\r \n", "x\r" + m, "x\r" + m + "\n",
				"text " + m + "\nrest", "a key: " + m,
				strings.ToLower(m), "-----BEGIN " + what + "----", "----BEGIN " + what + "-----",
				"-----BEGIN  " + what + "-----", "-----BEGIN " + what + "-----x",
				"-----END " + what + "-----", "-----BEGIN " + what + " BLOCK-----x",
			} {
				is(t, no, "")
			}
			// A marker split across two lines is no marker.
			is(t, m[:len(m)/2]+"\n"+m[len(m)/2:], "")
		})
	}
	// A different marker's text is not another's: BEGIN PRIVATE KEY is not a
	// line of the longer markers it ends.
	is(t, "-----BEGIN ENCRYPTED PRIVATE KEY-----", "private-key.pkcs8-encrypted")
	is(t, "-----BEGIN EC PRIVATE KEY-----\n", "private-key.ec")
	// Other armour is not on the list.
	for _, other := range []string{"CERTIFICATE", "PUBLIC KEY", "PGP PUBLIC KEY BLOCK", "PGP MESSAGE", "CERTIFICATE REQUEST", "X509 CRL"} {
		is(t, marker(other), "")
	}
}

// A shape is found in a value or a member name after ordinary parsing, so the
// spelling of an escape changes nothing, and nothing is decoded beyond it.
func TestMatchingIsOnTheParsedString(t *testing.T) {
	r := mustOne(t, `"type":"object","additionalProperties":false,"properties":{"a":{"type":"string"},"b":{"type":"string"}}`)
	tok := tokenOf("ghp_", 36)
	esc := `ghp_` + tok[4:] // the same string, spelled with an escape
	pem := marker("RSA PRIVATE KEY")
	for _, c := range []struct {
		name, doc, shape string
	}{
		{"value", `{"a":"` + tok + `"}`, "github.pat-classic"},
		{"escaped value", `{"a":"` + esc + `"}`, "github.pat-classic"},
		{"escaped slash around", `{"a":"x\/` + tok + `\/y"}`, "github.pat-classic"},
		{"escaped line feed", `{"a":"x\n` + pem + `\ny"}`, "private-key.rsa"},
		{"escaped carriage return", `{"a":"x\r\n` + pem + `\r\ny"}`, "private-key.rsa"},
		{"escaped quote beside", `{"a":"\"` + tok + `\""}`, "github.pat-classic"},
		{"escaped space beside", `{"a":" ` + tok + ` "}`, "github.pat-classic"},
		{"escaped period beside", `{"a":".` + tok + `"}`, ""},
		{"backslash-n is no line feed", `{"a":"x\\n` + pem + `"}`, ""},
		{"percent encoding is not decoded", `{"a":"` + strings.Replace(tok, "ghp_", "ghp%5F", 1) + `"}`, ""},
		{"base64 is not decoded", `{"a":"Z2hwXw=="}`, ""},
		{"member name", `{"` + tok + `":1}`, "github.pat-classic"},
		{"escaped member name", `{"` + esc + `":1}`, "github.pat-classic"},
		{"nested", `{"a":{"b":[1,{"c":["` + tok + `"]}]}}`, "github.pat-classic"},
		{"nothing", `{"a":"ghp_ is a prefix","b":"x"}`, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := r.ValidateOwned(testID, []byte(c.doc))
			var se *ShapeError
			if c.shape == "" {
				if err != nil {
					t.Errorf("refused: %v", err)
				}
				return
			}
			if !errors.As(err, &se) || se.Shape != c.shape {
				t.Fatalf("got %v, want the shape %s", err, c.shape)
			}
			if strings.Contains(err.Error(), tok) || strings.Contains(se.Field, tok) {
				t.Errorf("the refusal holds the text: %v", err)
			}
		})
	}
}

// Both boundaries apply within one string, never across fields.
func TestNothingMatchesAcrossFields(t *testing.T) {
	tok := tokenOf("ghp_", 36)
	for name, tree := range map[string]any{
		"two values":     map[string]any{"a": tok[:20], "b": tok[20:]},
		"two items":      []any{tok[:20], tok[20:]},
		"name and value": map[string]any{tok[:20]: tok[20:]},
		"key lines":      []any{marker("RSA PRIVATE KEY")[:10], marker("RSA PRIVATE KEY")[10:]},
	} {
		if err := CheckShapes(tree); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

const shapedSchema = `
"type":"object","additionalProperties":false,
"properties":{
  "text":{"type":"string"},
  "tags":{"type":"array","items":{"type":"string"}},
  "items":{"type":"array","items":{"type":"object","additionalProperties":false,"properties":{"note":{"type":"string"},"unknown":{"type":"object"}}}},
  "unknown":{"type":"object"},
  "refd":{"$ref":"urn:hive:schema:t:v1#/$defs/inner"},
  "either":{"anyOf":[{"type":"object","additionalProperties":false,"properties":{"left":{"type":"string"}}},{"type":"object","additionalProperties":false,"properties":{"right":{"type":"string"}}}]}
},
"$defs":{"inner":{"type":"object","additionalProperties":false,"properties":{"deep":{"type":"string"}}}}`

// A refusal names the nearest declared field that contains the match, or the
// root, never the text and never an arbitrary member name.
func TestRefusalNamesTheNearestDeclaredField(t *testing.T) {
	r := mustOne(t, shapedSchema)
	tok := tokenOf("ghp_", 36)
	for _, c := range []struct{ name, doc, field string }{
		{"declared field", `{"text":"` + tok + `"}`, "/text"},
		{"array of strings", `{"tags":["a","` + tok + `"]}`, "/tags"},
		{"declared field of an element", `{"items":[{},{"note":"` + tok + `"}]}`, "/items/1/note"},
		{"undeclared member of an element", `{"items":[{"unknown":{"x":"` + tok + `"}}]}`, "/items/0/unknown"},
		{"open object's value", `{"unknown":{"anything":"` + tok + `"}}`, "/unknown"},
		{"open object's deep value", `{"unknown":{"a":[{"b":"` + tok + `"}]}}`, "/unknown"},
		{"open object's member name", `{"unknown":{"` + tok + `":1}}`, "/unknown"},
		{"undeclared member name at the root", `{"` + tok + `":1}`, ""},
		{"undeclared member value at the root", `{"stray":"` + tok + `"}`, ""},
		{"referenced schema", `{"refd":{"deep":"` + tok + `"}}`, "/refd/deep"},
		{"combinator branch", `{"either":{"right":"` + tok + `"}}`, "/either/right"},
		{"member name of a declared object", `{"refd":{"` + tok + `":1}}`, "/refd"},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := r.ValidateOwned(testID, []byte(c.doc))
			var se *ShapeError
			if !errors.As(err, &se) {
				t.Fatalf("got %v", err)
			}
			if se.Field != c.field || se.Shape != "github.pat-classic" {
				t.Errorf("field %q shape %q, want %q", se.Field, se.Shape, c.field)
			}
			if strings.Contains(err.Error(), tok) {
				t.Errorf("the refusal holds the text: %v", err)
			}
			if strings.Contains(se.Under("/body").Field, "//") || se.Under("/body").Field != "/body"+c.field {
				t.Errorf("under: %q", se.Under("/body").Field)
			}
		})
	}
}

// A document holding a shape is refused for it whatever else is wrong with it,
// and one that does not is checked against the schema as before.
func TestShapeIsRefusedBeforeTheSchemaIsApplied(t *testing.T) {
	r := mustOne(t, shapedSchema)
	tok := tokenOf("ghp_", 36)
	var se *ShapeError
	if err := r.ValidateOwned(testID, []byte(`{"text":5,"stray":"`+tok+`"}`)); !errors.As(err, &se) {
		t.Errorf("got %v", err)
	}
	var ve *ValidationError
	if err := r.ValidateOwned(testID, []byte(`{"text":5}`)); !errors.As(err, &ve) {
		t.Errorf("got %v", err)
	}
	var syn *SyntaxError
	if err := r.ValidateOwned(testID, []byte(`{"text":"`+tok)); !errors.As(err, &syn) {
		t.Errorf("got %v", err)
	}
	if err := r.ValidateOwned(testID, []byte(`{"text":"ghp_ in prose"}`)); err != nil {
		t.Errorf("got %v", err)
	}
	if err := r.ValidateOwned("urn:hive:schema:nothing:v1", []byte(`{}`)); !errors.Is(err, ErrUnknownSchema) {
		t.Errorf("got %v", err)
	}
	// Validate alone is the schema's, as it was.
	if err := r.Validate(testID, []byte(`{"text":"`+tok+`"}`)); err != nil {
		t.Errorf("Validate refused a document that only holds a shape: %v", err)
	}
	// The ordinary parse is the record's: a member named twice is not a record.
	if err := r.ValidateOwned(testID, []byte(`{"text":"a","text":"b"}`)); !errors.As(err, &syn) {
		t.Errorf("got %v", err)
	}
}

func TestRegistryCheckShapes(t *testing.T) {
	r := mustOne(t, shapedSchema)
	var v any
	if err := json.Unmarshal([]byte(`{"tags":["`+tokenOf("gho_", 36)+`"]}`), &v); err != nil {
		t.Fatal(err)
	}
	var se *ShapeError
	if err := r.CheckShapes(testID, v); !errors.As(err, &se) || se.Field != "/tags" || se.Shape != "github.oauth-access" {
		t.Errorf("got %v", err)
	}
	if err := r.CheckShapes("urn:hive:schema:nothing:v1", v); !errors.Is(err, ErrUnknownSchema) {
		t.Errorf("got %v", err)
	}
	if err := CheckShapes(v); !errors.As(err, &se) || se.Field != "" || se.Where() != "the document root" {
		t.Errorf("got %v", err)
	}
	if err := CheckShapes(nil); err != nil {
		t.Errorf("got %v", err)
	}
}

func TestHoldsShape(t *testing.T) {
	tok := tokenOf("ghp_", 36)
	for s, want := range map[string]bool{
		tok: true, "cut at the end " + tok + " and after": true, tok[:30]: false, "": false,
		marker("EC PRIVATE KEY") + "\n": true, "ghp_ alone": false,
	} {
		if got := HoldsShape(s); got != want {
			t.Errorf("HoldsShape(%.12q) = %v, want %v", s, got, want)
		}
	}
}
