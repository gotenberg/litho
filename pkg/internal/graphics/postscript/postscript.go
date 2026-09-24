package postscript

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// maxStackDepth is the operand stack bound. ISO 32000-1, 7.10.5.1: an
// implementation "shall provide a stack with room for at least 100 entries.
// No implementation shall be required to provide a larger stack, and it shall
// be an error to overflow the stack." Without it, a few hundred bytes of
// `n copy` steps double the stack each time and exhaust memory.
const maxStackDepth = 100

// maxBlockDepth caps how deeply { } blocks may nest. It is an anti-DoS guard,
// not spec-mandated: 7.10.5 sets no nesting limit, but the parser and the
// if/ifelse executor recurse once per level, and a Go stack overflow is a
// fatal error no recover can catch. Real programs nest a few if/ifelse levels,
// so 100 leaves ample headroom while bounding the recursion.
const maxBlockDepth = 100

// token represents a parsed element in a Type 4 PostScript program.
type token struct {
	kind  tokenKind
	value string  // operator name when kind == tokenOperator
	num   float64 // parsed value when kind == tokenNumber
	block []token // populated when kind == tokenBlock
}

type tokenKind int

const (
	tokenNumber tokenKind = iota
	tokenOperator
	tokenBlock // { ... }
)

// Program is a compiled Type 4 PostScript calculator program.
//
// Concurrency: a Program is immutable once [Compile] returns, so concurrent
// [Program.Execute] calls are safe.
type Program struct {
	tokens []token
}

// Compile parses a Type 4 program once, so a caller evaluating it many times
// (a tint transform builds a lookup table from hundreds of samples) pays the
// parse once.
func Compile(program []byte) (*Program, error) {
	tokens, err := tokenize(string(program))
	if err != nil {
		return nil, fmt.Errorf("tokenizing postscript program: %w", err)
	}
	return &Program{tokens: tokens}, nil
}

// Execute evaluates the program. The inputs are pushed onto the stack, the
// program is executed, and the remaining stack values are returned as
// outputs.
func (p *Program) Execute(inputs []float64) ([]float64, error) {
	if len(inputs) > maxStackDepth {
		return nil, fmt.Errorf("%w: %d inputs exceed %d entries", ErrStackOverflow, len(inputs), maxStackDepth)
	}

	stack := make([]float64, 0, len(inputs)+8)
	stack = append(stack, inputs...)

	if err := execTokens(p.tokens, &stack); err != nil {
		return nil, fmt.Errorf("executing postscript program: %w", err)
	}

	return stack, nil
}

// Execute compiles and evaluates a Type 4 PostScript calculator function in
// one call. A caller evaluating the same program repeatedly should [Compile]
// it once instead.
func Execute(inputs []float64, program []byte) ([]float64, error) {
	p, err := Compile(program)
	if err != nil {
		return nil, err
	}
	return p.Execute(inputs)
}

// tokenize parses a PostScript Type 4 program into a flat token list.
// The outermost { } braces (required by the spec) are stripped.
func tokenize(src string) ([]token, error) {
	src = strings.TrimSpace(src)
	// Strip the outermost { } if present.
	if len(src) >= 2 && src[0] == '{' && src[len(src)-1] == '}' {
		src = src[1 : len(src)-1]
	}
	tokens, _, err := parseTokens(src, 0)
	return tokens, err
}

// parseTokens reads tokens from s. At depth 0 it reads the whole program;
// inside a block it stops at the matching '}'. It returns the tokens and the
// remaining unconsumed string.
func parseTokens(s string, depth int) ([]token, string, error) {
	var tokens []token
	for {
		s = strings.TrimLeft(s, " \t\n\r")
		if len(s) == 0 {
			if depth > 0 {
				return nil, "", fmt.Errorf("%w: unterminated block", ErrSyntax)
			}
			return tokens, "", nil
		}

		ch := s[0]

		if ch == '}' {
			if depth == 0 {
				return nil, "", fmt.Errorf("%w: unexpected '}'", ErrSyntax)
			}
			return tokens, s[1:], nil
		}

		if ch == '{' {
			if depth >= maxBlockDepth {
				return nil, "", fmt.Errorf("%w: more than %d levels", ErrBlockTooDeep, maxBlockDepth)
			}
			inner, rest, err := parseTokens(s[1:], depth+1)
			if err != nil {
				return nil, "", fmt.Errorf("parsing nested block: %w", err)
			}
			tokens = append(tokens, token{kind: tokenBlock, block: inner})
			s = rest
			continue
		}

		// Read a word (number or operator name).
		end := 0
		for end < len(s) && s[end] != ' ' && s[end] != '\t' && s[end] != '\n' && s[end] != '\r' && s[end] != '{' && s[end] != '}' {
			end++
		}
		word := s[:end]
		s = s[end:]

		if v, err := strconv.ParseFloat(word, 64); err == nil {
			tokens = append(tokens, token{kind: tokenNumber, num: v})
		} else {
			tokens = append(tokens, token{kind: tokenOperator, value: word})
		}
	}
}

// execOp dispatches a single operator.
//
//nolint:gocyclo // Type 4 PS requires handling many operators in one switch.
func execOp(op string, stack *[]float64, _ []token) error {
	switch op {
	// Arithmetic operators.
	case "add":
		a, b, err := pop2(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		*stack = append(*stack, a+b)

	case "sub":
		a, b, err := pop2(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		*stack = append(*stack, a-b)

	case "mul":
		a, b, err := pop2(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		*stack = append(*stack, a*b)

	case "div":
		a, b, err := pop2(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		if b == 0 {
			return fmt.Errorf("postscript op %q: %w", op, ErrDivisionByZero)
		}
		*stack = append(*stack, a/b)

	case "idiv":
		a, b, err := pop2(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		if b == 0 {
			return fmt.Errorf("postscript op %q: %w", op, ErrDivisionByZero)
		}
		*stack = append(*stack, float64(int64(a)/int64(b)))

	case "mod":
		a, b, err := pop2(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		if b == 0 {
			return fmt.Errorf("postscript op %q: %w", op, ErrDivisionByZero)
		}
		*stack = append(*stack, float64(int64(a)%int64(b)))

	case "neg":
		a, err := pop1(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		*stack = append(*stack, -a)

	case "abs":
		a, err := pop1(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		*stack = append(*stack, math.Abs(a))

	case "ceiling":
		a, err := pop1(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		*stack = append(*stack, math.Ceil(a))

	case "floor":
		a, err := pop1(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		*stack = append(*stack, math.Floor(a))

	case "round":
		a, err := pop1(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		*stack = append(*stack, math.Round(a))

	case "truncate":
		a, err := pop1(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		*stack = append(*stack, math.Trunc(a))

	case "sqrt":
		a, err := pop1(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		*stack = append(*stack, math.Sqrt(a))

	case "sin":
		a, err := pop1(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		*stack = append(*stack, math.Sin(a*math.Pi/180))

	case "cos":
		a, err := pop1(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		*stack = append(*stack, math.Cos(a*math.Pi/180))

	case "atan":
		num, den, err := pop2(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		// ISO 32000-1, 7.10.5 takes the operator from the PostScript Language
		// Reference, whose atan answers in degrees from 0 to 360; math.Atan2
		// answers from -180 to 180. pdfjs colorspace_atan.pdf tests exactly
		// that: its tint transform compares the angle back to its input, and
		// half the image came out wrong.
		angle := math.Atan2(num, den) * 180 / math.Pi
		if angle < 0 {
			angle += 360
		}
		*stack = append(*stack, angle)

	case "exp":
		base, exponent, err := pop2(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		*stack = append(*stack, math.Pow(base, exponent))

	case "ln":
		a, err := pop1(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		*stack = append(*stack, math.Log(a))

	case "log":
		a, err := pop1(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		*stack = append(*stack, math.Log10(a))

	case "cvi":
		a, err := pop1(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		*stack = append(*stack, float64(int64(a)))

	case "cvr":
		// In Type 4, cvr is a no-op since the stack is all floats.
		if len(*stack) < 1 {
			return fmt.Errorf("postscript op %q: %w", op, ErrStackUnderflow)
		}

	// Stack operators.
	case "dup":
		if len(*stack) < 1 {
			return fmt.Errorf("postscript op %q: %w", op, ErrStackUnderflow)
		}
		*stack = append(*stack, (*stack)[len(*stack)-1])

	case "exch":
		n := len(*stack)
		if n < 2 {
			return fmt.Errorf("postscript op %q: %w", op, ErrStackUnderflow)
		}
		(*stack)[n-1], (*stack)[n-2] = (*stack)[n-2], (*stack)[n-1]

	case "pop":
		if len(*stack) < 1 {
			return fmt.Errorf("postscript op %q: %w", op, ErrStackUnderflow)
		}
		*stack = (*stack)[:len(*stack)-1]

	case "copy":
		count, err := pop1(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		// Compare as a float before converting: a negative count would slice
		// past the end, and int() of NaN or a huge value is not a usable
		// count.
		if math.IsNaN(count) || count < 0 || count > float64(len(*stack)) {
			return fmt.Errorf("postscript op %q: %w", op, ErrStackUnderflow)
		}
		n := int(count)
		*stack = append(*stack, (*stack)[len(*stack)-n:]...)

	case "index":
		idx, err := pop1(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		i := int(idx)
		if i < 0 || len(*stack) <= i {
			return fmt.Errorf("postscript op %q: %w", op, ErrStackUnderflow)
		}
		*stack = append(*stack, (*stack)[len(*stack)-1-i])

	case "roll":
		a, b, err := pop2(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		n := int(a)
		j := int(b)
		sLen := len(*stack)
		if n < 0 || sLen < n {
			return fmt.Errorf("postscript op %q: %w", op, ErrStackUnderflow)
		}
		if n == 0 {
			break
		}
		j %= n
		if j < 0 {
			j += n
		}
		if j == 0 {
			break
		}
		sub := (*stack)[sLen-n:]
		rotated := make([]float64, n)
		for i := range n {
			rotated[i] = sub[(i+n-j)%n]
		}
		copy(sub, rotated)

	// Comparison operators. Push 1.0 for true, 0.0 for false.
	case "eq":
		a, b, err := pop2(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		*stack = append(*stack, boolToFloat(a == b))

	case "ne":
		a, b, err := pop2(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		*stack = append(*stack, boolToFloat(a != b))

	case "gt":
		a, b, err := pop2(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		*stack = append(*stack, boolToFloat(a > b))

	case "ge":
		a, b, err := pop2(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		*stack = append(*stack, boolToFloat(a >= b))

	case "lt":
		a, b, err := pop2(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		*stack = append(*stack, boolToFloat(a < b))

	case "le":
		a, b, err := pop2(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		*stack = append(*stack, boolToFloat(a <= b))

	// Boolean / bitwise operators.
	case "and":
		a, b, err := pop2(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		*stack = append(*stack, float64(int64(a)&int64(b)))

	case "or":
		a, b, err := pop2(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		*stack = append(*stack, float64(int64(a)|int64(b)))

	case "xor":
		a, b, err := pop2(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		*stack = append(*stack, float64(int64(a)^int64(b)))

	case "not":
		a, err := pop1(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		if a == 0 {
			*stack = append(*stack, 1.0)
		} else {
			*stack = append(*stack, 0.0)
		}

	case "bitshift":
		a, b, err := pop2(stack)
		if err != nil {
			return fmt.Errorf("postscript op %q: %w", op, err)
		}
		val := int64(a)
		shift := int64(b)
		if shift >= 0 {
			*stack = append(*stack, float64(val<<uint(shift))) //nolint:gosec // Intentional PS bitshift semantics.
		} else {
			*stack = append(*stack, float64(val>>uint(-shift))) //nolint:gosec // Intentional PS bitshift semantics.
		}

	// Boolean constants.
	case "true":
		*stack = append(*stack, 1.0)

	case "false":
		*stack = append(*stack, 0.0)

	default:
		return fmt.Errorf("%w: %s", ErrUnknownOperator, op)
	}

	return nil
}

// execTokens evaluates a token sequence, handling blocks and control flow.
func execTokens(tokens []token, stack *[]float64) error {
	// We need a block stack to accumulate blocks for if/ifelse.
	var blocks [][]token

	for _, tok := range tokens {
		switch tok.kind {
		case tokenNumber:
			*stack = append(*stack, tok.num)

		case tokenBlock:
			blocks = append(blocks, tok.block)

		case tokenOperator:
			switch tok.value {
			case "if":
				if len(blocks) < 1 {
					return fmt.Errorf("%w: 'if' requires a code block", ErrSyntax)
				}
				body := blocks[len(blocks)-1]
				blocks = blocks[:len(blocks)-1]

				cond, err := pop1(stack)
				if err != nil {
					return fmt.Errorf("popping condition for 'if': %w", err)
				}
				if cond != 0 {
					if err := execTokens(body, stack); err != nil {
						return fmt.Errorf("executing 'if' body: %w", err)
					}
				}

			case "ifelse":
				if len(blocks) < 2 {
					return fmt.Errorf("%w: 'ifelse' requires two code blocks", ErrSyntax)
				}
				falseBody := blocks[len(blocks)-1]
				trueBody := blocks[len(blocks)-2]
				blocks = blocks[:len(blocks)-2]

				cond, err := pop1(stack)
				if err != nil {
					return fmt.Errorf("popping condition for 'ifelse': %w", err)
				}
				if cond != 0 {
					if err := execTokens(trueBody, stack); err != nil {
						return fmt.Errorf("executing 'ifelse' true branch: %w", err)
					}
				} else {
					if err := execTokens(falseBody, stack); err != nil {
						return fmt.Errorf("executing 'ifelse' false branch: %w", err)
					}
				}

			default:
				if err := execOp(tok.value, stack, nil); err != nil {
					return err
				}
			}
		}

		// Checking after every token is enough to bound memory: the stack
		// holds at most maxStackDepth entries going into any operator, and
		// no single operator more than doubles it.
		if len(*stack) > maxStackDepth {
			return fmt.Errorf("%w: more than %d entries", ErrStackOverflow, maxStackDepth)
		}
	}

	return nil
}

func pop1(stack *[]float64) (float64, error) {
	n := len(*stack)
	if n < 1 {
		return 0, ErrStackUnderflow
	}
	v := (*stack)[n-1]
	*stack = (*stack)[:n-1]
	return v, nil
}

func pop2(stack *[]float64) (float64, float64, error) {
	n := len(*stack)
	if n < 2 {
		return 0, 0, ErrStackUnderflow
	}
	b := (*stack)[n-1]
	a := (*stack)[n-2]
	*stack = (*stack)[:n-2]
	return a, b, nil
}

func boolToFloat(b bool) float64 {
	if b {
		return 1.0
	}
	return 0.0
}
