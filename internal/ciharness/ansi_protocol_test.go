package ciharness_test

import (
	"image/color"
	"reflect"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
)

// TestANSIRejectsInvalidColorChannels exercises the actual selected parser.
func TestANSIRejectsInvalidColorChannels(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		"rgb:10000/0/0", "rgb:ffffffff/0/0", "rgb:wat/0/0", "rgb:/0/0", "rgb:-1/0/0",
		"rgb:00000/0/0",
		"rgba:0/0/0/10000", "rgba:0/0/0/ffffffff", "rgba:0/0/0/wat", "rgba:0/0/0/",
		"rgba:0/0/0/00000",
	} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()

			if actual := ansi.XParseColor(input); actual != nil {
				t.Errorf("invalid channel produced a color: %v", actual)
			}
		})
	}
}

// TestANSIValidColorChannelsPreserveNormalization protects supported input.
func TestANSIValidColorChannelsPreserveNormalization(t *testing.T) {
	t.Parallel()

	for input, expected := range map[string]color.RGBA{
		"rgb:ffff/0/8080":     {R: 255, G: 0, B: 128, A: 255},
		"rgb:ff/0/80":         {R: 255, G: 0, B: 128, A: 255},
		"rgba:ffff/0/80/abcd": {R: 255, G: 0, B: 128, A: 171},
	} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()

			if actual := ansi.XParseColor(input); actual != expected {
				t.Errorf("supported color changed: got %v, want %v", actual, expected)
			}
		})
	}
}

// TestANSIQuietModeRejectsOutOfRangeValues prevents wrapped protocol values
// while preserving valid modes and independently parsed options.
func TestANSIQuietModeRejectsOutOfRangeValues(t *testing.T) {
	t.Parallel()

	for input, expected := range map[string]byte{
		"q=-254": 1, "q=258": 1, "q=3": 1, "q=-1": 1,
		"q=0": 0, "q=1": 1, "q=2": 2,
	} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()

			var options kitty.Options

			quiet := quietMode(t, &options)
			quiet.SetUint(1)

			err := options.UnmarshalText([]byte("i=42," + input + ",p=21"))
			if err != nil {
				t.Fatal(err)
			}

			if quiet.Uint() != uint64(expected) || options.ID != 42 || options.PlacementID != 21 {
				t.Errorf("incorrect parsed options: %+v", options)
			}
		})
	}
}

// quietMode returns the field the selected parser fills for q=: the root
// parser renamed the misspelled Quite to Quiet, while Glamour's standalone
// parser still only has Quite.
func quietMode(t *testing.T, options *kitty.Options) reflect.Value {
	t.Helper()

	value := reflect.ValueOf(options).Elem()

	field := value.FieldByName("Quiet")
	if !field.IsValid() {
		field = value.FieldByName("Quite")
	}

	if !field.IsValid() || field.Kind() != reflect.Uint8 {
		t.Fatalf("kitty.Options has no byte quiet mode field: %+v", *options)
	}

	return field
}
