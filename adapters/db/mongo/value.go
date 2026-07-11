package mongo

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/no-today/aidev-clis/internal/core/errs"
)

// ParseValue parses one relaxed-JSON / mongosh value into a bson value.
func ParseValue(input string) (any, error) {
	p := &valParser{s: []rune(input)}
	p.ws()
	if p.pos >= len(p.s) {
		return nil, p.errf("unexpected end of input")
	}
	v, err := p.value()
	if err != nil {
		return nil, err
	}
	p.ws()
	if p.pos != len(p.s) {
		return nil, p.errf("unexpected trailing input")
	}
	return v, nil
}

type valParser struct {
	s   []rune
	pos int
}

func (p *valParser) errf(format string, a ...any) error {
	return errs.Config("MONGO_PARSE", fmt.Sprintf("parse error at %d: %s", p.pos, fmt.Sprintf(format, a...)))
}

// ws skips whitespace and JS comments (// line and /* block */).
func (p *valParser) ws() {
	for p.pos < len(p.s) {
		switch {
		case p.s[p.pos] == ' ' || p.s[p.pos] == '\t' || p.s[p.pos] == '\n' || p.s[p.pos] == '\r':
			p.pos++
		case p.pos+1 < len(p.s) && p.s[p.pos] == '/' && p.s[p.pos+1] == '/':
			// line comment — skip to end of line
			p.pos += 2
			for p.pos < len(p.s) && p.s[p.pos] != '\n' {
				p.pos++
			}
		case p.pos+1 < len(p.s) && p.s[p.pos] == '/' && p.s[p.pos+1] == '*':
			// block comment — skip to */
			p.pos += 2
			for p.pos+1 < len(p.s) {
				if p.s[p.pos] == '*' && p.s[p.pos+1] == '/' {
					p.pos += 2
					break
				}
				p.pos++
			}
		default:
			return
		}
	}
}

func (p *valParser) peek() rune {
	if p.pos < len(p.s) {
		return p.s[p.pos]
	}
	return 0
}

func (p *valParser) value() (any, error) {
	p.ws()
	if p.pos >= len(p.s) {
		return nil, p.errf("unexpected end of input")
	}
	c := p.s[p.pos]
	switch {
	case c == '{':
		return p.object()
	case c == '[':
		return p.array()
	case c == '"' || c == '\'':
		return p.str()
	case c == '/':
		return p.regex()
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	default:
		return p.identOrHelper()
	}
}

func (p *valParser) object() (any, error) {
	p.pos++ // consume {
	d := bson.D{}
	p.ws()
	if p.peek() == '}' {
		p.pos++
		return d, nil
	}
	// check for leading comma: {,} is malformed
	if p.peek() == ',' {
		return nil, p.errf("unexpected ',' in object")
	}
	for {
		p.ws()
		// if we see '}' here the previous iteration consumed a trailing comma
		if p.peek() == '}' {
			p.pos++
			return d, nil
		}
		key, err := p.key()
		if err != nil {
			return nil, err
		}
		p.ws()
		if p.peek() != ':' {
			return nil, p.errf("expected ':' after key %q", key)
		}
		p.pos++
		val, err := p.value()
		if err != nil {
			return nil, err
		}
		d = append(d, bson.E{Key: key, Value: val})
		p.ws()
		switch p.peek() {
		case ',':
			p.pos++
			p.ws()
			if p.peek() == '}' { // trailing comma
				p.pos++
				return d, nil
			}
			// double comma: {a:1,,b:2}
			if p.peek() == ',' {
				return nil, p.errf("unexpected ',' in object")
			}
		case '}':
			p.pos++
			return d, nil
		default:
			return nil, p.errf("expected ',' or '}' in object")
		}
	}
}

func (p *valParser) array() (any, error) {
	p.pos++ // consume [
	a := bson.A{}
	p.ws()
	if p.peek() == ']' {
		p.pos++
		return a, nil
	}
	for {
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		a = append(a, v)
		p.ws()
		switch p.peek() {
		case ',':
			p.pos++
			p.ws()
			if p.peek() == ']' { // trailing comma
				p.pos++
				return a, nil
			}
		case ']':
			p.pos++
			return a, nil
		default:
			return nil, p.errf("expected ',' or ']' in array")
		}
	}
}

// key reads an object key: a quoted string or an unquoted identifier
// ([A-Za-z0-9_$.] — covers _id, $gt, dotted paths).
func (p *valParser) key() (string, error) {
	if c := p.peek(); c == '"' || c == '\'' {
		s, err := p.str()
		if err != nil {
			return "", err
		}
		return s.(string), nil
	}
	start := p.pos
	for p.pos < len(p.s) && isKeyRune(p.s[p.pos]) {
		p.pos++
	}
	if p.pos == start {
		return "", p.errf("expected a key")
	}
	return string(p.s[start:p.pos]), nil
}

func isKeyRune(r rune) bool {
	return r == '_' || r == '$' || r == '.' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

func (p *valParser) str() (any, error) {
	q := p.s[p.pos]
	p.pos++
	var b strings.Builder
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		if c == q {
			p.pos++
			return b.String(), nil
		}
		if c == '\\' && p.pos+1 < len(p.s) {
			p.pos++
			e := p.s[p.pos]
			switch e {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '"', '\'', '\\', '/':
				b.WriteRune(e)
			case 'u':
				if p.pos+4 >= len(p.s) {
					return nil, p.errf("bad \\u escape")
				}
				n, err := strconv.ParseInt(string(p.s[p.pos+1:p.pos+5]), 16, 32)
				if err != nil {
					return nil, p.errf("bad \\u escape")
				}
				b.WriteRune(rune(n))
				p.pos += 4
			case 'x':
				// \xNN hex escape
				if p.pos+2 >= len(p.s) {
					return nil, p.errf("bad \\x escape: need 2 hex digits")
				}
				h := string(p.s[p.pos+1 : p.pos+3])
				n, err := strconv.ParseInt(h, 16, 32)
				if err != nil {
					return nil, p.errf("bad \\x escape %q", h)
				}
				b.WriteRune(rune(n))
				p.pos += 2
			default:
				// JS behavior: drop the backslash for unknown escapes
				b.WriteRune(e)
			}
			p.pos++
			continue
		}
		b.WriteRune(c)
		p.pos++
	}
	return nil, p.errf("unterminated string")
}

func (p *valParser) number() (any, error) {
	start := p.pos
	if p.peek() == '-' {
		p.pos++
	}
	isFloat := false
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		switch {
		case c >= '0' && c <= '9':
			p.pos++
		case c == '.' || c == 'e' || c == 'E' || c == '+' || c == '-':
			isFloat = true
			p.pos++
		default:
			goto done
		}
	}
done:
	tok := string(p.s[start:p.pos])
	if isFloat {
		f, err := strconv.ParseFloat(tok, 64)
		if err != nil {
			return nil, p.errf("bad number %q", tok)
		}
		return f, nil
	}
	n, err := strconv.ParseInt(tok, 10, 64)
	if err != nil {
		return nil, p.errf("bad integer %q", tok)
	}
	return n, nil
}

// regex parses /pattern/flags. Escaped slashes (\/) contribute a literal /;
// other escape sequences (\d, \.) keep both the backslash and the following char.
func (p *valParser) regex() (any, error) {
	p.pos++ // consume /
	var pat strings.Builder
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		if c == '/' {
			break
		}
		if c == '\\' {
			p.pos++
			if p.pos >= len(p.s) {
				return nil, p.errf("unterminated regex escape")
			}
			next := p.s[p.pos]
			if next == '/' {
				// escaped slash → literal /
				pat.WriteRune('/')
			} else {
				// other escapes → keep both chars (regex metacharacters)
				pat.WriteRune('\\')
				pat.WriteRune(next)
			}
			p.pos++
			continue
		}
		pat.WriteRune(c)
		p.pos++
	}
	if p.pos >= len(p.s) {
		return nil, p.errf("unterminated regex")
	}
	p.pos++ // consume closing /
	fstart := p.pos
	for p.pos < len(p.s) && p.s[p.pos] >= 'a' && p.s[p.pos] <= 'z' {
		p.pos++
	}
	return primitive.Regex{Pattern: pat.String(), Options: string(p.s[fstart:p.pos])}, nil
}

// identOrHelper handles true/false/null, MinKey/MaxKey, `new Date(...)`, and the
// type helpers ObjectId/ISODate/Date/NumberLong/NumberInt/NumberDecimal/
// Timestamp/UUID/BinData/Infinity/NaN.
func (p *valParser) identOrHelper() (any, error) {
	// `new` keyword followed by any whitespace — skip the keyword then ws.
	if p.pos+3 <= len(p.s) && string(p.s[p.pos:p.pos+3]) == "new" &&
		p.pos+3 < len(p.s) && unicode.IsSpace(p.s[p.pos+3]) {
		p.pos += 3
		p.ws()
	}
	start := p.pos
	for p.pos < len(p.s) && isIdentRune(p.s[p.pos]) {
		p.pos++
	}
	name := string(p.s[start:p.pos])
	switch name {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null":
		return nil, nil
	case "Infinity":
		return math.Inf(1), nil
	case "NaN":
		return math.NaN(), nil
	case "MinKey":
		p.optionalEmptyCall()
		return primitive.MinKey{}, nil
	case "MaxKey":
		p.optionalEmptyCall()
		return primitive.MaxKey{}, nil
	case "":
		return nil, p.errf("unexpected character %q", string(p.peek()))
	}
	// a helper call: name(args)
	args, err := p.callArgs()
	if err != nil {
		return nil, err
	}
	return p.helper(name, args)
}

func isIdentRune(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

func (p *valParser) hasPrefix(s string) bool {
	rs := []rune(s)
	if p.pos+len(rs) > len(p.s) {
		return false
	}
	return string(p.s[p.pos:p.pos+len(rs)]) == s
}

// optionalEmptyCall consumes a trailing "()" if present (MinKey vs MinKey()).
func (p *valParser) optionalEmptyCall() {
	p.ws()
	if p.peek() == '(' {
		p.pos++
		p.ws()
		if p.peek() == ')' {
			p.pos++
		}
	}
}

// callArgs parses `( v1, v2, ... )` after a helper name.
func (p *valParser) callArgs() ([]any, error) {
	p.ws()
	if p.peek() != '(' {
		return nil, p.errf("expected '(' for helper call")
	}
	p.pos++
	var args []any
	p.ws()
	if p.peek() == ')' {
		p.pos++
		return args, nil
	}
	for {
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		args = append(args, v)
		p.ws()
		switch p.peek() {
		case ',':
			p.pos++
		case ')':
			p.pos++
			return args, nil
		default:
			return nil, p.errf("expected ',' or ')' in helper call")
		}
	}
}

func (p *valParser) helper(name string, args []any) (any, error) {
	str1 := func() (string, bool) {
		if len(args) >= 1 {
			s, ok := args[0].(string)
			return s, ok
		}
		return "", false
	}
	switch name {
	case "ObjectId", "ObjectID":
		s, ok := str1()
		if !ok {
			return nil, p.errf("ObjectId expects a hex string")
		}
		oid, err := primitive.ObjectIDFromHex(s)
		if err != nil {
			return nil, p.errf("invalid ObjectId %q", s)
		}
		return oid, nil
	case "ISODate", "Date":
		s, ok := str1()
		if !ok {
			return nil, p.errf("%s expects a date string", name)
		}
		tm, err := parseDate(s)
		if err != nil {
			return nil, p.errf("invalid date %q", s)
		}
		return primitive.NewDateTimeFromTime(tm), nil
	case "NumberLong":
		return toInt64(args, p)
	case "NumberInt":
		n, err := toInt64(args, p)
		if err != nil {
			return nil, err
		}
		iv := n.(int64)
		if iv < math.MinInt32 || iv > math.MaxInt32 {
			return nil, p.errf("NumberInt value %d overflows int32", iv)
		}
		return int32(iv), nil
	case "NumberDecimal":
		s, ok := str1()
		if !ok {
			return nil, p.errf("NumberDecimal expects a string")
		}
		d, err := primitive.ParseDecimal128(s)
		if err != nil {
			return nil, p.errf("invalid decimal %q", s)
		}
		return d, nil
	case "UUID":
		s, ok := str1()
		if !ok {
			return nil, p.errf("UUID expects a string")
		}
		// strip dashes and hex-decode → 16 bytes
		clean := strings.ReplaceAll(s, "-", "")
		if len(clean) != 32 {
			return nil, p.errf("invalid UUID %q: expected 32 hex chars after stripping dashes", s)
		}
		data, err := hex.DecodeString(clean)
		if err != nil {
			return nil, p.errf("invalid UUID %q: %v", s, err)
		}
		return primitive.Binary{Subtype: 4, Data: data}, nil
	case "BinData":
		if len(args) != 2 {
			return nil, p.errf("BinData expects (subtype, base64string)")
		}
		subtypeArg, ok := args[0].(int64)
		if !ok {
			return nil, p.errf("BinData subtype must be an integer")
		}
		b64, ok := args[1].(string)
		if !ok {
			return nil, p.errf("BinData second argument must be a base64 string")
		}
		data, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, p.errf("BinData invalid base64: %v", err)
		}
		return primitive.Binary{Subtype: byte(subtypeArg), Data: data}, nil
	case "Timestamp":
		if len(args) == 2 {
			t, _ := args[0].(int64)
			i, _ := args[1].(int64)
			return primitive.Timestamp{T: uint32(t), I: uint32(i)}, nil
		}
		return nil, p.errf("Timestamp expects (t, i)")
	default:
		return nil, p.errf("unknown helper %q", name)
	}
}

func toInt64(args []any, p *valParser) (any, error) {
	if len(args) != 1 {
		return nil, p.errf("expected one numeric argument")
	}
	switch v := args[0].(type) {
	case int64:
		return v, nil
	case float64:
		return int64(v), nil
	case string:
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, p.errf("invalid integer %q", v)
		}
		return n, nil
	}
	return nil, p.errf("expected a numeric argument")
}

func parseDate(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("bad date")
}
