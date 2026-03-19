package protocol

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const AmountScale int64 = 1_000_000

type Amount int64

func ParseAmountString(raw string) (Amount, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, fmt.Errorf("amount is required")
	}

	negative := false
	switch value[0] {
	case '-':
		negative = true
		value = value[1:]
	case '+':
		value = value[1:]
	}

	if value == "" {
		return 0, fmt.Errorf("amount is required")
	}

	parts := strings.SplitN(value, ".", 2)
	if len(parts) > 2 {
		return 0, fmt.Errorf("invalid amount %q", raw)
	}

	wholePart := parts[0]
	if wholePart == "" {
		wholePart = "0"
	}
	whole, err := strconv.ParseInt(wholePart, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse amount whole part: %w", err)
	}

	var fractional int64
	if len(parts) == 2 {
		fraction := parts[1]
		if fraction == "" {
			fraction = "0"
		}
		if len(fraction) > 6 {
			return 0, fmt.Errorf("amount supports at most 6 fractional digits")
		}
		fraction = fraction + strings.Repeat("0", 6-len(fraction))
		fractional, err = strconv.ParseInt(fraction, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse amount fractional part: %w", err)
		}
	}

	maxWhole := math.MaxInt64 / AmountScale
	maxFractional := math.MaxInt64 % AmountScale
	if whole > maxWhole || (whole == maxWhole && fractional > maxFractional) {
		return 0, fmt.Errorf("amount overflow")
	}

	total := whole*AmountScale + fractional
	if negative {
		total = -total
	}
	return Amount(total), nil
}

func AmountFromFloat(value float64) (Amount, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("invalid amount")
	}
	scaled := math.Round(value * float64(AmountScale))
	if scaled > math.MaxInt64 || scaled < math.MinInt64 {
		return 0, fmt.Errorf("amount overflow")
	}
	return Amount(int64(scaled)), nil
}

func (a Amount) Float64() float64 {
	return float64(a) / float64(AmountScale)
}

func (a Amount) String() string {
	value := int64(a)
	if value == 0 {
		return "0"
	}

	sign := ""
	var abs uint64
	if value < 0 {
		sign = "-"
		abs = uint64(-(value + 1))
		abs++
	} else {
		abs = uint64(value)
	}

	whole := abs / uint64(AmountScale)
	fractional := abs % uint64(AmountScale)
	if fractional == 0 {
		return sign + strconv.FormatUint(whole, 10)
	}

	fractionText := strconv.FormatUint(fractional+uint64(AmountScale), 10)[1:]
	fractionText = strings.TrimRight(fractionText, "0")
	return sign + strconv.FormatUint(whole, 10) + "." + fractionText
}

func (a Amount) MarshalJSON() ([]byte, error) {
	return []byte(a.String()), nil
}

func (a *Amount) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		return fmt.Errorf("amount is required")
	}

	if strings.HasPrefix(trimmed, "\"") {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		value, err := ParseAmountString(text)
		if err != nil {
			return err
		}
		*a = value
		return nil
	}

	value, err := ParseAmountString(trimmed)
	if err != nil {
		return err
	}
	*a = value
	return nil
}

func (a Amount) Value() (driver.Value, error) {
	return int64(a), nil
}

func (a *Amount) Scan(src any) error {
	switch value := src.(type) {
	case nil:
		*a = 0
		return nil
	case int64:
		*a = Amount(value)
		return nil
	case int32:
		*a = Amount(value)
		return nil
	case int:
		*a = Amount(value)
		return nil
	case float64:
		parsed, err := AmountFromFloat(value)
		if err != nil {
			return err
		}
		*a = parsed
		return nil
	case []byte:
		parsed, err := ParseAmountString(string(value))
		if err != nil {
			return err
		}
		*a = parsed
		return nil
	case string:
		parsed, err := ParseAmountString(value)
		if err != nil {
			return err
		}
		*a = parsed
		return nil
	default:
		return fmt.Errorf("unsupported amount scan type %T", src)
	}
}
