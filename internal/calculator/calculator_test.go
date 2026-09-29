package calculator

import (
	"errors"
	"math"
	"testing"
)

func TestCalculate(t *testing.T) {
	tests := []struct {
		name  string
		left  float64
		op    string
		right float64
		want  float64
	}{
		{"addition", 1, "+", 2, 3},
		{"subtraction", 5, "-", 8, -3},
		{"multiplication", -7, "*", 3, -21},
		{"division", 10, "/", 4, 2.5},
		{"float multiplication", 12.5, "*", 4, 50},
		{"float addition", 0.5, "+", 0.25, 0.75},
		{"negative operands", -2.5, "*", -2, 5},
		{"negative division", -9, "/", 3, -3},
		{"modulo", 7, "%", 3, 1},
		{"negative modulo", -7, "%", 3, -1},
		{"float modulo", 5.5, "%", 2, 1.5},
		{"power", 2, "^", 10, 1024},
		{"negative exponent", 2, "^", -1, 0.5},
		{"fractional exponent", 9, "^", 0.5, 3},
		{"negative zero normalized", -0.0, "*", 5, 0},
		{"zero divided", 0, "/", 5, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Calculate(tt.left, tt.op, tt.right)
			if err != nil {
				t.Fatalf("Calculate(%v, %q, %v) returned error: %v", tt.left, tt.op, tt.right, err)
			}
			if got != tt.want {
				t.Fatalf("Calculate(%v, %q, %v) = %v, want %v", tt.left, tt.op, tt.right, got, tt.want)
			}
			if math.Signbit(got) && got == 0 {
				t.Fatalf("Calculate(%v, %q, %v) returned negative zero", tt.left, tt.op, tt.right)
			}
		})
	}
}

func TestCalculateFloatingPointPrecision(t *testing.T) {
	got, err := Calculate(0.1, "+", 0.2)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got-0.3) > 1e-15 {
		t.Fatalf("0.1 + 0.2 = %v, want approximately 0.3", got)
	}
}

func TestCalculateErrors(t *testing.T) {
	tests := []struct {
		name  string
		left  float64
		op    string
		right float64
		want  error
	}{
		{"division by zero", 1, "/", 0, ErrDivisionByZero},
		{"zero divided by zero", 0, "/", 0, ErrDivisionByZero},
		{"division by negative zero", 1, "/", math.Copysign(0, -1), ErrDivisionByZero},
		{"modulo by zero", 5, "%", 0, ErrDivisionByZero},
		{"unsupported operator", 1, "&", 2, ErrUnsupportedOperator},
		{"empty operator", 1, "", 2, ErrUnsupportedOperator},
		{"word operator", 1, "plus", 2, ErrUnsupportedOperator},
		{"overflow", math.MaxFloat64, "*", 10, ErrOutOfRange},
		{"power overflow", 10, "^", 400, ErrOutOfRange},
		{"zero to negative power", 0, "^", -1, ErrOutOfRange},
		{"root of negative", -8, "^", 0.5, ErrNotRealNumber},
		{"infinite operand", math.Inf(1), "+", 1, ErrInvalidOperand},
		{"NaN operand", 1, "+", math.NaN(), ErrInvalidOperand},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Calculate(tt.left, tt.op, tt.right)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Calculate(%v, %q, %v) error = %v, want %v", tt.left, tt.op, tt.right, err, tt.want)
			}
		})
	}
}
