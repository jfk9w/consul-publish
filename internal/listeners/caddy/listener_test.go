package caddy

import (
	"bytes"
	"errors"
	"testing"
)

func TestWriteCommon(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		common string
		want   string
	}{
		{name: "empty"},
		{
			name:   "single line",
			common: "header >Alt-Svc `h3=\":443\"; ma=2592000`",
			want:   "    header >Alt-Svc `h3=\":443\"; ma=2592000`\n",
		},
		{
			name:   "multiline trimmed",
			common: "\nheader {\n    -Server\n}\n",
			want:   "    header {\n        -Server\n    }\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got bytes.Buffer
			listener := New(Config{Common: tt.common})
			if err := listener.writeCommon(&got); err != nil {
				t.Fatalf("writeCommon() error = %v", err)
			}
			if got.String() != tt.want {
				t.Errorf("writeCommon() = %q, want %q", got.String(), tt.want)
			}
		})
	}
}

func TestWriteCommonError(t *testing.T) {
	t.Parallel()

	listener := New(Config{Common: "header -Server"})
	if err := listener.writeCommon(errorWriter{}); err == nil {
		t.Fatal("writeCommon() error = nil, want write error")
	}
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}
