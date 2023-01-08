package cmd

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/spf13/cobra"
)

var FlakebotApiUrl string

const DefaultFlakebotApiUrl = "https://api.flakebot.com"

type PresignedUrl struct {
	Url    string `json:"url"`
	Fields struct {
		Key            string `json:"key"`
		AWSAccessKeyId string `json:"AWSAccessKeyId"`
		Policy         string `json:"policy"`
		Signature      string `json:"signature"`
	} `json:"fields"`
}

type Report struct {
	Archive  string                 `json:"archive"`
	Provider string                 `json:"provider"`
	Metadata map[string]interface{} `json:"metadata"`
}

var rootCmd = &cobra.Command{
	Use:   "reporter",
	Short: "Reporting tool for Flakebot",
	Long:  `The reporter command sends your test reports to Flakebot for processing and analysis.`,
	Args:  cobra.MatchAll(cobra.ExactArgs(1), ValidatePath, ValidateKey),
	RunE:  HandleReport,
}

func ValidateKey(cmd *cobra.Command, args []string) error {
	key := os.Getenv("FLAKEBOT_REPORTER_KEY")

	if len(key) == 0 {
		return errors.New("Could not find environment variable, FLAKEBOT_REPORTER_KEY")
	}

	return nil
}

func ValidatePath(cmd *cobra.Command, args []string) error {
	xmlRegex := regexp.MustCompile(`.xml`)

	path := args[0]
	pathInfo, err := os.Stat(path)

	if os.IsNotExist(err) {
		return errors.New("Provided path does not exist.")
	}

	if pathInfo.IsDir() {
		files, e := os.ReadDir(path)
		if e != nil {
			panic(e)
		}

		if len(files) == 0 {
			return errors.New("Directory is empty.")
		}

		valid := false

		for _, file := range files {
			if !file.IsDir() && xmlRegex.MatchString(file.Name()) {
				valid = true
				break
			}
		}

		if !valid {
			return errors.New("No valid .xml files in directory.")
		}
	} else if !xmlRegex.MatchString(path) {
		return errors.New("Path is not valid .xml file.")
	}

	return nil
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().StringVarP(&FlakebotApiUrl, "api", "a", DefaultFlakebotApiUrl, "Override the Flakebot API Url")
}

func GetPresignedUrl(apiKey string) (error, PresignedUrl) {
	data := PresignedUrl{}

	req, err := http.NewRequest(http.MethodPost, FlakebotApiUrl+"/reports/upload/", bytes.NewReader([]byte{}))
	if err != nil {
		return nil, data
	}

	req.Header.Set("X-Reporter-Key", apiKey)

	client := http.Client{
		Timeout: 30 * time.Second,
	}

	res, err := client.Do(req)
	if err != nil {
		return err, data
	}

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("Bad Status: %s", res.Status), data
	}

	resBody, err := ioutil.ReadAll(res.Body)
	if err != nil {
		fmt.Printf("client: could not read response body: %s\n", err)
		os.Exit(1)
	}

	err = json.Unmarshal(resBody, &data)

	return err, data

}

func CreateArchive(path string) (error, string) {
	info, err := os.Stat(path)
	if err != nil {
		return err, ""
	}

	report, err := os.Create("report.zip")
	if err != nil {
		return err, ""
	}

	defer report.Close()

	w := zip.NewWriter(report)
	defer w.Close()

	if info.IsDir() {
		walker := func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			f, err := w.Create(path)
			if err != nil {
				return err
			}

			_, err = io.Copy(f, file)
			if err != nil {
				return err
			}

			return nil
		}

		err = filepath.Walk(path, walker)

		return err, report.Name()
	}

	file, err := os.Open(path)
	if err != nil {
		return err, ""
	}
	defer file.Close()

	f, err := w.Create(filepath.Base(file.Name()))
	if err != nil {
		return err, ""
	}

	_, err = io.Copy(f, file)
	if err != nil {
		return err, ""
	}

	return nil, report.Name()
}

func UploadArchive(path string, presignedUrl PresignedUrl) error {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	_ = w.WriteField("key", presignedUrl.Fields.Key)
	_ = w.WriteField("AWSAccessKeyId", presignedUrl.Fields.AWSAccessKeyId)
	_ = w.WriteField("policy", presignedUrl.Fields.Policy)
	_ = w.WriteField("signature", presignedUrl.Fields.Signature)

	file, _ := os.Open(path)
	defer file.Close()

	fileField, _ := w.CreateFormFile("file", filepath.Base(path))
	io.Copy(fileField, file)

	w.Close()

	req, err := http.NewRequest("POST", presignedUrl.Url, &b)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return err
	}

	if res.StatusCode != http.StatusNoContent {
		return fmt.Errorf("Bad Status: %s", res.Status)
	}

	return nil
}

func CreateReport(report Report, apiKey string) error {
	body, err := json.Marshal(report)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, FlakebotApiUrl+"/reports/", bytes.NewReader(body))
	if err != nil {
		return nil
	}

	req.Header.Set("X-Reporter-Key", apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := http.Client{
		Timeout: 30 * time.Second,
	}

	res, err := client.Do(req)
	if err != nil {
		return err
	}

	if res.StatusCode != http.StatusCreated {
		return fmt.Errorf("Bad Status: %s", res.Status)
	}

	return nil
}

// Provider class to get Metadata
type Provider struct {
	Name        string
	GetMetadata func(map[string]interface{}) error
}

func GetProvider() (Provider, error) {
	switch {
	case os.Getenv("CIRCLECI") == "true":
		return Provider{
			Name:        "circle_ci",
			GetMetadata: GetCircleCIMetadata,
		}, nil
	case os.Getenv("GITHUB_ACTIONS") == "true":
		return Provider{
			Name:        "github_action",
			GetMetadata: GetGitHubMetadata,
		}, nil
	default:
		return Provider{}, fmt.Errorf("Environment does not appear to be a supported CI provider (CircleCI, GitHub Actions, etc.)")
	}
}

func GetGitHubMetadata(metadata map[string]interface{}) error {
	var githubEnvs = []string{
		"GITHUB_JOB",
		"GITHUB_REF",
		"GITHUB_REF_NAME",
		"GITHUB_REF_TYPE",
		"GITHUB_REPOSITORY",
		"GITHUB_RUN_ID",
		"GITHUB_SHA",
		"GITHUB_RUN_ATTEMPT",
		"RUNNER_ARCH",
		"RUNNER_OS",
		"RUNNER_TEMP",
	}

	for _, env := range githubEnvs {
		metadata[env] = os.Getenv(env)
	}

	return nil
}

func GetCircleCIMetadata(metadata map[string]interface{}) error {
	var circleciEnvs = []string{
		"CIRCLE_BRANCH",
		"CIRCLE_BUILD_NUM",
		"CIRCLE_BUILD_URL",
		"CIRCLE_NODE_INDEX",
		"CIRCLE_NODE_TOTAL",
		"CIRCLE_PR_NUMBER",
		"CIRCLE_PR_USERNAME",
		"CIRCLE_PR_REPONAME",
		"CIRCLE_PROJECT_REPONAME",
		"CIRCLE_PROJECT_USERNAME",
		"CIRCLE_PULL_REQUEST",
		"CIRCLE_PULL_REQUESTS",
		"CIRCLE_REPOSITORY_URL",
		"CIRCLE_SHA1",
		"CIRCLE_TAG",
		"CIRCLE_WORKFLOW_ID",
		"CIRCLE_WORKFLOW_JOB_ID",
		"CIRCLE_WORKFLOW_WORKSPACE_ID",
	}

	for _, env := range circleciEnvs {
		metadata[env] = os.Getenv(env)
	}

	return nil
}

func HandleReport(cmd *cobra.Command, args []string) error {
	apiKey := os.Getenv("FLAKEBOT_REPORTER_KEY")

	err, presignedUrl := GetPresignedUrl(apiKey)
	if err != nil {
		return err
	}
	path := args[0]
	err, archive := CreateArchive(path)
	if err != nil {
		return err
	}
	err = UploadArchive(archive, presignedUrl)
	if err != nil {
		return err
	}

	provider, err := GetProvider()
	if err != nil {
		return err
	}

	report := Report{Archive: presignedUrl.Fields.Key, Provider: provider.Name, Metadata: map[string]interface{}{}}

	err = provider.GetMetadata(report.Metadata)
	if err != nil {
		return err
	}

	err = CreateReport(report, apiKey)
	if err != nil {
		return err
	}

	return nil
}
