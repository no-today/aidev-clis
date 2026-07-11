package mongo

// Statement is a parsed mongosh statement.
type Statement struct {
	Collection string
	Method     string
	Args       []any
	Modifiers  []Modifier
}

// Modifier is a chained call after the method (e.g. .limit(50)).
type Modifier struct {
	Name string
	Arg  any // nil if the call had no argument
}

// ParseStatement parses `db.<coll>.<method>(args)[.<mod>(arg)]*`.
func ParseStatement(input string) (*Statement, error) {
	p := &valParser{s: []rune(input)}
	p.ws()
	if !p.consumeIdent("db") {
		return nil, p.errf("statement must start with 'db'")
	}
	coll, err := p.collection()
	if err != nil {
		return nil, err
	}
	if !p.consumeDot() {
		return nil, p.errf("expected '.<method>(...)'")
	}
	method := p.ident()
	if method == "" {
		return nil, p.errf("expected a method name")
	}
	args, err := p.callArgs()
	if err != nil {
		return nil, err
	}
	st := &Statement{Collection: coll, Method: method, Args: args}
	// chained modifiers
	for {
		p.ws()
		if p.peek() != '.' {
			break
		}
		p.pos++
		name := p.ident()
		if name == "" {
			return nil, p.errf("expected a modifier name after '.'")
		}
		margs, err := p.callArgs()
		if err != nil {
			return nil, err
		}
		var arg any
		if len(margs) > 0 {
			arg = margs[0]
		}
		st.Modifiers = append(st.Modifiers, Modifier{Name: name, Arg: arg})
	}
	p.ws()
	if p.pos != len(p.s) {
		return nil, p.errf("unexpected trailing input")
	}
	return st, nil
}

// collection parses `.name`, `.getCollection("name")`, or `["name"]`.
func (p *valParser) collection() (string, error) {
	p.ws()
	if p.peek() == '[' { // db["name"]
		p.pos++
		s, err := p.value()
		if err != nil {
			return "", err
		}
		name, ok := s.(string)
		if !ok {
			return "", p.errf("db[...] expects a string")
		}
		p.ws()
		if p.peek() != ']' {
			return "", p.errf("expected ']'")
		}
		p.pos++
		return name, nil
	}
	if !p.consumeDot() {
		return "", p.errf("expected '.<collection>'")
	}
	if p.hasPrefix("getCollection") {
		p.pos += len("getCollection")
		args, err := p.callArgs()
		if err != nil {
			return "", err
		}
		if len(args) != 1 {
			return "", p.errf("getCollection expects one string")
		}
		name, ok := args[0].(string)
		if !ok {
			return "", p.errf("getCollection expects a string")
		}
		return name, nil
	}
	name := p.ident()
	if name == "" {
		return "", p.errf("expected a collection name")
	}
	return name, nil
}

func (p *valParser) consumeIdent(want string) bool {
	p.ws()
	if p.hasPrefix(want) {
		end := p.pos + len([]rune(want))
		if end == len(p.s) || !isIdentRune(p.s[end]) {
			p.pos = end
			return true
		}
	}
	return false
}

func (p *valParser) consumeDot() bool {
	p.ws()
	if p.peek() == '.' {
		p.pos++
		return true
	}
	return false
}

func (p *valParser) ident() string {
	p.ws()
	start := p.pos
	for p.pos < len(p.s) && isIdentRune(p.s[p.pos]) {
		p.pos++
	}
	return string(p.s[start:p.pos])
}
