package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/sethvargo/go-envconfig"
)

type config struct {
	GitHubToken   string `env:"GH_TOKEN,required"`
	MelangeConfig string `env:"MELANGE_CONFIG,default=melange.yaml"`
	GitHubOutput  string `env:"GITHUB_OUTPUT"`
}

func main() {
	ctx := context.Background()

	var cfg config
	envconfig.MustProcess(ctx, &cfg)

	data, err := os.ReadFile(cfg.MelangeConfig)
	if err != nil {
		slog.Error("reading melange config", "error", err)
		os.Exit(1)
	}

	versionRe := regexp.MustCompile(`version: "([0-9.]+)"`)
	m := versionRe.FindSubmatch(data)
	if m == nil {
		slog.Error("version field not found in melange config")
		os.Exit(1)
	}
	currentVersion := string(m[1])

	latestVersion, err := fetchLatestVersion(ctx, cfg.GitHubToken)
	if err != nil {
		slog.Error("fetching latest version", "error", err)
		os.Exit(1)
	}

	slog.Info("version check", "current", currentVersion, "latest", latestVersion)

	setOutput(cfg.GitHubOutput, "current", currentVersion)
	setOutput(cfg.GitHubOutput, "latest", latestVersion)

	if currentVersion == latestVersion {
		slog.Info("already up to date")
		setOutput(cfg.GitHubOutput, "updated", "false")
		return
	}

	sha, err := fetchTagCommit(ctx, cfg.GitHubToken, "v"+latestVersion)
	if err != nil {
		slog.Error("fetching tag commit", "error", err)
		os.Exit(1)
	}

	updated := versionRe.ReplaceAll(data, []byte(`version: "`+latestVersion+`"`))

	commitRe := regexp.MustCompile(`expected-commit: [a-f0-9]+`)
	updated = commitRe.ReplaceAll(updated, []byte("expected-commit: "+sha))

	if err := os.WriteFile(cfg.MelangeConfig, updated, 0644); err != nil {
		slog.Error("writing melange config", "error", err)
		os.Exit(1)
	}

	slog.Info("updated melange config", "version", latestVersion, "sha", sha)
	setOutput(cfg.GitHubOutput, "updated", "true")
}

func fetchLatestVersion(ctx context.Context, token string) (string, error) {
	var r struct {
		TagName string `json:"tag_name"`
	}
	if err := githubGet(ctx, token, "https://api.github.com/repos/liujed/dns01proxy/releases/latest", &r); err != nil {
		return "", err
	}
	return strings.TrimPrefix(r.TagName, "v"), nil
}

func fetchTagCommit(ctx context.Context, token, tag string) (string, error) {
	var c struct {
		SHA string `json:"sha"`
	}
	if err := githubGet(ctx, token, "https://api.github.com/repos/liujed/dns01proxy/commits/"+tag, &c); err != nil {
		return "", err
	}
	return c.SHA, nil
}

func githubGet(ctx context.Context, token, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub API returned %s for %s", resp.Status, url)
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

func setOutput(path, key, value string) {
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		slog.Warn("opening GITHUB_OUTPUT", "error", err)
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s=%s\n", key, value)
}
