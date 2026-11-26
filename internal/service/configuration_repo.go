package service

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"go-dsc-pull/internal/logs"
	"sort"
)

const configurationCompileTimeout = 30 * time.Minute
const configurationGitSyncTimeout = 5 * time.Minute

var configurationRepoSyncMu sync.Mutex

type ConfigurationRepoConfig struct {
	URL                  string
	Branch               string
	Directory            string
	CompilerScript       string
	PowerShellExecutable string
	RepositoryUsername   string
	RepositoryPassword   string
	NodeContexts         []NodeCompilationContext
	// ResolveNodeContexts, si non nul, est appelé pour produire les contextes de
	// compilation **seulement lorsque le dépôt a changé** (c'est-à-dire quand on
	// va réellement compiler). Permet au worker périodique d'éviter de résoudre
	// les propriétés de tous les nœuds à chaque cycle alors que, dans le cas
	// fréquent « dépôt à jour », aucune compilation n'est nécessaire.
	// Prioritaire sur NodeContexts lorsqu'il est défini.
	ResolveNodeContexts func() ([]NodeCompilationContext, error)
}

type NodeCompilationContext struct {
	NodeName string              `json:"node_name"`
	Tags     map[string][]string `json:"tags"`
}

type CompiledConfiguration struct {
	Name string
	Path string
	Data []byte
}

func RunConfigurationRepoSync(config ConfigurationRepoConfig, process func([]CompiledConfiguration) (int, error)) (int, error) {
	return RunConfigurationRepoSyncForNames(config, nil, process)
}

func RunConfigurationRepoSyncForNames(config ConfigurationRepoConfig, names []string, process func([]CompiledConfiguration) (int, error)) (int, error) {
	return RunConfigurationRepoSyncForNode(config, names, "", process)
}

func RunConfigurationRepoSyncForNode(config ConfigurationRepoConfig, names []string, nodeName string, process func([]CompiledConfiguration) (int, error)) (int, error) {
	return RunConfigurationRepoSyncForNodeContext(config, names, nodeName, nil, process)
}

func RunConfigurationRepoSyncForNodeContext(config ConfigurationRepoConfig, names []string, nodeName string, tags map[string][]string, process func([]CompiledConfiguration) (int, error)) (int, error) {
	configurationRepoSyncMu.Lock()
	defer configurationRepoSyncMu.Unlock()
	_ = logs.WriteLogFile("INFO [CONFIGURATION REPO] Sync and compile started")
	compiled, err := SyncAndCompileConfigurationsForNodeContext(config, names, nodeName, tags)
	if err != nil {
		_ = logs.WriteLogFile(fmt.Sprintf("ERROR [CONFIGURATION REPO] Sync or compilation failed: %v", err))
		return 0, err
	}
	if process == nil {
		return 0, fmt.Errorf("configuration importer is required")
	}
	_ = logs.WriteLogFile(fmt.Sprintf("INFO [CONFIGURATION REPO] Importing %d compiled MOF(s)", len(compiled)))
	changed, err := process(compiled)
	if err != nil {
		_ = logs.WriteLogFile(fmt.Sprintf("ERROR [CONFIGURATION REPO] Database import failed: %v", err))
		return 0, err
	}
	_ = logs.WriteLogFile(fmt.Sprintf("INFO [CONFIGURATION REPO] Sync completed; changed configurations=%d", changed))
	return changed, nil
}

// SyncAndCompileConfigurations updates a managed checkout and returns its generated MOFs.
func SyncAndCompileConfigurations(config ConfigurationRepoConfig) ([]CompiledConfiguration, error) {
	return SyncAndCompileConfigurationsForNames(config, nil)
}

func SyncAndCompileConfigurationsForNames(config ConfigurationRepoConfig, names []string) ([]CompiledConfiguration, error) {
	return SyncAndCompileConfigurationsForNode(config, names, "")
}

func SyncAndCompileConfigurationsForNode(config ConfigurationRepoConfig, names []string, nodeName string) ([]CompiledConfiguration, error) {
	return SyncAndCompileConfigurationsForNodeContext(config, names, nodeName, nil)
}

func SyncAndCompileConfigurationsForNodeContext(config ConfigurationRepoConfig, names []string, nodeName string, tags map[string][]string) ([]CompiledConfiguration, error) {
	if strings.TrimSpace(config.URL) == "" || strings.TrimSpace(config.Directory) == "" || strings.TrimSpace(config.CompilerScript) == "" {
		return nil, fmt.Errorf("repository URL, working directory, and compiler script are required")
	}
	if config.PowerShellExecutable == "" {
		config.PowerShellExecutable = "pwsh"
	}

	repoDir := config.Directory
	if !filepath.IsAbs(repoDir) {
		executable, err := os.Executable()
		if err != nil {
			return nil, err
		}
		repoDir = filepath.Join(filepath.Dir(executable), repoDir)
	}
	repoDir, err := filepath.Abs(repoDir)
	if err != nil {
		return nil, err
	}
	_ = logs.WriteLogFile(fmt.Sprintf("INFO [CONFIGURATION REPO] Git sync started; branch=%q directory=%q", config.Branch, repoDir))
	if err := os.MkdirAll(filepath.Dir(repoDir), 0755); err != nil {
		return nil, err
	}
	gitContext, cancelGit := context.WithTimeout(context.Background(), configurationGitSyncTimeout)
	defer cancelGit()
	gitChanged, err := syncGitRepositoryContext(gitContext, repoDir, config.URL, config.Branch, config.RepositoryUsername, config.RepositoryPassword)
	if err != nil {
		return nil, err
	}
	_ = logs.WriteLogFile("INFO [CONFIGURATION REPO] Git sync completed")
	if !gitChanged && nodeName == "" && len(tags) == 0 {
		_ = logs.WriteLogFile("INFO [CONFIGURATION REPO] Git repository is up to date; skipping compilation")
		return []CompiledConfiguration{}, nil
	}
	_ = logs.WriteLogFile("INFO [CONFIGURATION REPO] PowerShell compilation starting")

	script := config.CompilerScript
	if !filepath.IsAbs(script) {
		script = filepath.Join(repoDir, script)
	}
	if _, err := os.Stat(script); err != nil {
		return nil, fmt.Errorf("compiler script unavailable: %w", err)
	}
	outputDir, err := os.MkdirTemp("", "dsc-compiled-mofs-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(outputDir)

	ctx, cancel := context.WithTimeout(context.Background(), configurationCompileTimeout)
	defer cancel()
	args := []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script, "-SourcePath", repoDir, "-OutputPath", outputDir}
	if len(names) > 0 {
		nameJSON, err := json.Marshal(names)
		if err != nil {
			return nil, err
		}
		args = append(args, "-ConfigurationNamesJson", string(nameJSON))
	}
	nodeContexts := append([]NodeCompilationContext(nil), config.NodeContexts...)
	if config.ResolveNodeContexts != nil {
		resolved, err := config.ResolveNodeContexts()
		if err != nil {
			return nil, err
		}
		nodeContexts = append(nodeContexts, resolved...)
	}
	if nodeName != "" || len(tags) > 0 {
		nodeContexts = append(nodeContexts, NodeCompilationContext{NodeName: nodeName, Tags: tags})
	}
	if len(nodeContexts) > 0 {
		contextPath, err := writeNodeCompilationContext(nodeContexts, names)
		if err != nil {
			return nil, err
		}
		defer os.Remove(contextPath)
		args = append(args, "-NodeContextPath", contextPath)
	}
	cmd := exec.CommandContext(ctx, config.PowerShellExecutable, args...)
	cmd.Dir = repoDir
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open PowerShell stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("open PowerShell stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start PowerShell compilation: %w", err)
	}
	var outputMu sync.Mutex
	var output strings.Builder
	var outputReaders sync.WaitGroup
	streamOutput := func(name string, stream io.ReadCloser) {
		defer outputReaders.Done()
		scanner := bufio.NewScanner(stream)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			outputMu.Lock()
			output.WriteString(line)
			output.WriteByte('\n')
			outputMu.Unlock()
			_ = logs.WriteLogFile(fmt.Sprintf("INFO [CONFIGURATION REPO] PowerShell %s: %s", name, line))
		}
		if scanErr := scanner.Err(); scanErr != nil {
			_ = logs.WriteLogFile(fmt.Sprintf("ERROR [CONFIGURATION REPO] Reading PowerShell %s: %v", name, scanErr))
		}
	}
	outputReaders.Add(2)
	go streamOutput("stdout", stdout)
	go streamOutput("stderr", stderr)
	stopHeartbeat := make(chan struct{})
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = logs.WriteLogFile("INFO [CONFIGURATION REPO] PowerShell compilation still running")
			case <-stopHeartbeat:
				return
			}
		}
	}()
	compileStarted := time.Now()
	outputReaders.Wait()
	err = cmd.Wait()
	close(stopHeartbeat)
	<-heartbeatDone
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("PowerShell compilation timed out after %s", configurationCompileTimeout)
		}
		return nil, fmt.Errorf("PowerShell compilation failed: %w: %s", err, strings.TrimSpace(output.String()))
	}
	_ = logs.WriteLogFile(fmt.Sprintf("INFO [CONFIGURATION REPO] PowerShell compilation completed in %s", time.Since(compileStarted).Round(time.Second)))

	compiled := make([]CompiledConfiguration, 0)
	err = filepath.WalkDir(outputDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".mof") {
			return nil
		}
		// #nosec G122 -- walking the freshly-compiled outputDir, created by the
		// server for this compilation and not attacker-controlled.
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if len(data) == 0 {
			return fmt.Errorf("compiled MOF is empty: %s", entry.Name())
		}
		compiled = append(compiled, CompiledConfiguration{
			Name: strings.ToLower(strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))),
			Path: path,
			Data: data,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(compiled) == 0 {
		return nil, fmt.Errorf("compiler produced no MOF files")
	}
	sort.Slice(compiled, func(i, j int) bool { return compiled[i].Name < compiled[j].Name })
	for i := 1; i < len(compiled); i++ {
		if compiled[i-1].Name == compiled[i].Name {
			return nil, fmt.Errorf("multiple MOF files map to configuration name %q", compiled[i].Name)
		}
	}
	return compiled, nil
}

func writeNodeCompilationContext(nodes []NodeCompilationContext, names []string) (string, error) {
	contextData := struct {
		NodeName           string                   `json:"node_name,omitempty"`
		ConfigurationNames []string                 `json:"configuration_names"`
		Tags               map[string][]string      `json:"tags,omitempty"`
		Nodes              []NodeCompilationContext `json:"nodes,omitempty"`
	}{ConfigurationNames: names, Nodes: nodes}
	if len(nodes) == 1 {
		contextData.NodeName = nodes[0].NodeName
		contextData.Tags = nodes[0].Tags
		contextData.Nodes = nil
	}
	data, err := json.Marshal(contextData)
	if err != nil {
		return "", err
	}
	file, err := os.CreateTemp("", "dsc-node-context-*.json")
	if err != nil {
		return "", err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		os.Remove(file.Name())
		return "", err
	}
	if err := file.Close(); err != nil {
		os.Remove(file.Name())
		return "", err
	}
	return file.Name(), nil
}

func SyncGitRepository(repoDir, repositoryURL, branch string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), configurationGitSyncTimeout)
	defer cancel()
	return syncGitRepositoryContext(ctx, repoDir, repositoryURL, branch, "", "")
}

// gitAuthMethod retourne l'authentification HTTP à utiliser pour go-git. Si des
// credentials explicites sont fournies, elles priment sur tout jeton embarqué
// dans l'URL. Sinon on retourne nil (go-git se rabattra sur l'userinfo de l'URL).
func gitAuthMethod(username, password string) transport.AuthMethod {
	if username == "" && password == "" {
		return nil
	}
	return &http.BasicAuth{Username: username, Password: password}
}

func syncGitRepositoryContext(ctx context.Context, repoDir, repositoryURL, branch, username, password string) (bool, error) {
	if _, err := os.Stat(filepath.Join(repoDir, ".git")); os.IsNotExist(err) {
		if _, dirErr := os.Stat(repoDir); dirErr == nil {
			entries, readErr := os.ReadDir(repoDir)
			if readErr != nil {
				return false, readErr
			}
			if len(entries) != 0 {
				return false, fmt.Errorf("repository directory is not empty and is not a git checkout")
			}
		} else if !os.IsNotExist(dirErr) {
			return false, fmt.Errorf("inspect repository directory: %w", dirErr)
		}
		cloneOptions := &git.CloneOptions{URL: repositoryURL, Progress: nil, Auth: gitAuthMethod(username, password)}
		if branch != "" {
			cloneOptions.ReferenceName = plumbing.NewBranchReferenceName(branch)
			cloneOptions.SingleBranch = true
		}
		if _, err := git.PlainCloneContext(ctx, repoDir, false, cloneOptions); err != nil {
			return false, fmt.Errorf("git clone failed: %w", err)
		}
		return true, nil
	} else if err != nil {
		return false, fmt.Errorf("inspect repository directory: %w", err)
	}

	repository, err := git.PlainOpen(repoDir)
	if err != nil {
		return false, fmt.Errorf("open git checkout: %w", err)
	}
	remote, err := repository.Remote("origin")
	if err != nil {
		return false, fmt.Errorf("open git origin: %w", err)
	}
	if len(remote.Config().URLs) == 0 || remote.Config().URLs[0] != repositoryURL {
		return false, fmt.Errorf("repository origin does not match configured URL")
	}

	if branch == "" {
		head, err := repository.Head()
		if err != nil {
			return false, fmt.Errorf("resolve checked-out branch: %w", err)
		}
		if !head.Name().IsBranch() {
			return false, fmt.Errorf("repository is in detached HEAD state and no branch is configured")
		}
		branch = head.Name().Short()
	}

	branchRef := plumbing.NewBranchReferenceName(branch)
	worktree, err := repository.Worktree()
	if err != nil {
		return false, err
	}
	if err := worktree.Checkout(&git.CheckoutOptions{Branch: branchRef, Keep: true}); err != nil {
		return false, fmt.Errorf("checkout branch %q: %w", branch, err)
	}
	if err := remote.FetchContext(ctx, &git.FetchOptions{RefSpecs: []config.RefSpec{config.RefSpec("+refs/heads/" + branch + ":refs/remotes/origin/" + branch)}, Prune: true, Auth: gitAuthMethod(username, password)}); err != nil && err != git.NoErrAlreadyUpToDate && err != transport.ErrEmptyRemoteRepository {
		return false, fmt.Errorf("fetch branch %q: %w", branch, err)
	}
	remoteRef, err := repository.Reference(plumbing.NewRemoteReferenceName("origin", branch), true)
	if err != nil {
		return false, fmt.Errorf("resolve remote branch %q: %w", branch, err)
	}
	head, err := repository.Head()
	if err != nil {
		return false, fmt.Errorf("resolve local HEAD: %w", err)
	}
	if head.Hash() == remoteRef.Hash() {
		return false, nil
	}
	if err := worktree.PullContext(ctx, &git.PullOptions{RemoteName: "origin", ReferenceName: branchRef, SingleBranch: true, Force: false, Auth: gitAuthMethod(username, password)}); err != nil && err != git.NoErrAlreadyUpToDate {
		return false, fmt.Errorf("fast-forward branch %q: %w", branch, err)
	}
	return true, nil
}
