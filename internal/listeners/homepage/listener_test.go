package homepage

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jfk9w/consul-publish/internal/consul"
	"github.com/jfk9w/consul-publish/internal/listeners"
	"gopkg.in/yaml.v3"
)

func TestNotifyReloadsOnlyAfterChange(t *testing.T) {
	t.Parallel()

	currentUser, err := user.Current()
	if err != nil {
		t.Fatalf("get current user: %v", err)
	}
	currentGroup, err := user.LookupGroupId(currentUser.Gid)
	if err != nil {
		t.Fatalf("get current group: %v", err)
	}

	dir := t.TempDir()
	marker := filepath.Join(dir, "reloaded")
	listener := New(Config{
		KV:   "homepage",
		Exec: fmt.Sprintf("touch %q", marker),
		Services: listeners.File{
			Path:  filepath.Join(dir, "services.yaml"),
			Mode:  0o644,
			User:  currentUser.Username,
			Group: currentGroup.Name,
		},
	})
	state := &consul.State{
		Nodes: map[string]consul.Node{
			"node": {Name: "node", Services: []consul.Service{{ID: "app", Meta: map[string]string{
				listeners.HomepagePathKey:    "Apps/App",
				listeners.PublishHomepageKey: "all",
			}}}},
		},
		Self: "node",
		KV:   consul.Folder{"homepage": consul.Folder{"app": consul.Value("href: /")}},
	}

	if err := listener.Notify(context.Background(), state); err != nil {
		t.Fatalf("first Notify() error = %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("reload marker after changed configuration: %v", err)
	}

	if err := os.Remove(marker); err != nil {
		t.Fatalf("remove reload marker: %v", err)
	}
	if err := listener.Notify(context.Background(), state); err != nil {
		t.Fatalf("second Notify() error = %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("reload command ran for unchanged configuration: stat error = %v", err)
	}
}

func TestWrite(t *testing.T) {
	t.Parallel()

	state := &consul.State{
		Self: "homepage",
		Nodes: map[string]consul.Node{
			"homepage": {Name: "homepage", Address: "10.0.0.1"},
			"backend-a": {
				Name: "backend-a",
				Services: []consul.Service{{
					ID: "grafana", Name: "grafana", Address: "10.0.0.2", Port: 3000,
					Meta: map[string]string{
						listeners.HomepagePathKey:    " Home / Grafana ",
						listeners.PublishHomepageKey: "all",
					},
				}},
			},
			"backend-b": {
				Name: "backend-b",
				Services: []consul.Service{
					{
						ID: "grafana", Name: "grafana", Address: "10.0.0.3", Port: 3000,
						Meta: map[string]string{
							listeners.HomepagePathKey:    "Monitoring/Grafana",
							listeners.PublishHomepageKey: "all",
						},
					},
					{ID: "hidden", Meta: map[string]string{
						listeners.HomepagePathKey:    "Home/Hidden",
						listeners.PublishHomepageKey: "other",
					}},
				},
			},
		},
	}
	definitions := map[string]consul.Value{
		"grafana": []byte("href: http://[[ (index . 0).Service.Address ]]:[[ (index . 0).Service.Port ]]\nwidget:\n  type: grafana"),
		"hidden":  []byte("href: http://hidden"),
	}

	var got bytes.Buffer
	if err := New(Config{}).write(state, &got, definitions); err != nil {
		t.Fatalf("write() error = %v", err)
	}

	want := "- Home:\n" +
		"    - Grafana:\n" +
		"        href: http://10.0.0.2:3000\n" +
		"        widget:\n" +
		"          type: grafana\n" +
		"\n" +
		"- Monitoring:\n" +
		"    - Grafana:\n" +
		"        href: http://10.0.0.2:3000\n" +
		"        widget:\n" +
		"          type: grafana\n"
	if got.String() != want {
		t.Errorf("write() = %q, want %q", got.String(), want)
	}

	var document any
	if err := yaml.Unmarshal(got.Bytes(), &document); err != nil {
		t.Errorf("generated configuration is not valid YAML: %v", err)
	}
}

func TestWriteUsesLocalAddress(t *testing.T) {
	t.Parallel()

	state := &consul.State{
		Self: "homepage",
		Nodes: map[string]consul.Node{
			"homepage": {
				Name: "homepage", Address: "10.0.0.1",
				Services: []consul.Service{{
					ID: "app", Address: "10.0.0.1", Port: 8080,
					Meta: map[string]string{
						listeners.HomepagePathKey:    "Apps/App",
						listeners.PublishHomepageKey: "all",
					},
				}},
			},
		},
	}

	var got bytes.Buffer
	err := New(Config{}).write(state, &got, map[string]consul.Value{
		"app": []byte("href: http://[[ (index . 0).Service.Address ]]:[[ (index . 0).Service.Port ]]"),
	})
	if err != nil {
		t.Fatalf("write() error = %v", err)
	}
	if !strings.Contains(got.String(), "http://127.0.0.1:8080") {
		t.Errorf("write() = %q, want local address", got.String())
	}
}

func TestWriteAllowsSpacesInPathElements(t *testing.T) {
	t.Parallel()

	state := &consul.State{Self: "node", Nodes: map[string]consul.Node{
		"node": {Name: "node", Services: []consul.Service{{ID: "home-assistant", Meta: map[string]string{
			listeners.HomepagePathKey:    " Дом / Home Assistant ",
			listeners.PublishHomepageKey: "all",
		}}}},
	}}

	var got bytes.Buffer
	err := New(Config{}).write(state, &got, map[string]consul.Value{
		"home-assistant": []byte("href: /"),
	})
	if err != nil {
		t.Fatalf("write() error = %v", err)
	}

	want := "- Дом:\n" +
		"    - Home Assistant:\n" +
		"        href: /\n"
	if got.String() != want {
		t.Errorf("write() = %q, want %q", got.String(), want)
	}
}

func TestWriteRejectsInvalidGroup(t *testing.T) {
	t.Parallel()

	state := &consul.State{Self: "node", Nodes: map[string]consul.Node{
		"node": {Name: "node", Services: []consul.Service{{ID: "app", Meta: map[string]string{
			listeners.HomepagePathKey:    "invalid",
			listeners.PublishHomepageKey: "all",
		}}}},
	}}

	err := New(Config{}).write(state, &bytes.Buffer{}, map[string]consul.Value{"app": []byte("href: /")})
	if err == nil || !strings.Contains(err.Error(), "expected group/service-name") {
		t.Fatalf("write() error = %v, want invalid group error", err)
	}
}

func TestWriteTemplateError(t *testing.T) {
	t.Parallel()

	state := &consul.State{Self: "node", Nodes: map[string]consul.Node{
		"node": {Name: "node", Services: []consul.Service{{ID: "app", Meta: map[string]string{
			listeners.HomepagePathKey:    "Apps/App",
			listeners.PublishHomepageKey: "all",
		}}}},
	}}

	err := New(Config{}).write(state, &bytes.Buffer{}, map[string]consul.Value{"app": []byte("[[")})
	if err == nil || !strings.Contains(err.Error(), "parse template for app") {
		t.Fatalf("write() error = %v, want template parse error", err)
	}
}
