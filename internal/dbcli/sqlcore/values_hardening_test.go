package sqlcore

import (
	"strconv"
	"testing"
)

func TestCoerce_BinaryToBase64(t *testing.T) {
	bin := []byte{0xff, 0xfe, 0x00, 0x01} // invalid UTF-8
	got, ok := Coerce(bin).(string)
	if !ok || got != "//4AAQ==" {
		t.Fatalf("binary []byte should base64-encode, got %v", Coerce(bin))
	}
}

func TestCoerce_BigIntToString(t *testing.T) {
	big := int64(9007199254740993) // 2^53 + 1, not exactly representable as float64
	if got := Coerce(big); got != strconv.FormatInt(big, 10) {
		t.Fatalf("big int64 should stringify, got %T %v", got, got)
	}
	small := int64(42)
	if got := Coerce(small); got != int64(42) {
		t.Fatalf("small int64 stays numeric, got %T %v", got, got)
	}
}

type decimalish struct{ s string }

func (d decimalish) String() string { return d.s }

func TestCoerce_StringerDecimal(t *testing.T) {
	if got := Coerce(decimalish{"12.3400"}); got != "12.3400" {
		t.Fatalf("Stringer should render exact string, got %v", got)
	}
}
