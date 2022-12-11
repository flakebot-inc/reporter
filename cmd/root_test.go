package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matryer/is"
	"github.com/spf13/cobra"
)

func MockS3Server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}))
}

func MockFlakebotServer(apiKey string, s3Url string) (*httptest.Server, *Report) {
	report := &Report{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if r.Header.Get("X-REPORTER-KEY") != apiKey {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		if r.URL.Path == "/reports/upload/" {
			data := PresignedUrl{
				Url: s3Url,
			}
			data.Fields.AWSAccessKeyId = "aws-access-key-id"
			data.Fields.Policy = "policy"
			data.Fields.Signature = "signature"
			data.Fields.Key = "key"

			json.NewEncoder(w).Encode(data)
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "application/json")
		} else if r.URL.Path == "/reports/" {
			defer r.Body.Close()

			json.NewDecoder(r.Body).Decode(report)

			w.WriteHeader(http.StatusCreated)

		}
	})), report
}

func execute(t *testing.T, c *cobra.Command, args ...string) (string, error) {
	t.Helper()

	buf := new(bytes.Buffer)
	c.SetOut(buf)
	c.SetErr(buf)
	c.SetArgs(args)

	err := c.Execute()
	return strings.TrimSpace(buf.String()), err
}

func TestWithoutArguments(t *testing.T) {
	is := is.New(t)

	_, err := execute(t, rootCmd)

	is.Equal(errors.New("accepts 1 arg(s), received 0"), err)
}

func TestWithoutApiKey(t *testing.T) {
	is := is.New(t)

	_, err := execute(t, rootCmd, "../fixtures")

	is.Equal(errors.New("Could not find environment variable, FLAKEBOT_REPORTER_KEY"), err)
}

var MetadataEnv = map[string]string{
	"GITHUB_JOB":         "1",
	"GITHUB_REF":         "refs/pull/1/merge",
	"GITHUB_REF_NAME":    "feature-branch-1",
	"GITHUB_REF_TYPE":    "branch",
	"GITHUB_REPOSITORY":  "flakebot-inc/reporter",
	"GITHUB_RUN_ID":      "1",
	"GITHUB_SHA":         "2a50735d3d7125ddee01fbc1f945c280bf348eda",
	"GITHUB_RUN_ATTEMPT": "1",
	"RUNNER_ARCH":        "ARM64",
	"RUNNER_OS":          "macOS",
	"RUNNER_TEMP":        "./temp",
}

func TestSuccess(t *testing.T) {
	is := is.New(t)

	t.Setenv("FLAKEBOT_REPORTER_KEY", "rk_test")
	for key, element := range MetadataEnv {
		t.Setenv(key, element)
	}

	s3Server := MockS3Server()

	flakebotServer, report := MockFlakebotServer("rk_test", s3Server.URL)

	_, err := execute(t, rootCmd, "../fixtures", "--api", flakebotServer.URL)
	is.NoErr(err)

	is.Equal(report.Archive, "key")
	is.Equal(report.Provider, "github_action")

	for key, element := range MetadataEnv {
		is.Equal(report.Metadata[key], element)
	}
}
