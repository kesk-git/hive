package schema

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// compilePattern compiles a `pattern` after holding it to the syntax common to
// RE2 and ECMAScript, so that the same schema gives the same answer in Go and
// in a browser. Go's engine accepts more than a browser does (named groups,
// flag groups, POSIX classes, \p, \z, \Q...\E, \x{...}) and a browser accepts
// more than Go does (lookaround, back-references, \u escapes); every one of
// those is refused here, and so is a bare brace or bracket whose meaning the
// two engines settle differently. \s, \S and an unescaped dot are refused as
// well, for they match different characters in the two engines (Unicode
// white space and line terminators); a pattern writes the class out.
func compilePattern(src string) (*regexp.Regexp, error) {
	if err := commonSyntax(src); err != nil {
		return nil, err
	}
	re, err := regexp.Compile(src)
	if err != nil {
		return nil, fmt.Errorf("not a regular expression: %w", err)
	}
	return re, nil
}

const quantifierMax = 1000 // the repeat count RE2 allows

func commonSyntax(p string) error {
	inClass := false
	classStart := 0
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c == '\\':
			i++
			if i >= len(p) {
				return fmt.Errorf("ends in a lone backslash")
			}
			e := p[i]
			switch {
			case strings.IndexByte(`\^$.*+?()[]{}|/-`, e) >= 0:
			case strings.IndexByte("dDwWnrtfv", e) >= 0:
			case e == 's' || e == 'S':
				return fmt.Errorf("\\%c matches different characters in RE2 and ECMAScript; write the class out", e)
			case (e == 'b' || e == 'B') && !inClass:
			case e == 'x':
				if i+2 >= len(p) || !isHex(p[i+1]) || !isHex(p[i+2]) {
					return fmt.Errorf("\\x needs exactly two hexadecimal digits")
				}
				i += 2
			default:
				return fmt.Errorf("escape \\%c is not in the syntax common to RE2 and ECMAScript", e)
			}
		case inClass:
			switch c {
			case '[':
				return fmt.Errorf("unescaped [ inside a class")
			case ']':
				if i == classStart {
					return fmt.Errorf("a class may not begin with ]")
				}
				inClass = false
			}
		case c == '.':
			return fmt.Errorf("an unescaped . matches different characters in RE2 and ECMAScript; write the class out")
		case c == '[':
			inClass = true
			classStart = i + 1
			if classStart < len(p) && p[classStart] == '^' {
				classStart++
				i++
			}
		case c == ']' || c == '}':
			return fmt.Errorf("unescaped %c outside a class", c)
		case c == '(':
			if i+1 < len(p) && p[i+1] == '?' {
				if !strings.HasPrefix(p[i:], "(?:") {
					return fmt.Errorf("only (?: may follow ( ?")
				}
				i += 2
			}
		case c == '{':
			n, err := quantifierLen(p[i:])
			if err != nil {
				return err
			}
			i += n - 1
		}
	}
	if inClass {
		return fmt.Errorf("unterminated class")
	}
	return nil
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// quantifierLen measures a {n}, {n,} or {n,m} that starts s, and refuses a
// brace that is not one: a browser reads it as a literal and a careless
// reader may read it as something else.
func quantifierLen(s string) (int, error) {
	end := strings.IndexByte(s, '}')
	if end < 0 {
		return 0, fmt.Errorf("unescaped { that opens no quantifier")
	}
	body := s[1:end]
	lo, hi, ranged := strings.Cut(body, ",")
	n, err := strconv.Atoi(lo)
	if err != nil || lo == "" || lo[0] == '+' || lo[0] == '-' {
		return 0, fmt.Errorf("unescaped { that opens no quantifier")
	}
	top := n
	if ranged && hi != "" {
		if top, err = strconv.Atoi(hi); err != nil || hi[0] == '+' || hi[0] == '-' {
			return 0, fmt.Errorf("unescaped { that opens no quantifier")
		}
		if top < n {
			return 0, fmt.Errorf("quantifier {%s} counts down", body)
		}
	}
	if n > quantifierMax || top > quantifierMax {
		return 0, fmt.Errorf("quantifier {%s} exceeds %d", body, quantifierMax)
	}
	return end + 1, nil
}
