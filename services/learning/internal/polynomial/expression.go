package polynomial

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
	"unicode"
)

// polynomials are univariate with exact rational coefficients.
// The parser accepts only arithmetic; it never executes learner code.
type polynomial []*big.Rat

func constant(r *big.Rat) polynomial { return polynomial{new(big.Rat).Set(r)} }
func variable() polynomial           { return polynomial{new(big.Rat), big.NewRat(1, 1)} }
func trim(a polynomial) polynomial {
	for len(a) > 1 && a[len(a)-1].Sign() == 0 {
		a = a[:len(a)-1]
	}
	return a
}
func checkBounds(a polynomial) error {
	if len(a) > 65 {
		return errors.New("polynomial degree exceeds 64")
	}
	for _, r := range a {
		if r.Num().BitLen() > 4096 || r.Denom().BitLen() > 4096 {
			return errors.New("polynomial coefficient exceeds size limit")
		}
	}
	return nil
}
func add(a, b polynomial, negative bool) (polynomial, error) {
	n := max(len(a), len(b))
	out := make(polynomial, n)
	for i := 0; i < n; i++ {
		out[i] = new(big.Rat)
		if i < len(a) {
			out[i].Set(a[i])
		}
		if i < len(b) {
			if negative {
				out[i].Sub(out[i], b[i])
			} else {
				out[i].Add(out[i], b[i])
			}
		}
	}
	out = trim(out)
	return out, checkBounds(out)
}
func mul(a, b polynomial) (polynomial, error) {
	if len(a)+len(b)-2 > 64 {
		return nil, errors.New("polynomial degree exceeds 64")
	}
	out := make(polynomial, len(a)+len(b)-1)
	for i := range out {
		out[i] = new(big.Rat)
	}
	for i, ai := range a {
		for j, bj := range b {
			out[i+j].Add(out[i+j], new(big.Rat).Mul(ai, bj))
		}
	}
	out = trim(out)
	return out, checkBounds(out)
}
func div(a, b polynomial) (polynomial, error) {
	if len(b) != 1 || b[0].Sign() == 0 {
		return nil, errors.New("only division by a nonzero scalar is supported")
	}
	inverse := new(big.Rat).Inv(b[0])
	out := make(polynomial, len(a))
	for i, x := range a {
		out[i] = new(big.Rat).Mul(x, inverse)
	}
	return trim(out), checkBounds(out)
}
func derivative(a polynomial) polynomial {
	if len(a) == 1 {
		return constant(big.NewRat(0, 1))
	}
	out := make(polynomial, len(a)-1)
	for i := 1; i < len(a); i++ {
		out[i-1] = new(big.Rat).Mul(a[i], big.NewRat(int64(i), 1))
	}
	return trim(out)
}
func equal(a, b polynomial) bool {
	a, b = trim(a), trim(b)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Cmp(b[i]) != 0 {
			return false
		}
	}
	return true
}
func canonical(a polynomial) string {
	parts := make([]string, 0, len(a))
	for i := len(a) - 1; i >= 0; i-- {
		if a[i].Sign() == 0 {
			continue
		}
		term := a[i].RatString()
		if i > 0 {
			term += "*x"
			if i > 1 {
				term += fmt.Sprintf("^%d", i)
			}
		}
		parts = append(parts, term)
	}
	if len(parts) == 0 {
		return "0"
	}
	return strings.Join(parts, " + ")
}

type parser struct {
	text   string
	offset int
	symbol string
	visits int
}

func (p *parser) space() {
	for p.offset < len(p.text) && unicode.IsSpace(rune(p.text[p.offset])) {
		p.offset++
	}
}
func (p *parser) consume(c byte) bool {
	p.space()
	if p.offset < len(p.text) && p.text[p.offset] == c {
		p.offset++
		return true
	}
	return false
}
func (p *parser) next() byte {
	p.space()
	if p.offset >= len(p.text) {
		return 0
	}
	return p.text[p.offset]
}
func (p *parser) expr() (polynomial, error) {
	left, err := p.term()
	if err != nil {
		return nil, err
	}
	for {
		switch p.next() {
		case '+', '-':
			sign := p.text[p.offset]
			p.offset++
			right, e := p.term()
			if e != nil {
				return nil, e
			}
			left, e = add(left, right, sign == '-')
			if e != nil {
				return nil, e
			}
		default:
			return left, nil
		}
	}
}
func (p *parser) term() (polynomial, error) {
	left, err := p.unary()
	if err != nil {
		return nil, err
	}
	for {
		switch p.next() {
		case '*', '/':
			op := p.text[p.offset]
			p.offset++
			right, e := p.unary()
			if e != nil {
				return nil, e
			}
			if op == '*' {
				left, e = mul(left, right)
			} else {
				left, e = div(left, right)
			}
			if e != nil {
				return nil, e
			}
		default:
			return left, nil
		}
	}
}
func (p *parser) unary() (polynomial, error) {
	if p.consume('+') {
		return p.unary()
	}
	if p.consume('-') {
		result, err := p.unary()
		if err != nil {
			return nil, err
		}
		for _, v := range result {
			v.Neg(v)
		}
		return result, nil
	}
	return p.power()
}
func (p *parser) power() (polynomial, error) {
	base, err := p.atom()
	if err != nil {
		return nil, err
	}
	if !p.consume('^') {
		return base, nil
	}
	p.space()
	start := p.offset
	for p.offset < len(p.text) && p.text[p.offset] >= '0' && p.text[p.offset] <= '9' {
		p.offset++
	}
	if start == p.offset {
		return nil, errors.New("exponent must be an integer between 0 and 32")
	}
	digits := p.text[start:p.offset]
	if len(digits) > 2 {
		return nil, errors.New("exponent exceeds 32")
	}
	var exponent int
	_, err = fmt.Sscanf(digits, "%d", &exponent)
	if err != nil || exponent > 32 {
		return nil, errors.New("exponent exceeds 32")
	}
	out := constant(big.NewRat(1, 1))
	for i := 0; i < exponent; i++ {
		out, err = mul(out, base)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (p *parser) atom() (polynomial, error) {
	p.visits++
	if p.visits > 8192 {
		return nil, errors.New("expression exceeds complexity limit")
	}
	p.space()
	if p.consume('(') {
		v, err := p.expr()
		if err != nil {
			return nil, err
		}
		if !p.consume(')') {
			return nil, errors.New("missing closing parenthesis")
		}
		return v, nil
	}
	start := p.offset
	for p.offset < len(p.text) && ((p.text[p.offset] >= '0' && p.text[p.offset] <= '9') || p.text[p.offset] == '.') {
		p.offset++
	}
	if p.offset > start {
		n := new(big.Rat)
		if _, ok := n.SetString(p.text[start:p.offset]); !ok {
			return nil, errors.New("invalid number")
		}
		v := constant(n)
		return v, checkBounds(v)
	}
	for p.offset < len(p.text) && ((p.text[p.offset] >= 'a' && p.text[p.offset] <= 'z') || (p.text[p.offset] >= 'A' && p.text[p.offset] <= 'Z') || (p.text[p.offset] >= '0' && p.text[p.offset] <= '9') || p.text[p.offset] == '_') {
		p.offset++
	}
	if p.offset > start {
		name := p.text[start:p.offset]
		if name != p.symbol {
			return nil, fmt.Errorf("unsupported variable %q", name)
		}
		return variable(), nil
	}
	return nil, fmt.Errorf("unexpected expression token at byte %d", p.offset)
}

func Parse(expression, symbol string) (polynomial, error) {
	if len(expression) == 0 || len(expression) > 4096 {
		return nil, errors.New("expression must contain 1..4096 bytes")
	}
	if len(symbol) != 1 || symbol[0] < 'a' || symbol[0] > 'z' {
		return nil, errors.New("variable must be one lowercase ASCII letter")
	}
	p := &parser{text: expression, symbol: symbol}
	result, err := p.expr()
	if err != nil {
		return nil, err
	}
	if p.next() != 0 {
		return nil, fmt.Errorf("unexpected trailing token at byte %d", p.offset)
	}
	return trim(result), nil
}
