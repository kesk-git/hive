package envelope

import (
	"strings"
	"testing"
)

const provisionRequest = "urn:hive:schema:ops:hive-fleet.provision.request:v1"

// shaped are strings of listed shapes, built at run time from a prefix and a
// repeated character.
var shaped = map[string]string{
	"github.pat-classic":             "ghp_" + strings.Repeat("a", 36),
	"github.oauth-access":            "gho_" + strings.Repeat("b", 36),
	"github.app-user-access":         "ghu_" + strings.Repeat("c", 36),
	"github.app-installation-opaque": "ghs_" + strings.Repeat("d", 255),
	"github.app-refresh-scanner40":   "ghr_" + strings.Repeat("e", 36),
	"github.pat-fine-grained":        "github_pat_" + strings.Repeat("f", 82),
	"private-key.openssh":            "-----BEGIN OPENSSH PRIVATE KEY-----",
	"private-key.pgp2-secret":        "-----BEGIN PGP SECRET KEY BLOCK-----\r\n",
	"private-key.pkcs8-encrypted":    "text\n-----BEGIN ENCRYPTED PRIVATE KEY-----\nrest",
}

func bodyNamed(name string) map[string]any {
	return map[string]any{"home": map[string]any{"role": "hive", "name": name}}
}

// A body holding a string of a listed shape is refused whole as invalid with
// secret_shape_recognized: the sentence names the field and the shape, and the
// text is in neither the message nor the diagnostic.
func TestValidateBodyRefusesAListedShape(t *testing.T) {
	for shape, text := range shaped {
		t.Run(shape, func(t *testing.T) {
			note := "a note: " + text + " ."
			if strings.HasPrefix(shape, "private-key.") {
				note = text
			}
			err := ValidateBody(bodyNamed(note), provisionRequest)
			failure(t, err, Invalid, CodeSecretShapeRecognized)
			f := AsFailure(err)
			if !strings.Contains(f.Err.Message, "/home/name") || !strings.Contains(f.Err.Message, shape) {
				t.Errorf("message %q", f.Err.Message)
			}
			if d := f.Diagnostic(); d != nil && strings.Contains(strings.Join(d.Chain, "\n"), text) {
				t.Errorf("diagnostic %v", d.Chain)
			}
			if strings.Contains(f.Err.Message, text) {
				t.Errorf("the message holds the text: %q", f.Err.Message)
			}
		})
	}
}

// A member name is checked as a value is, and is named by the nearest declared
// field that holds it, never by itself.
func TestValidateBodyRefusesAShapeInAMemberName(t *testing.T) {
	text := shaped["github.pat-classic"]
	body := bodyNamed("A home")
	body["home"].(map[string]any)[text] = "x"
	failure(t, ValidateBody(body, provisionRequest), Invalid, CodeSecretShapeRecognized)
	f := AsFailure(ValidateBody(body, provisionRequest))
	if !strings.Contains(f.Err.Message, "at /home") || strings.Contains(f.Err.Message, text) {
		t.Errorf("message %q", f.Err.Message)
	}
	root := bodyNamed("A home")
	root[text] = "x"
	f = AsFailure(ValidateBody(root, provisionRequest))
	if f == nil || f.Err.Code != CodeSecretShapeRecognized || !strings.Contains(f.Err.Message, "the root of the body") {
		t.Errorf("failure %v", f)
	}
}

// A prefix without the full shape passes, a shape is refused before the
// schema's own verdict, and a body without one is judged by the schema as it
// was.
func TestValidateBodyKeepsWhatIsNotAShape(t *testing.T) {
	for _, s := range []string{"ghp_", "ghp_ is the prefix of a classic token", "ghp_" + strings.Repeat("a", 35), "ghp_" + strings.Repeat("a", 37), "x" + shaped["github.pat-classic"], shaped["github.pat-classic"] + "-1"} {
		if err := ValidateBody(bodyNamed(s), provisionRequest); err != nil {
			t.Errorf("%.20q: %v", s, err)
		}
	}
	body := bodyNamed(shaped["github.oauth-access"])
	body["home"].(map[string]any)["role"] = "nest"
	failure(t, ValidateBody(body, provisionRequest), Invalid, CodeSecretShapeRecognized)
	body = bodyNamed("A home")
	body["home"].(map[string]any)["role"] = "nest"
	failure(t, ValidateBody(body, provisionRequest), Invalid, CodeSchemaViolation)
}
