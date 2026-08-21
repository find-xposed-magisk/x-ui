package common

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// The message these builders produce is already formatted, so it must not be
// handed to a formatter again: a percent sign that survives into the text - a
// percent-encoded proxy URL, a remark like "50%off", an escaped path - would
// otherwise be read as a verb and reported as "%!s(MISSING)".
func TestErrorsKeepPercentSigns(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "NewErrorf with a percent-encoded value",
			err:  NewErrorf("proxy url <%v> is invalid", "socks5://user:p%40ss@host:1080"),
			want: "socks5://user:p%40ss@host:1080",
		},
		{
			name: "NewErrorf with a literal percent in the format",
			err:  NewErrorf("usage at 90%% of %v", "quota"),
			want: "usage at 90% of quota",
		},
		{
			name: "NewError with a percent in a value",
			err:  NewError("bad remark:", "50%off"),
			want: "50%off",
		},
		{
			name: "NewError with an escaped path",
			err:  NewError("path:", "/a%2Fb"),
			want: "/a%2Fb",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.err.Error()
			if !strings.Contains(got, test.want) {
				t.Errorf("error = %q, want it to contain %q", got, test.want)
			}
			if strings.Contains(got, "%!") {
				t.Errorf("error = %q, which carries a formatting artefact", got)
			}
		})
	}
}

// Going through fmt.Errorf rather than errors.New(fmt.Sprintf(...)) is what
// keeps %w meaningful: wrapping with the latter yields "%!w(...)" and breaks
// both errors.Is and errors.As, silently defeating any caller that wraps a
// cause.
func TestNewErrorfWraps(t *testing.T) {
	cause := errors.New("underlying cause")
	wrapped := NewErrorf("reading config: %w", cause)

	if !errors.Is(wrapped, cause) {
		t.Errorf("errors.Is could not find the wrapped cause in %q", wrapped)
	}
	if got := wrapped.Error(); got != "reading config: underlying cause" {
		t.Errorf("error = %q", got)
	}

	_, openErr := os.Open(t.TempDir() + "/missing")
	var pathErr *os.PathError
	if !errors.As(NewErrorf("open failed: %w", openErr), &pathErr) {
		t.Error("errors.As could not reach the wrapped *os.PathError")
	}
}

// NewError has no format string, so its arguments must be taken literally. The
// text is built through a variable because vet warns, reasonably, when a string
// constant that looks like a format reaches a non-printf function.
func TestNewErrorDoesNotFormat(t *testing.T) {
	literal := "literal " + "%s" + " and " + "%d" + " stay put"
	if got := NewError(literal).Error(); !strings.Contains(got, "%s and %d") {
		t.Errorf("error = %q, want the verbs left alone", got)
	}
}

// The builders' shapes are relied on by 88 call sites; keep them honest.
func TestErrorBuilders(t *testing.T) {
	if got := NewErrorf("port %d is not valid", 70000).Error(); got != "port 70000 is not valid" {
		t.Errorf("NewErrorf = %q", got)
	}
	// NewError joins its arguments with spaces, the way fmt.Sprintln does.
	if got := NewError("a", "b", 1).Error(); got != "a b 1\n" {
		t.Errorf("NewError = %q", got)
	}
	if NewError("x") == nil {
		t.Error("NewError returned nil")
	}
}
