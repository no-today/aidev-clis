package exec

import (
	"context"
	"runtime"
	"testing"
)

func TestLocal_StreamsLines(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses unix printf")
	}
	var got []string
	err := Local{}.Run(context.Background(),
		Spec{Argv: []string{"printf", "a\nb\nc\n"}},
		func(line string) error { got = append(got, line); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Fatalf("got %v", got)
	}
}

func TestLocal_NonZeroExitIsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses unix false")
	}
	err := Local{}.Run(context.Background(), Spec{Argv: []string{"false"}}, func(string) error { return nil })
	if err == nil {
		t.Fatal("non-zero exit must be an error")
	}
}

func TestLocal_MissingBinary(t *testing.T) {
	err := Local{}.Run(context.Background(), Spec{Argv: []string{"definitely-not-a-real-binary-xyz"}}, func(string) error { return nil })
	if err == nil {
		t.Fatal("missing binary must error")
	}
}
