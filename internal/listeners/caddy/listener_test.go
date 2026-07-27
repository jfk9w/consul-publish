package caddy

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/jfk9w/consul-publish/internal/consul"
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

func TestForwardAuth(t *testing.T) {
	t.Parallel()

	state := &consul.State{
		Self: "caddy",
		Nodes: map[string]consul.Node{
			"caddy": {
				Name:    "caddy",
				Address: "10.0.0.1",
			},
		},
	}
	instance := Instance{
		Node: consul.Node{
			Name: "backend",
			Services: []consul.Service{
				{ID: "authelia", Address: "10.0.0.2", Port: 9091},
			},
		},
		Service: consul.Service{ID: "app"},
	}

	got, err := New(Config{Auth: "authelia"}).auth(state, instance, 4)
	if err != nil {
		t.Fatalf("auth() error = %v", err)
	}

	want := "forward_auth 10.0.0.2:9091 { \n" +
		"    \turi /api/authz/forward-auth\n" +
		"    \tcopy_headers Remote-User Remote-Groups Remote-Email Remote-Name\n" +
		"    }\n"
	if got != want {
		t.Errorf("auth() = %q, want %q", got, want)
	}
}

func TestForwardAuthDisabled(t *testing.T) {
	t.Parallel()

	got, err := New(Config{}).auth(&consul.State{}, Instance{}, 0)
	if err != nil {
		t.Fatalf("auth() error = %v", err)
	}
	if got != "" {
		t.Errorf("auth() = %q, want empty string", got)
	}
}

func TestForwardAuthServiceNotFound(t *testing.T) {
	t.Parallel()

	state := &consul.State{
		Self: "backend",
		Nodes: map[string]consul.Node{
			"backend": {Name: "backend"},
		},
	}
	instance := Instance{
		Node: consul.Node{
			Name: "backend",
			Services: []consul.Service{
				{ID: "app"},
				{ID: "authelia-metrics"},
			},
		},
		Service: consul.Service{ID: "app"},
	}

	_, err := New(Config{Auth: "authelia header -Alt-Svc"}).auth(state, instance, 0)
	if err == nil {
		t.Fatal("auth() error = nil, want missing service error")
	}

	for _, part := range []string{
		`forward auth service "authelia header -Alt-Svc" not found`,
		`node "backend"`,
		`service "app"`,
		"app, authelia-metrics",
	} {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("auth() error = %q, want it to contain %q", err, part)
		}
	}
}

func TestForwardAuthFailureStopsTemplateExecution(t *testing.T) {
	t.Parallel()

	state := &consul.State{
		Self: "backend",
		Nodes: map[string]consul.Node{
			"backend": {Name: "backend"},
		},
	}
	instance := Instance{
		Node:    consul.Node{Name: "backend"},
		Service: consul.Service{ID: "app"},
	}
	listener := New(Config{Auth: "authelia"})
	tmpl, err := listener.tmpl(state, map[string]consul.Value{
		"app": []byte(`route { [[ ForwardAuth 8 ]] }`),
	}, instance)
	if err != nil {
		t.Fatalf("tmpl() error = %v", err)
	}

	var output bytes.Buffer
	err = tmpl.Execute(&output, instance)
	if err == nil {
		t.Fatal("Execute() error = nil, want missing forward auth service error")
	}
	if !strings.Contains(err.Error(), `forward auth service "authelia" not found`) {
		t.Errorf("Execute() error = %q, want forward auth lookup error", err)
	}
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}
