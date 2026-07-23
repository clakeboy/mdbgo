package mdbgo

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type sqlTokenKind uint8

const (
	sqlTokenEOF sqlTokenKind = iota
	sqlTokenIdent
	sqlTokenNumber
	sqlTokenString
	sqlTokenDate
	sqlTokenComma
	sqlTokenDot
	sqlTokenLParen
	sqlTokenRParen
	sqlTokenStar
	sqlTokenSemicolon
	sqlTokenOperator
)

type sqlToken struct {
	kind sqlTokenKind
	text string
	pos  int
}

type sqlLexer struct {
	text string
	pos  int
}

func lexSQL(text string) ([]sqlToken, error) {
	l := &sqlLexer{text: text}
	var tokens []sqlToken
	for {
		tok, err := l.next()
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, tok)
		if tok.kind == sqlTokenEOF {
			return tokens, nil
		}
	}
}

func (l *sqlLexer) next() (sqlToken, error) {
	for l.pos < len(l.text) {
		switch l.text[l.pos] {
		case ' ', '\t', '\r', '\n':
			l.pos++
			continue
		case '-':
			if l.pos+1 < len(l.text) && l.text[l.pos+1] == '-' {
				l.pos += 2
				for l.pos < len(l.text) && l.text[l.pos] != '\n' {
					l.pos++
				}
				continue
			}
		}
		break
	}
	if l.pos >= len(l.text) {
		return sqlToken{kind: sqlTokenEOF, pos: l.pos}, nil
	}
	start := l.pos
	ch := l.text[l.pos]
	l.pos++
	switch ch {
	case ',':
		return sqlToken{kind: sqlTokenComma, text: ",", pos: start}, nil
	case '.':
		return sqlToken{kind: sqlTokenDot, text: ".", pos: start}, nil
	case '(':
		return sqlToken{kind: sqlTokenLParen, text: "(", pos: start}, nil
	case ')':
		return sqlToken{kind: sqlTokenRParen, text: ")", pos: start}, nil
	case '*':
		return sqlToken{kind: sqlTokenStar, text: "*", pos: start}, nil
	case ';':
		return sqlToken{kind: sqlTokenSemicolon, text: ";", pos: start}, nil
	case '[':
		end := byte(']')
		var b strings.Builder
		for l.pos < len(l.text) {
			c := l.text[l.pos]
			l.pos++
			if c == end {
				if l.pos < len(l.text) && l.text[l.pos] == end {
					b.WriteByte(end)
					l.pos++
					continue
				}
				return sqlToken{kind: sqlTokenIdent, text: b.String(), pos: start}, nil
			}
			b.WriteByte(c)
		}
		return sqlToken{}, fmt.Errorf("SQL position %d: unterminated identifier", start)
	case '\'', '"':
		quote := ch
		var b strings.Builder
		for l.pos < len(l.text) {
			c := l.text[l.pos]
			l.pos++
			if c == quote {
				if l.pos < len(l.text) && l.text[l.pos] == quote {
					b.WriteByte(quote)
					l.pos++
					continue
				}
				return sqlToken{kind: sqlTokenString, text: b.String(), pos: start}, nil
			}
			b.WriteByte(c)
		}
		return sqlToken{}, fmt.Errorf("SQL position %d: unterminated string", start)
	case '#':
		end := strings.IndexByte(l.text[l.pos:], '#')
		if end < 0 {
			return sqlToken{}, fmt.Errorf("SQL position %d: unterminated date literal", start)
		}
		value := l.text[l.pos : l.pos+end]
		l.pos += end + 1
		return sqlToken{kind: sqlTokenDate, text: value, pos: start}, nil
	case '=', '<', '>', '+', '/', '&', '^':
		if l.pos < len(l.text) {
			pair := l.text[start : l.pos+1]
			if pair == "<=" || pair == ">=" || pair == "<>" {
				l.pos++
				return sqlToken{kind: sqlTokenOperator, text: pair, pos: start}, nil
			}
		}
		return sqlToken{kind: sqlTokenOperator, text: string(ch), pos: start}, nil
	}
	if ch == '-' || (ch >= '0' && ch <= '9') {
		if ch == '-' && (l.pos >= len(l.text) || l.text[l.pos] < '0' || l.text[l.pos] > '9') {
			return sqlToken{kind: sqlTokenOperator, text: "-", pos: start}, nil
		}
		for l.pos < len(l.text) && l.text[l.pos] >= '0' && l.text[l.pos] <= '9' {
			l.pos++
		}
		if l.pos < len(l.text) && l.text[l.pos] == '.' {
			l.pos++
			for l.pos < len(l.text) && l.text[l.pos] >= '0' && l.text[l.pos] <= '9' {
				l.pos++
			}
		}
		if l.pos < len(l.text) && (l.text[l.pos] == 'e' || l.text[l.pos] == 'E') {
			l.pos++
			if l.pos < len(l.text) && (l.text[l.pos] == '+' || l.text[l.pos] == '-') {
				l.pos++
			}
			for l.pos < len(l.text) && l.text[l.pos] >= '0' && l.text[l.pos] <= '9' {
				l.pos++
			}
		}
		return sqlToken{kind: sqlTokenNumber, text: l.text[start:l.pos], pos: start}, nil
	}
	if unicode.IsLetter(rune(ch)) || ch == '_' || ch >= 0x80 {
		for l.pos < len(l.text) {
			r := rune(l.text[l.pos])
			if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '$' || r == '@' || r >= 0x80 {
				l.pos++
				continue
			}
			break
		}
		return sqlToken{kind: sqlTokenIdent, text: l.text[start:l.pos], pos: start}, nil
	}
	return sqlToken{}, fmt.Errorf("SQL position %d: unexpected character %q", start, ch)
}

type sqlExpr interface {
	sqlExpr()
}

type sqlLiteral struct{ value Value }
type sqlIdent struct{ parts []string }
type sqlUnary struct {
	op   string
	expr sqlExpr
}
type sqlBinary struct {
	op          string
	left, right sqlExpr
}
type sqlCall struct {
	name string
	args []sqlExpr
}
type sqlStar struct{ qualifier string }
type sqlIn struct {
	expr  sqlExpr
	list  []sqlExpr
	query *sqlSelect
	not   bool
}
type sqlBetween struct {
	expr, low, high sqlExpr
	not             bool
}
type sqlSubquery struct {
	stmt   *sqlSelect
	exists bool
}

func (sqlLiteral) sqlExpr()  {}
func (sqlIdent) sqlExpr()    {}
func (sqlUnary) sqlExpr()    {}
func (sqlBinary) sqlExpr()   {}
func (sqlCall) sqlExpr()     {}
func (sqlStar) sqlExpr()     {}
func (sqlIn) sqlExpr()       {}
func (sqlBetween) sqlExpr()  {}
func (sqlSubquery) sqlExpr() {}

type sqlSelectItem struct {
	expr  sqlExpr
	alias string
}

type sqlOrder struct {
	expr sqlExpr
	desc bool
}

type sqlTableRef struct {
	name  string
	alias string
}

type sqlSource interface {
	sqlSource()
}

type sqlTableSource struct {
	ref sqlTableRef
}

type sqlJoinSource struct {
	kind        string
	left, right sqlSource
	on          sqlExpr
}

func (sqlTableSource) sqlSource() {}
func (sqlJoinSource) sqlSource()  {}

type sqlJoin struct {
	kind  string
	right sqlTableRef
	on    sqlExpr
}

type sqlSelect struct {
	distinct    bool
	distinctRow bool
	top         int
	topPercent  bool
	items       []sqlSelectItem
	source      sqlSource
	from        sqlTableRef
	joins       []sqlJoin
	where       sqlExpr
	group       []sqlExpr
	having      sqlExpr
	order       []sqlOrder
	limit       int
	offset      int
	union       *sqlSelect
	unionAll    bool
	params      map[string]string
}

type sqlParser struct {
	tokens []sqlToken
	pos    int
}

func parseAccessSQL(text string) (*sqlSelect, error) {
	tokens, err := lexSQL(text)
	if err != nil {
		return nil, err
	}
	p := &sqlParser{tokens: tokens}
	params := make(map[string]string)
	if p.keyword("PARAMETERS") {
		for {
			name, err := p.identifier()
			if err != nil {
				return nil, err
			}
			typ := ""
			if p.peek().kind == sqlTokenIdent {
				typ = p.take().text
			}
			params[strings.ToLower(name)] = typ
			if p.accept(sqlTokenSemicolon, "") {
				break
			}
			if !p.accept(sqlTokenComma, "") {
				return nil, p.errorf("expected ',' or ';' after parameter")
			}
		}
	}
	stmt, err := p.parseSelect()
	if err != nil {
		return nil, err
	}
	stmt.params = params
	p.accept(sqlTokenSemicolon, "")
	if p.peek().kind != sqlTokenEOF {
		return nil, p.errorf("unexpected token %q", p.peek().text)
	}
	return stmt, nil
}

func (p *sqlParser) parseSelect() (*sqlSelect, error) {
	if !p.keyword("SELECT") {
		return nil, p.errorf("expected SELECT")
	}
	s := &sqlSelect{top: -1, limit: -1}
	if p.keyword("DISTINCTROW") {
		s.distinctRow = true
	} else if p.keyword("DISTINCT") {
		s.distinct = true
	} else {
		p.keyword("ALL")
	}
	if p.keyword("TOP") {
		n, err := p.integer()
		if err != nil {
			return nil, err
		}
		s.top = n
		s.topPercent = p.keyword("PERCENT")
	}
	for {
		expr, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		item := sqlSelectItem{expr: expr}
		if p.keyword("AS") {
			item.alias, err = p.identifier()
			if err != nil {
				return nil, err
			}
		} else if p.peek().kind == sqlTokenIdent && !isClauseKeyword(p.peek().text) {
			item.alias = p.take().text
		}
		s.items = append(s.items, item)
		if !p.accept(sqlTokenComma, "") {
			break
		}
	}
	if !p.keyword("FROM") {
		return nil, p.errorf("expected FROM")
	}
	var err error
	s.source, err = p.parseSource()
	if err != nil {
		return nil, err
	}
	s.from, s.joins = flattenSQLSource(s.source)

	if p.keyword("WHERE") {
		s.where, err = p.parseExpr(0)
		if err != nil {
			return nil, err
		}
	}
	if p.keyword("GROUP") {
		if !p.keyword("BY") {
			return nil, p.errorf("expected BY")
		}
		s.group, err = p.parseExprList()
		if err != nil {
			return nil, err
		}
	}
	if p.keyword("HAVING") {
		s.having, err = p.parseExpr(0)
		if err != nil {
			return nil, err
		}
	}
	if p.keyword("ORDER") {
		if !p.keyword("BY") {
			return nil, p.errorf("expected BY")
		}
		for {
			e, eerr := p.parseExpr(0)
			if eerr != nil {
				return nil, eerr
			}
			o := sqlOrder{expr: e}
			if p.keyword("DESC") {
				o.desc = true
			} else {
				p.keyword("ASC")
			}
			s.order = append(s.order, o)
			if !p.accept(sqlTokenComma, "") {
				break
			}
		}
	}
	if p.keyword("LIMIT") {
		s.limit, err = p.integer()
		if err != nil {
			return nil, err
		}
		if p.keyword("OFFSET") {
			s.offset, err = p.integer()
			if err != nil {
				return nil, err
			}
		}
	} else if p.keyword("OFFSET") {
		s.offset, err = p.integer()
		if err != nil {
			return nil, err
		}
	}
	if p.keyword("WITH") {
		if !p.keyword("OWNERACCESS") || !p.keyword("OPTION") {
			return nil, p.errorf("expected OWNERACCESS OPTION")
		}
	}
	if p.keyword("UNION") {
		s.unionAll = p.keyword("ALL")
		s.union, err = p.parseSelect()
		if err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (p *sqlParser) parseSource() (sqlSource, error) {
	left, err := p.parseSourcePrimary()
	if err != nil {
		return nil, err
	}
	for {
		if p.accept(sqlTokenComma, "") {
			right, err := p.parseSourcePrimary()
			if err != nil {
				return nil, err
			}
			left = sqlJoinSource{kind: "CROSS", left: left, right: right}
			continue
		}
		kind := ""
		switch {
		case p.keyword("INNER"):
			kind = "INNER"
		case p.keyword("LEFT"):
			kind = "LEFT"
			p.keyword("OUTER")
		case p.keyword("RIGHT"):
			kind = "RIGHT"
			p.keyword("OUTER")
		case p.keyword("CROSS"):
			kind = "CROSS"
		case p.isKeyword("JOIN"):
			kind = "INNER"
		default:
			return left, nil
		}
		if !p.keyword("JOIN") {
			return nil, p.errorf("expected JOIN")
		}
		right, err := p.parseSourcePrimary()
		if err != nil {
			return nil, err
		}
		var on sqlExpr
		if kind != "CROSS" {
			if !p.keyword("ON") {
				return nil, p.errorf("expected ON")
			}
			on, err = p.parseExpr(0)
			if err != nil {
				return nil, err
			}
		}
		left = sqlJoinSource{kind: kind, left: left, right: right, on: on}
	}
}

func (p *sqlParser) parseSourcePrimary() (sqlSource, error) {
	if p.accept(sqlTokenLParen, "") {
		source, err := p.parseSource()
		if err != nil {
			return nil, err
		}
		if !p.accept(sqlTokenRParen, "") {
			return nil, p.errorf("expected ')' after joined table expression")
		}
		return source, nil
	}
	ref, err := p.parseTableRef()
	if err != nil {
		return nil, err
	}
	return sqlTableSource{ref: ref}, nil
}

func flattenSQLSource(source sqlSource) (sqlTableRef, []sqlJoin) {
	var first sqlTableRef
	var joins []sqlJoin
	var walk func(sqlSource)
	walk = func(current sqlSource) {
		switch x := current.(type) {
		case sqlTableSource:
			if first.name == "" {
				first = x.ref
			}
		case sqlJoinSource:
			walk(x.left)
			var right sqlTableRef
			if table, ok := x.right.(sqlTableSource); ok {
				right = table.ref
			} else {
				before := len(joins)
				walk(x.right)
				if before < len(joins) {
					right = joins[before].right
				}
			}
			joins = append(joins, sqlJoin{kind: x.kind, right: right, on: x.on})
		}
	}
	walk(source)
	return first, joins
}

func (p *sqlParser) parseTableRef() (sqlTableRef, error) {
	name, err := p.identifier()
	if err != nil {
		return sqlTableRef{}, err
	}
	ref := sqlTableRef{name: name}
	if p.keyword("AS") {
		ref.alias, err = p.identifier()
	} else if p.peek().kind == sqlTokenIdent && !isClauseKeyword(p.peek().text) {
		ref.alias = p.take().text
	}
	return ref, err
}

func (p *sqlParser) parseExprList() ([]sqlExpr, error) {
	var result []sqlExpr
	for {
		e, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		result = append(result, e)
		if !p.accept(sqlTokenComma, "") {
			return result, nil
		}
	}
}

var sqlPrecedence = map[string]int{
	"OR": 1, "AND": 2,
	"=": 3, "<>": 3, "<": 3, ">": 3, "<=": 3, ">=": 3, "LIKE": 3, "IS": 3, "IN": 3, "BETWEEN": 3,
	"&": 4, "+": 5, "-": 5, "*": 6, "/": 6,
}

func (p *sqlParser) parseExpr(minPrec int) (sqlExpr, error) {
	var left sqlExpr
	tok := p.take()
	switch tok.kind {
	case sqlTokenNumber:
		if strings.ContainsAny(tok.text, ".eE") {
			n, err := strconv.ParseFloat(tok.text, 64)
			if err != nil {
				return nil, p.errorAt(tok, "invalid number")
			}
			left = sqlLiteral{value: FloatValue(n)}
		} else {
			n, err := strconv.ParseInt(tok.text, 10, 64)
			if err != nil {
				return nil, p.errorAt(tok, "invalid integer")
			}
			left = sqlLiteral{value: IntValue(n)}
		}
	case sqlTokenString:
		left = sqlLiteral{value: StringValue(tok.text)}
	case sqlTokenDate:
		v, err := parseAccessDate(tok.text)
		if err != nil {
			return nil, p.errorAt(tok, err.Error())
		}
		left = sqlLiteral{value: TimeValue(v)}
	case sqlTokenStar:
		left = sqlStar{}
	case sqlTokenLParen:
		var err error
		if p.isKeyword("SELECT") {
			var stmt *sqlSelect
			stmt, err = p.parseSelect()
			left = sqlSubquery{stmt: stmt}
		} else {
			left, err = p.parseExpr(0)
		}
		if err != nil {
			return nil, err
		}
		if !p.accept(sqlTokenRParen, "") {
			return nil, p.errorf("expected ')'")
		}
	case sqlTokenOperator:
		if tok.text != "+" && tok.text != "-" {
			return nil, p.errorAt(tok, "unexpected operator")
		}
		e, err := p.parseExpr(7)
		if err != nil {
			return nil, err
		}
		left = sqlUnary{op: tok.text, expr: e}
	case sqlTokenIdent:
		upper := strings.ToUpper(tok.text)
		if upper == "NOT" {
			e, err := p.parseExpr(7)
			if err != nil {
				return nil, err
			}
			left = sqlUnary{op: "NOT", expr: e}
			break
		}
		if upper == "EXISTS" {
			if !p.accept(sqlTokenLParen, "") || !p.isKeyword("SELECT") {
				return nil, p.errorAt(tok, "EXISTS requires a SELECT subquery")
			}
			stmt, err := p.parseSelect()
			if err != nil {
				return nil, err
			}
			if !p.accept(sqlTokenRParen, "") {
				return nil, p.errorf("expected ')' after EXISTS subquery")
			}
			left = sqlSubquery{stmt: stmt, exists: true}
			break
		}
		switch upper {
		case "NULL":
			left = NullValueExpr()
			goto infix
		case "TRUE", "YES":
			left = sqlLiteral{value: BoolValue(true)}
			goto infix
		case "FALSE", "NO":
			left = sqlLiteral{value: BoolValue(false)}
			goto infix
		}
		if p.accept(sqlTokenLParen, "") {
			call := sqlCall{name: tok.text}
			if !p.accept(sqlTokenRParen, "") {
				for {
					e, err := p.parseExpr(0)
					if err != nil {
						return nil, err
					}
					call.args = append(call.args, e)
					if p.accept(sqlTokenRParen, "") {
						break
					}
					if !p.accept(sqlTokenComma, "") {
						return nil, p.errorf("expected ',' or ')'")
					}
				}
			}
			left = call
			break
		}
		parts := []string{tok.text}
		for p.accept(sqlTokenDot, "") {
			if p.accept(sqlTokenStar, "") {
				left = sqlStar{qualifier: strings.Join(parts, ".")}
				goto infix
			}
			part, err := p.identifier()
			if err != nil {
				return nil, err
			}
			parts = append(parts, part)
		}
		left = sqlIdent{parts: parts}
	default:
		return nil, p.errorAt(tok, "expected expression")
	}
infix:
	for {
		op := ""
		notModifier := false
		next := p.peek()
		if next.kind == sqlTokenOperator || next.kind == sqlTokenStar {
			op = next.text
			if next.kind == sqlTokenStar {
				op = "*"
			}
		} else if next.kind == sqlTokenIdent {
			up := strings.ToUpper(next.text)
			if up == "NOT" && p.pos+1 < len(p.tokens) {
				after := strings.ToUpper(p.tokens[p.pos+1].text)
				if after == "IN" || after == "BETWEEN" || after == "LIKE" {
					notModifier = true
					op = after
				}
			} else {
				op = up
			}
		}
		prec, ok := sqlPrecedence[op]
		if !ok || prec < minPrec {
			break
		}
		p.take()
		if notModifier {
			p.take()
		}
		if op == "IS" {
			not := p.keyword("NOT")
			if !p.keyword("NULL") {
				return nil, p.errorf("expected NULL after IS")
			}
			right := sqlLiteral{value: NullValue()}
			if not {
				left = sqlUnary{op: "NOT", expr: sqlBinary{op: "IS", left: left, right: right}}
			} else {
				left = sqlBinary{op: "IS", left: left, right: right}
			}
			continue
		}
		if op == "IN" {
			if !p.accept(sqlTokenLParen, "") {
				return nil, p.errorf("expected '(' after IN")
			}
			if p.isKeyword("SELECT") {
				query, err := p.parseSelect()
				if err != nil {
					return nil, err
				}
				if !p.accept(sqlTokenRParen, "") {
					return nil, p.errorf("expected ')' after IN subquery")
				}
				left = sqlIn{expr: left, query: query, not: notModifier}
				continue
			}
			list, err := p.parseExprList()
			if err != nil {
				return nil, err
			}
			if !p.accept(sqlTokenRParen, "") {
				return nil, p.errorf("expected ')' after IN list")
			}
			left = sqlIn{expr: left, list: list, not: notModifier}
			continue
		}
		if op == "BETWEEN" {
			low, err := p.parseExpr(prec + 1)
			if err != nil {
				return nil, err
			}
			if !p.keyword("AND") {
				return nil, p.errorf("expected AND in BETWEEN")
			}
			high, err := p.parseExpr(prec + 1)
			if err != nil {
				return nil, err
			}
			left = sqlBetween{expr: left, low: low, high: high, not: notModifier}
			continue
		}
		right, err := p.parseExpr(prec + 1)
		if err != nil {
			return nil, err
		}
		if notModifier && op == "LIKE" {
			left = sqlUnary{op: "NOT", expr: sqlBinary{op: op, left: left, right: right}}
		} else {
			left = sqlBinary{op: op, left: left, right: right}
		}
	}
	return left, nil
}

func (p *sqlParser) identifier() (string, error) {
	tok := p.take()
	if tok.kind != sqlTokenIdent {
		return "", p.errorAt(tok, "expected identifier")
	}
	return tok.text, nil
}

func (p *sqlParser) integer() (int, error) {
	tok := p.take()
	if tok.kind != sqlTokenNumber {
		return 0, p.errorAt(tok, "expected integer")
	}
	n, err := strconv.Atoi(tok.text)
	if err != nil || n < 0 {
		return 0, p.errorAt(tok, "invalid non-negative integer")
	}
	return n, nil
}

func (p *sqlParser) keyword(word string) bool {
	if p.isKeyword(word) {
		p.pos++
		return true
	}
	return false
}

func (p *sqlParser) isKeyword(word string) bool {
	t := p.peek()
	return t.kind == sqlTokenIdent && strings.EqualFold(t.text, word)
}

func (p *sqlParser) accept(kind sqlTokenKind, text string) bool {
	t := p.peek()
	if t.kind != kind || (text != "" && !strings.EqualFold(t.text, text)) {
		return false
	}
	p.pos++
	return true
}

func (p *sqlParser) take() sqlToken {
	t := p.peek()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return t
}

func (p *sqlParser) peek() sqlToken {
	if p.pos >= len(p.tokens) {
		return sqlToken{kind: sqlTokenEOF}
	}
	return p.tokens[p.pos]
}

func (p *sqlParser) errorf(format string, args ...any) error {
	return p.errorAt(p.peek(), fmt.Sprintf(format, args...))
}

func (p *sqlParser) errorAt(tok sqlToken, message string) error {
	return fmt.Errorf("SQL position %d: %s", tok.pos, message)
}

func isClauseKeyword(text string) bool {
	switch strings.ToUpper(text) {
	case "FROM", "WHERE", "GROUP", "HAVING", "ORDER", "LIMIT", "OFFSET", "UNION",
		"INNER", "LEFT", "RIGHT", "CROSS", "JOIN", "ON", "WITH", "ASC", "DESC":
		return true
	default:
		return false
	}
}

func parseAccessDate(text string) (time.Time, error) {
	layouts := []string{
		"1/2/2006 3:04:05 PM", "1/2/2006 3:04 PM", "1/2/2006",
		"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, strings.TrimSpace(text), time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid Access date literal #%s#", text)
}

func NullValueExpr() sqlExpr {
	return sqlLiteral{value: NullValue()}
}
