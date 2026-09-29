// Package calculator implements the binary arithmetic operations supported by
// HelloCalc. It is deliberately small: one operator applied to two operands.
package calculator

import (
	"errors"
	"math"
)

// Errors returned by Calculate. Their messages are safe to show to users.
var (
	ErrUnsupportedOperator = errors.New("unsupported operator")
	ErrDivisionByZero      = errors.New("division by zero")
	ErrInvalidOperand      = errors.New("operands must be finite numbers")
	ErrOutOfRange          = errors.New("result out of range")
	ErrNotRealNumber       = errors.New("result is not a real number")
)

// Calculate applies operator (one of + - * / % ^) to left and right and
// returns a finite result.
func Calculate(left float64, operator string, right float64) (float64, error) {
	if !isFinite(left) || !isFinite(right) {
		return 0, ErrInvalidOperand
	}

	var result float64
	switch operator {
	case "+":
		result = left + right
	case "-":
		result = left - right
	case "*":
		result = left * right
	case "/":
		if right == 0 {
			return 0, ErrDivisionByZero
		}
		result = left / right
	case "%":
		if right == 0 {
			return 0, ErrDivisionByZero
		}
		result = math.Mod(left, right)
	case "^":
		result = math.Pow(left, right)
	default:
		return 0, ErrUnsupportedOperator
	}

	switch {
	case math.IsNaN(result):
		return 0, ErrNotRealNumber
	case math.IsInf(result, 0):
		return 0, ErrOutOfRange
	case result == 0:
		// Normalize negative zero so clients never see "-0".
		return 0, nil
	}
	return result, nil
}

func isFinite(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}
