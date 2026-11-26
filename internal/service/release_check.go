package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type githubLatestRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

type ReleaseCheckResult struct {
	CurrentVersion   string
	LatestRelease    string
	LatestReleaseURL string
	UpdateAvailable  bool
}

func normalizeVersionTag(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return ""
	}
	if version[0] == 'v' || version[0] == 'V' {
		return version[1:]
	}
	return version
}

func versionsEqualIgnoringVPrefix(a, b string) bool {
	return normalizeVersionTag(a) == normalizeVersionTag(b)
}

func CheckLatestRelease(currentVersion string) (ReleaseCheckResult, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/openinfraops/powershell-dsc-pullserver/releases/latest", nil)
	if err != nil {
		return ReleaseCheckResult{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "go-dsc-pull-release-check")

	resp, err := client.Do(req)
	if err != nil {
		return ReleaseCheckResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ReleaseCheckResult{}, fmt.Errorf("github api returned status %d", resp.StatusCode)
	}

	var rel githubLatestRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return ReleaseCheckResult{}, err
	}

	result := ReleaseCheckResult{
		CurrentVersion:   currentVersion,
		LatestRelease:    rel.TagName,
		LatestReleaseURL: rel.HTMLURL,
		UpdateAvailable:  rel.TagName != "" && !versionsEqualIgnoringVPrefix(rel.TagName, currentVersion),
	}
	return result, nil
}
