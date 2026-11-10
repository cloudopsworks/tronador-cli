package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestUpdateNotice(t *testing.T) {
	for _, tt := range []struct {
		name, current, body string
		status              int
		want                bool
	}{
		{"new patch", "0.5.12", `{"tag_name":"v0.5.13"}`, 200, true},
		{"numeric ordering", "v0.9.9", `{"tag_name":"v0.10.0"}`, 200, true},
		{"same version", "v0.5.12", `{"tag_name":"v0.5.12"}`, 200, false},
		{"older release", "1.0.0", `{"tag_name":"v0.5.12"}`, 200, false},
		{"same core stable", "0.5.13-beta.2", `{"tag_name":"v0.5.13"}`, 200, true},
		{"newer prerelease installed", "0.6.0-beta.2", `{"tag_name":"v0.5.13"}`, 200, false},
		{"metadata ignored", "0.5.13+build.5", `{"tag_name":"v0.5.13"}`, 200, false},
		{"draft", "0.5.12", `{"tag_name":"v0.5.13","draft":true}`, 200, false},
		{"prerelease", "0.5.12", `{"tag_name":"v0.5.13","prerelease":true}`, 200, false},
		{"prerelease tag", "0.5.12", `{"tag_name":"v0.5.13-beta.1"}`, 200, false},
		{"invalid tag", "0.5.12", `{"tag_name":"v0.5.013"}`, 200, false},
		{"missing tag", "0.5.12", `{}`, 200, false},
		{"malformed json", "0.5.12", `{`, 200, false},
		{"trailing json", "0.5.12", `{"tag_name":"v0.5.13"}{}`, 200, false},
		{"rate limited", "0.5.12", `{"tag_name":"v0.5.13"}`, 403, false},
		{"server failure", "0.5.12", `{"tag_name":"v0.5.13"}`, 500, false},
		{"oversized body", "0.5.12", `{"tag_name":"v0.5.13","body":"` + strings.Repeat("x", 65536) + `"}`, 200, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.Header.Get("Accept") != "application/vnd.github+json" {
					t.Errorf("unexpected request: %s %v", r.Method, r.Header)
				}
				if r.Header.Get("Authorization") != "" {
					t.Error("public check must not send credentials")
				}
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			}))
			defer server.Close()
			var output bytes.Buffer
			notifyUpdate(context.Background(), &output, tt.current, server.Client(), server.URL)
			if tt.want {
				want := "A new version of tronador is available: v0.5.13 (current: " + tt.current + "). https://github.com/cloudopsworks/tronador-cli/releases/latest\n"
				if tt.name == "numeric ordering" {
					want = strings.ReplaceAll(want, "v0.5.13", "v0.10.0")
				}
				if output.String() != want {
					t.Fatalf("notice = %q; want %q", output.String(), want)
				}
			} else if output.Len() != 0 {
				t.Fatalf("unexpected output: %q", output.String())
			}
		})
	}
}

type updateTransport func(*http.Request) (*http.Response, error)

func (f updateTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestUpdateCheckSkipsUnknownBuilds(t *testing.T) {
	client := &http.Client{Transport: updateTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("unknown version must not make a network request")
		return nil, nil
	})}
	for _, version := range []string{"dev", "", "invalid", "v1.2", "1.02.3", "1.2.3-beta.01"} {
		var output bytes.Buffer
		notifyUpdate(context.Background(), &output, version, client, "https://example.invalid")
		if output.Len() != 0 {
			t.Fatalf("unexpected output for %q: %q", version, output.String())
		}
	}
}

func TestUpdateCheckFailureDoesNotChangeCommandResult(t *testing.T) {
	for _, commandErr := range []error{nil, errors.New("command failure")} {
		var output bytes.Buffer
		command := &cobra.Command{Use: "test", SilenceErrors: true, SilenceUsage: true, RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "command output")
			return commandErr
		}}
		command.SetArgs(nil)
		command.SetOut(&output)
		command.SetErr(&output)
		client := &http.Client{Transport: updateTransport(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("offline")
		})}
		if err := executeWithUpdateNotice(command, "1.2.3", client, "https://example.invalid"); err != commandErr {
			t.Fatalf("result = %v; want %v", err, commandErr)
		}
		if output.String() != "command output\n" {
			t.Fatalf("output = %q", output.String())
		}
	}
}

func TestUpdateNoticePrecedesAllCommandOutput(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"--unknown"}} {
		var output bytes.Buffer
		command := &cobra.Command{Use: "test", Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), "command output")
		}}
		command.SetArgs(args)
		command.SetOut(&output)
		command.SetErr(&output)
		client := &http.Client{Transport: updateTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"tag_name":"v1.2.4"}`)), Header: make(http.Header)}, nil
		})}
		_ = executeWithUpdateNotice(command, "1.2.3", client, "https://example.invalid")
		if !strings.HasPrefix(output.String(), "A new version of tronador is available: v1.2.4") {
			t.Fatalf("notice is not first for %v: %q", args, output.String())
		}
		if strings.Count(output.String(), "A new version") != 1 {
			t.Fatalf("notice repeated: %q", output.String())
		}
	}
}

func TestUpdateCheckTimeoutIsSilentAndFast(t *testing.T) {
	for _, slowBody := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if slowBody {
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
			}
			<-r.Context().Done()
		}))
		var output bytes.Buffer
		start := time.Now()
		notifyUpdate(context.Background(), &output, "1.2.3", server.Client(), server.URL)
		elapsed := time.Since(start)
		server.Close()
		if elapsed > time.Second {
			t.Fatalf("check took %s; timeout should be %s", elapsed, updateCheckTimeout)
		}
		if output.Len() != 0 {
			t.Fatalf("timeout output: %q", output.String())
		}
	}
}
