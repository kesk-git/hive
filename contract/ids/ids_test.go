package ids

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/kesk-git/hive/contract/clock"
	"github.com/kesk-git/hive/spec"
)

type vectorFile struct {
	Mint []struct {
		Name    string `json:"name"`
		Prefix  Prefix `json:"prefix"`
		TimeMS  uint64 `json:"time_ms"`
		Entropy string `json:"entropy"`
		ID      string `json:"id"`
	} `json:"mint"`
	Invalid []struct {
		Name  string `json:"name"`
		ID    string `json:"id"`
		Error string `json:"error"`
	} `json:"invalid"`
}

func loadVectors(t *testing.T) vectorFile {
	t.Helper()
	data, err := os.ReadFile("testdata/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v vectorFile
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestMintVectors(t *testing.T) {
	for _, v := range loadVectors(t).Mint {
		t.Run(v.Name, func(t *testing.T) {
			raw, err := hex.DecodeString(v.Entropy)
			if err != nil || len(raw) != 10 {
				t.Fatalf("bad vector entropy %q", v.Entropy)
			}
			clk := clock.NewFake(time.UnixMilli(int64(v.TimeMS)))
			got, err := MintFrom(clk, bytes.NewReader(raw), v.Prefix)
			if err != nil || got != v.ID {
				t.Fatalf("MintFrom = %q, %v; want %q", got, err, v.ID)
			}
			if err := Check(got, v.Prefix); err != nil {
				t.Fatalf("a minted identifier fails its own check: %v", err)
			}
			p, ms, e := split(got)
			if p != v.Prefix || ms != v.TimeMS || hex.EncodeToString(e[:]) != v.Entropy {
				t.Fatalf("split(%q) = %q, %d, %x; want %q, %d, %s", got, p, ms, e, v.Prefix, v.TimeMS, v.Entropy)
			}
		})
	}
}

// refDecode is a second implementation, with arbitrary-precision arithmetic,
// that the vectors are checked against so the two cannot share a mistake.
func refDecode(ulid string) (ms uint64, entropy string) {
	n := new(big.Int)
	for _, c := range ulid {
		n.Lsh(n, 5)
		n.Or(n, big.NewInt(int64(strings.IndexRune(alphabet, c))))
	}
	mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 80), big.NewInt(1))
	low := new(big.Int).And(n, mask)
	ms = new(big.Int).Rsh(n, 80).Uint64()
	return ms, hex.EncodeToString(low.FillBytes(make([]byte, 10)))
}

func TestVectorsAgreeWithReferenceDecoder(t *testing.T) {
	for _, v := range loadVectors(t).Mint {
		ms, e := refDecode(strings.TrimPrefix(v.ID, string(v.Prefix)+"_"))
		if ms != v.TimeMS || e != v.Entropy {
			t.Errorf("%s: reference decodes %q to %d, %s; vector says %d, %s", v.Name, v.ID, ms, e, v.TimeMS, v.Entropy)
		}
	}
}

func TestInvalidVectors(t *testing.T) {
	want := map[string]error{"form": ErrForm, "prefix": ErrPrefix}
	for _, v := range loadVectors(t).Invalid {
		t.Run(v.Name, func(t *testing.T) {
			if _, err := Parse(v.ID); !errors.Is(err, want[v.Error]) {
				t.Fatalf("Parse(%q) error = %v; want %v", v.ID, err, want[v.Error])
			}
			if Valid(v.ID) {
				t.Fatalf("Valid(%q) = true", v.ID)
			}
		})
	}
}

func TestMintRandomAndUnique(t *testing.T) {
	clk := clock.NewFake(time.UnixMilli(1729886278806))
	seen := map[string]bool{}
	for i := 0; i < 2000; i++ {
		id, err := Mint(clk, Task)
		if err != nil {
			t.Fatal(err)
		}
		if err := Check(id, Task); err != nil {
			t.Fatal(err)
		}
		if seen[id] {
			t.Fatalf("identifier minted twice at one instant: %s", id)
		}
		seen[id] = true
	}
}

func TestMintRefuses(t *testing.T) {
	clk := clock.NewFake(time.UnixMilli(1729886278806))
	if _, err := Mint(clk, "nope"); !errors.Is(err, ErrPrefix) {
		t.Errorf("unregistered prefix: %v", err)
	}
	if _, err := Mint(clk, ""); !errors.Is(err, ErrPrefix) {
		t.Errorf("empty prefix: %v", err)
	}
	for name, at := range map[string]time.Time{
		"before 1970":     time.UnixMilli(-1),
		"after 48 bits":   time.UnixMilli(maxMillis + 1),
		"far in the past": time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC),
	} {
		if _, err := MintFrom(clock.NewFake(at), rand.Reader, Task); !errors.Is(err, ErrTime) {
			t.Errorf("%s: error = %v; want ErrTime", name, err)
		}
	}
	if _, err := MintFrom(clk, bytes.NewReader(make([]byte, 9)), Task); err == nil {
		t.Error("short entropy source was accepted")
	}
}

func TestCheckWrongKind(t *testing.T) {
	if err := Check("ws_01jb2m3n4p5q6r7s8t9vwxyz0a", Project); !errors.Is(err, ErrWrongPrefix) {
		t.Fatalf("error = %v; want ErrWrongPrefix", err)
	}
	if err := Check("ws_01jb2m3n4p5q6r7s8t9vwxyz0a", Workspace); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryForm(t *testing.T) {
	seen := map[Prefix]bool{}
	for _, p := range Prefixes() {
		if !ValidPrefixForm(p) {
			t.Errorf("registered prefix %q has not the prefix form", p)
		}
		if seen[p] {
			t.Errorf("prefix %q registered twice", p)
		}
		seen[p] = true
		if !Registered(p) {
			t.Errorf("Registered(%q) = false", p)
		}
	}
	for _, p := range []Prefix{"", "9a", "A", "ab-c", "abcdefghi", "a_b", "é"} {
		if ValidPrefixForm(p) {
			t.Errorf("ValidPrefixForm(%q) = true", p)
		}
	}
	got := Prefixes()
	got[0] = "changed"
	if Prefixes()[0] == "changed" {
		t.Error("Prefixes returns the registry itself")
	}
}

// The registry is the table of spec/identities.md: the same prefixes, in the
// same order.
func TestRegistryAgreesWithSpecification(t *testing.T) {
	data, err := os.ReadFile("../../spec/identities.md")
	if err != nil {
		t.Fatal(err)
	}
	_, table, ok := strings.Cut(string(data), "| Prefix | Identifies | Minted by |")
	if !ok {
		t.Fatal("identifier table not found in spec/identities.md")
	}
	row := regexp.MustCompile("^\\| `([a-z][a-z0-9]*)_` \\|")
	var spec []Prefix
	for _, line := range strings.Split(table, "\n")[2:] {
		m := row.FindStringSubmatch(line)
		if m == nil {
			break
		}
		spec = append(spec, Prefix(m[1]))
	}
	got := Prefixes()
	if len(spec) != len(got) {
		t.Fatalf("specification registers %d prefixes %v, library %d %v", len(spec), spec, len(got), got)
	}
	for i := range spec {
		if spec[i] != got[i] {
			t.Errorf("row %d: specification %q, library %q", i+1, spec[i], got[i])
		}
	}
}

// Every identifier the library mints satisfies the schema pattern of its
// kind, and the pattern refuses every other kind and upper case.
func TestIdentifiersAgreeWithCommonSchema(t *testing.T) {
	data, err := spec.Schemas.ReadFile("schemas/common.v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Defs map[string]struct {
			Pattern string `json:"pattern"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFake(time.UnixMilli(1729886278806))
	typed := regexp.MustCompile(`^\^([a-z][a-z0-9]*)_\[0-7\]\[0-9a-hjkmnp-tv-z\]\{25\}\$$`)
	checked := map[Prefix]bool{}
	var names []string
	for name := range doc.Defs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		pattern := doc.Defs[name].Pattern
		if m := typed.FindStringSubmatch(pattern); m != nil {
			p := Prefix(m[1])
			if !Registered(p) {
				t.Errorf("%s: schema names prefix %q, which the library does not register", name, p)
				continue
			}
			re := regexp.MustCompile(pattern)
			id, err := Mint(clk, p)
			if err != nil || !re.MatchString(id) {
				t.Errorf("%s: minted %q, %v does not match %s", name, id, err, pattern)
			}
			if re.MatchString(strings.ToUpper(id)) {
				t.Errorf("%s: pattern accepts upper case", name)
			}
			checked[p] = true
		}
		if name == "any_id" {
			re := regexp.MustCompile(pattern)
			for _, p := range Prefixes() {
				id, _ := Mint(clk, p)
				if !re.MatchString(id) {
					t.Errorf("any_id does not match %q", id)
				}
			}
		}
	}
	if len(checked) == 0 {
		t.Fatal("no typed identifier pattern found in the common schema")
	}
}

func TestNumbers(t *testing.T) {
	for _, n := range []int64{1, 2, 1000, MaxNumber} {
		if err := CheckNumber(n); err != nil {
			t.Errorf("CheckNumber(%d) = %v", n, err)
		}
	}
	for _, n := range []int64{0, -1, MaxNumber + 1, 1 << 62} {
		if err := CheckNumber(n); !errors.Is(err, ErrNumber) {
			t.Errorf("CheckNumber(%d) = %v; want ErrNumber", n, err)
		}
	}
	if n, err := NextNumber(0); n != 1 || err != nil {
		t.Errorf("NextNumber(0) = %d, %v", n, err)
	}
	if n, err := NextNumber(41); n != 42 || err != nil {
		t.Errorf("NextNumber(41) = %d, %v", n, err)
	}
	for _, n := range []int64{-1, MaxNumber, MaxNumber + 1} {
		if _, err := NextNumber(n); !errors.Is(err, ErrNumber) {
			t.Errorf("NextNumber(%d) error = %v; want ErrNumber", n, err)
		}
	}
}

func FuzzParseNeverPanics(f *testing.F) {
	for _, s := range []string{"", "ws_01jb2m3n4p5q6r7s8t9vwxyz0a", "_", "a_" + strings.Repeat("z", 26)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		p, err := Parse(s)
		if err != nil {
			return
		}
		pfx, ms, e := split(s)
		if pfx != p {
			t.Fatalf("prefix %q != %q", pfx, p)
		}
		if again := compose(p, ms, e); again != s {
			t.Fatalf("compose(split(%q)) = %q", s, again)
		}
	})
}
