package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type upstreamWorktreeError struct {
	code    string
	message string
}

func (e upstreamWorktreeError) Error() string {
	return e.message
}

func upstreamWorktreeFailed(err error) map[string]any {
	if typed, ok := err.(upstreamWorktreeError); ok {
		return map[string]any{"status": "failed", "code": typed.code, "message": typed.message}
	}
	return map[string]any{"status": "failed", "code": "unknown", "message": err.Error()}
}

func upstreamWorktreeStatusValue() map[string]any {
	available := gitAvailable()
	status := "ok"
	if !available {
		status = "failed"
	}
	return map[string]any{
		"status":            status,
		"feature":           "upstream-worktree",
		"gitAvailable":      available,
		"platformSupported": true,
	}
}

func upstreamWorktreeDefaultsValue(payload map[string]any) map[string]any {
	if projectID := strings.TrimSpace(stringFromAny(payload["projectId"])); projectID != "" {
		if project, ok := remoteProjectForID(projectID); ok {
			result, err := remoteDefaultsForProject(project)
			if err != nil {
				return upstreamWorktreeFailed(err)
			}
			return result
		}
	}
	repoPath := strings.TrimSpace(stringFromAny(payload["repoPath"]))
	if repoPath == "" {
		return upstreamWorktreeFailed(upstreamWorktreeError{"not-git-repo", "Repository path is required"})
	}
	result, err := upstreamDefaultsForRepo(repoPath)
	if err != nil {
		return upstreamWorktreeFailed(err)
	}
	return result
}

func upstreamWorktreePrepareValue(payload map[string]any) map[string]any {
	req, err := upstreamSourceRequestFromPayload(payload)
	if err != nil {
		return upstreamWorktreeFailed(err)
	}
	if req.projectID != "" {
		if project, ok := remoteProjectForID(req.projectID); ok {
			result, err := remotePrepareSourceRef(project, req)
			if err != nil {
				return upstreamWorktreeFailed(err)
			}
			return result
		}
	}
	result, err := prepareSourceRef(req)
	if err != nil {
		return upstreamWorktreeFailed(err)
	}
	return result
}

func upstreamWorktreeCreateValue(payload map[string]any) map[string]any {
	req, err := upstreamRequestFromPayload(payload)
	if err != nil {
		return upstreamWorktreeFailed(err)
	}
	if req.projectID != "" {
		if project, ok := remoteProjectForID(req.projectID); ok {
			result, err := remoteCreateWorktree(project, req)
			if err != nil {
				return upstreamWorktreeFailed(err)
			}
			return result
		}
	}
	result, err := createWorktree(req)
	if err != nil {
		return upstreamWorktreeFailed(err)
	}
	return result
}

type upstreamWorktreeRequest struct {
	repoPath     string
	projectID    string
	branchName   string
	worktreePath string
	remote       string
	baseBranch   string
	fetch        bool
}

type upstreamSourceRequest struct {
	repoPath   string
	projectID  string
	remote     string
	baseBranch string
	fetch      bool
}

func upstreamRequestFromPayload(payload map[string]any) (upstreamWorktreeRequest, error) {
	req := upstreamWorktreeRequest{
		repoPath:     strings.TrimSpace(stringFromAny(payload["repoPath"])),
		projectID:    strings.TrimSpace(stringFromAny(payload["projectId"])),
		branchName:   strings.TrimSpace(stringFromAny(payload["branchName"])),
		worktreePath: strings.TrimSpace(stringFromAny(payload["worktreePath"])),
		remote:       strings.TrimSpace(stringFromAny(payload["remote"])),
		baseBranch:   strings.TrimSpace(stringFromAny(payload["baseBranch"])),
		fetch:        true,
	}
	if value, ok := payload["fetch"]; ok {
		req.fetch = boolFromAny(value)
	}
	if req.repoPath == "" && req.projectID == "" {
		return req, upstreamWorktreeError{"not-git-repo", "Repository path is required"}
	}
	if req.worktreePath == "" {
		return req, upstreamWorktreeError{"path-exists", "Worktree path is required"}
	}
	if err := validateBranchName(req.branchName); err != nil {
		return req, err
	}
	if err := validateBaseBranch(req.baseBranch); err != nil {
		return req, err
	}
	if err := validateRemoteName(req.remote); err != nil {
		return req, err
	}
	return req, nil
}

func upstreamSourceRequestFromPayload(payload map[string]any) (upstreamSourceRequest, error) {
	req := upstreamSourceRequest{
		repoPath:   strings.TrimSpace(stringFromAny(payload["repoPath"])),
		projectID:  strings.TrimSpace(stringFromAny(payload["projectId"])),
		remote:     strings.TrimSpace(stringFromAny(payload["remote"])),
		baseBranch: strings.TrimSpace(stringFromAny(payload["baseBranch"])),
		fetch:      true,
	}
	if value, ok := payload["fetch"]; ok {
		req.fetch = boolFromAny(value)
	}
	if req.repoPath == "" && req.projectID == "" {
		return req, upstreamWorktreeError{"not-git-repo", "Repository path is required"}
	}
	if err := validateBaseBranch(req.baseBranch); err != nil {
		return req, err
	}
	if err := validateRemoteName(req.remote); err != nil {
		return req, err
	}
	return req, nil
}

func validateBranchName(branch string) error {
	branch = strings.TrimSpace(branch)
	if branch == "" || strings.HasPrefix(branch, "-") || strings.Contains(branch, `\`) {
		return upstreamWorktreeError{"branch-invalid", "Invalid branch name: " + branch}
	}
	output, err := gitOutput("", "check-ref-format", "--branch", branch)
	if err != nil {
		return upstreamWorktreeError{"git-missing", "Git is not available"}
	}
	if !output.ok {
		return upstreamWorktreeError{"branch-invalid", "Invalid branch name: " + branch}
	}
	return nil
}

func validateBaseBranch(baseBranch string) error {
	if err := validateBranchName(baseBranch); err != nil {
		if typed, ok := err.(upstreamWorktreeError); ok && typed.code == "git-missing" {
			return err
		}
		return upstreamWorktreeError{"base-branch-missing", "Invalid base branch: " + baseBranch}
	}
	return nil
}

func validateRemoteName(remote string) error {
	remote = strings.TrimSpace(remote)
	if remote == "" || strings.HasPrefix(remote, "-") || strings.ContainsAny(remote, `/\`) {
		return upstreamWorktreeError{"remote-missing", "Remote is required"}
	}
	return nil
}

type gitCommandOutput struct {
	ok     bool
	stdout string
	stderr string
}

func gitAvailable() bool {
	output, err := gitOutput("", "--version")
	return err == nil && output.ok
}

func gitOutput(repo string, args ...string) (gitCommandOutput, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var commandArgs []string
	if strings.TrimSpace(repo) != "" {
		commandArgs = append(commandArgs, "-C", repo)
	}
	commandArgs = append(commandArgs, args...)
	cmd := exec.CommandContext(ctx, "git", commandArgs...)
	hideSubprocessWindow(cmd)
	output, err := cmd.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return gitCommandOutput{ok: false, stdout: strings.TrimSpace(string(output)), stderr: strings.TrimSpace(string(exit.Stderr))}, nil
		}
		return gitCommandOutput{}, err
	}
	return gitCommandOutput{ok: true, stdout: strings.TrimSpace(string(output)), stderr: ""}, nil
}

func repoRootForPath(repoPath string) (string, error) {
	output, err := gitOutput(repoPath, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", upstreamWorktreeError{"git-missing", "Git is not available"}
	}
	if !output.ok || output.stdout == "" {
		return "", upstreamWorktreeError{"not-git-repo", "Path is not inside a Git repository"}
	}
	return output.stdout, nil
}

func currentGitBranch(repoRoot string) string {
	output, err := gitOutput(repoRoot, "branch", "--show-current")
	if err != nil || !output.ok {
		return ""
	}
	return strings.TrimSpace(output.stdout)
}

func gitRemoteNames(repoRoot string) ([]string, error) {
	output, err := gitOutput(repoRoot, "remote")
	if err != nil {
		return nil, upstreamWorktreeError{"git-missing", "Git is not available"}
	}
	if !output.ok {
		return nil, upstreamWorktreeError{"remote-missing", firstNonEmpty(output.stderr, "Cannot read Git remotes")}
	}
	return nonEmptyLines(output.stdout), nil
}

func defaultRemoteName(remotes []string) string {
	for _, remote := range remotes {
		if remote == "upstream" {
			return "upstream"
		}
	}
	for _, remote := range remotes {
		if remote == "origin" {
			return "origin"
		}
	}
	if len(remotes) > 0 {
		return remotes[0]
	}
	return "upstream"
}

func upstreamDefaultsForRepo(repoPath string) (map[string]any, error) {
	root, err := repoRootForPath(repoPath)
	if err != nil {
		return nil, err
	}
	branch := currentGitBranch(root)
	defaultBaseBranch := branch
	if defaultBaseBranch == "" {
		defaultBaseBranch = "main"
	}
	remotes, err := gitRemoteNames(root)
	if err != nil {
		return nil, err
	}
	defaultRemote := defaultRemoteName(remotes)
	return map[string]any{
		"status":            "ok",
		"repoRoot":          root,
		"currentBranch":     branch,
		"defaultBaseBranch": defaultBaseBranch,
		"remotes":           remotes,
		"defaultRemote":     defaultRemote,
		"upstreamRefs":      upstreamRefs(root, defaultRemote, defaultBaseBranch),
		"worktreeBranches":  worktreeBranches(root),
	}, nil
}

func upstreamRefs(repoRoot, remote, fallbackBranch string) []map[string]any {
	output, err := gitOutput(repoRoot, "for-each-ref", "--format=%(refname)", "refs/remotes/"+remote)
	if err != nil || !output.ok {
		return fallbackUpstreamRefs(remote, fallbackBranch)
	}
	return refsFromOutput(output.stdout, remote, fallbackBranch)
}

func refsFromOutput(output, remote, fallbackBranch string) []map[string]any {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return []map[string]any{}
	}
	prefix := "refs/remotes/" + remote + "/"
	var refs []map[string]any
	for _, line := range strings.Split(output, "\n") {
		refName := strings.TrimSpace(line)
		if !strings.HasPrefix(refName, prefix) {
			continue
		}
		branch := strings.TrimPrefix(refName, prefix)
		if branch == "" || branch == "HEAD" {
			continue
		}
		refs = append(refs, map[string]any{
			"remote":    remote,
			"branch":    branch,
			"label":     remote + "/" + branch,
			"sourceRef": "refs/remotes/" + remote + "/" + branch,
		})
	}
	sort.Slice(refs, func(i, j int) bool { return stringFromAny(refs[i]["label"]) < stringFromAny(refs[j]["label"]) })
	if len(refs) == 0 {
		return fallbackUpstreamRefs(remote, fallbackBranch)
	}
	return refs
}

func fallbackUpstreamRefs(remote, baseBranch string) []map[string]any {
	remote = strings.TrimSpace(remote)
	baseBranch = strings.TrimSpace(baseBranch)
	if remote == "" || baseBranch == "" {
		return []map[string]any{}
	}
	return []map[string]any{{
		"remote":    remote,
		"branch":    baseBranch,
		"label":     remote + "/" + baseBranch,
		"sourceRef": "refs/remotes/" + remote + "/" + baseBranch,
	}}
}

func worktreeBranches(repoRoot string) []map[string]any {
	output, err := gitOutput(repoRoot, "worktree", "list", "--porcelain")
	if err != nil || !output.ok {
		return []map[string]any{}
	}
	return worktreeBranchesFromOutput(output.stdout)
}

func worktreeBranchesFromOutput(output string) []map[string]any {
	var branches []map[string]any
	worktreePath := ""
	branchName := ""
	for _, line := range append(strings.Split(output, "\n"), "") {
		line = strings.TrimSpace(line)
		if line == "" {
			if worktreePath != "" && branchName != "" {
				path := worktreePath
				if abs, err := filepath.Abs(path); err == nil {
					path = abs
				}
				branches = append(branches, map[string]any{"path": path, "branch": branchName})
			}
			worktreePath = ""
			branchName = ""
			continue
		}
		if value, ok := strings.CutPrefix(line, "worktree "); ok {
			worktreePath = value
		} else if value, ok := strings.CutPrefix(line, "branch refs/heads/"); ok {
			branchName = value
		}
	}
	return branches
}

func ensureRemoteExists(remotes []string, remote string) error {
	for _, candidate := range remotes {
		if candidate == remote {
			return nil
		}
	}
	return upstreamWorktreeError{"remote-missing", "Remote does not exist: " + remote}
}

func ensureBranchAvailable(repoRoot, branchName string) error {
	output, err := gitOutput(repoRoot, "show-ref", "--verify", "--quiet", "refs/heads/"+branchName)
	if err != nil {
		return upstreamWorktreeError{"git-missing", "Git is not available"}
	}
	if output.ok {
		return upstreamWorktreeError{"branch-exists", "Branch already exists: " + branchName}
	}
	return nil
}

func fetchRemoteBranch(repoRoot, remote, baseBranch string) error {
	refspec := "+refs/heads/" + baseBranch + ":refs/remotes/" + remote + "/" + baseBranch
	output, err := gitOutput(repoRoot, "fetch", remote, refspec)
	if err != nil {
		return upstreamWorktreeError{"git-missing", "Git is not available"}
	}
	if !output.ok {
		return upstreamWorktreeError{"fetch-failed", firstNonEmpty(output.stderr, "Failed to fetch "+remote+"/"+baseBranch)}
	}
	return nil
}

func ensureSourceRefExists(repoRoot, qualifiedRef string) (string, error) {
	output, err := gitOutput(repoRoot, "rev-parse", "--verify", qualifiedRef+"^{commit}")
	if err != nil {
		return "", upstreamWorktreeError{"git-missing", "Git is not available"}
	}
	if output.ok && output.stdout != "" {
		return output.stdout, nil
	}
	return "", upstreamWorktreeError{"base-branch-missing", "Base branch does not exist: " + qualifiedRef}
}

func sourceRef(remote, baseBranch string) string {
	return strings.TrimSpace(remote) + "/" + strings.TrimSpace(baseBranch)
}

func qualifiedRemoteRef(remote, baseBranch string) string {
	return "refs/remotes/" + strings.TrimSpace(remote) + "/" + strings.TrimSpace(baseBranch)
}

func prepareSourceRef(req upstreamSourceRequest) (map[string]any, error) {
	root, err := repoRootForPath(req.repoPath)
	if err != nil {
		return nil, err
	}
	remotes, err := gitRemoteNames(root)
	if err != nil {
		return nil, err
	}
	if err := ensureRemoteExists(remotes, req.remote); err != nil {
		return nil, err
	}
	if req.fetch {
		if err := fetchRemoteBranch(root, req.remote, req.baseBranch); err != nil {
			return nil, err
		}
	}
	displayRef := sourceRef(req.remote, req.baseBranch)
	qualifiedRef := qualifiedRemoteRef(req.remote, req.baseBranch)
	head, err := ensureSourceRefExists(root, qualifiedRef)
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": "ok", "repoRoot": root, "sourceRef": displayRef, "qualifiedSourceRef": qualifiedRef, "sourceHead": head}, nil
}

func createWorktree(req upstreamWorktreeRequest) (map[string]any, error) {
	root, err := repoRootForPath(req.repoPath)
	if err != nil {
		return nil, err
	}
	remotes, err := gitRemoteNames(root)
	if err != nil {
		return nil, err
	}
	if err := ensureRemoteExists(remotes, req.remote); err != nil {
		return nil, err
	}
	if err := ensureBranchAvailable(root, req.branchName); err != nil {
		return nil, err
	}
	worktreePath := req.worktreePath
	if !filepath.IsAbs(worktreePath) {
		worktreePath = filepath.Join(root, worktreePath)
	}
	if fileExists(worktreePath) {
		return nil, upstreamWorktreeError{"path-exists", "Worktree path already exists: " + worktreePath}
	}
	if req.fetch {
		if err := fetchRemoteBranch(root, req.remote, req.baseBranch); err != nil {
			return nil, err
		}
	}
	displayRef := sourceRef(req.remote, req.baseBranch)
	qualifiedRef := qualifiedRemoteRef(req.remote, req.baseBranch)
	head, err := ensureSourceRefExists(root, qualifiedRef)
	if err != nil {
		return nil, err
	}
	output, err := gitOutput(root, "worktree", "add", "-b", req.branchName, worktreePath, qualifiedRef)
	if err != nil {
		return nil, upstreamWorktreeError{"git-missing", "Git is not available"}
	}
	if !output.ok {
		return nil, upstreamWorktreeError{"worktree-create-failed", firstNonEmpty(output.stderr, "Failed to create worktree")}
	}
	return map[string]any{"status": "ok", "repoRoot": root, "worktreePath": worktreePath, "branchName": req.branchName, "sourceRef": displayRef, "sourceHead": head}, nil
}

type upstreamRemoteProject struct {
	projectID  string
	hostID     string
	remotePath string
	label      string
}

func remoteProjectForID(projectID string) (upstreamRemoteProject, bool) {
	data, err := os.ReadFile(codexGlobalStatePath(codexHomeDir()))
	if err != nil {
		return upstreamRemoteProject{}, false
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		return upstreamRemoteProject{}, false
	}
	projects, _ := state["remote-projects"].([]any)
	for _, item := range projects {
		project, _ := item.(map[string]any)
		if project == nil || stringFromAny(project["id"]) != projectID {
			continue
		}
		hostID := stringFromAny(project["hostId"])
		remotePath := stringFromAny(project["remotePath"])
		if hostID == "" || !strings.HasPrefix(remotePath, "/") {
			return upstreamRemoteProject{}, false
		}
		return upstreamRemoteProject{projectID: projectID, hostID: hostID, remotePath: remotePath, label: stringFromAny(project["label"])}, true
	}
	return upstreamRemoteProject{}, false
}

func remoteDefaultsForProject(project upstreamRemoteProject) (map[string]any, error) {
	output, err := remoteShell(project, remoteDefaultsSnapshotScript(project.remotePath))
	if err != nil {
		return nil, err
	}
	if !output.ok {
		return nil, upstreamWorktreeError{"not-git-repo", firstNonEmpty(output.stderr, "Remote path is not inside a Git repository")}
	}
	snapshot := parseRemoteDefaultsSnapshot(output.stdout)
	if snapshot.root == "" {
		return nil, upstreamWorktreeError{"not-git-repo", "Remote path is not inside a Git repository"}
	}
	defaultBaseBranch := snapshot.branch
	if defaultBaseBranch == "" {
		defaultBaseBranch = "main"
	}
	defaultRemote := defaultRemoteName(snapshot.remotes)
	return map[string]any{
		"status":            "ok",
		"remoteProject":     true,
		"projectId":         project.projectID,
		"hostId":            project.hostID,
		"remotePath":        project.remotePath,
		"repoRoot":          snapshot.root,
		"currentBranch":     snapshot.branch,
		"defaultBaseBranch": defaultBaseBranch,
		"remotes":           snapshot.remotes,
		"defaultRemote":     defaultRemote,
		"upstreamRefs":      refsFromOutput(snapshot.refsOutput, defaultRemote, defaultBaseBranch),
		"worktreeBranches":  worktreeBranchesFromOutput(snapshot.worktreesOutput),
	}, nil
}

type remoteDefaultsSnapshot struct {
	root            string
	branch          string
	remotes         []string
	refsOutput      string
	worktreesOutput string
}

func remoteDefaultsSnapshotScript(remotePath string) string {
	quoted := shellQuote(remotePath)
	return strings.Join([]string{
		"set -e",
		"cd " + quoted,
		"printf '__ROOT__\\n'",
		"git rev-parse --show-toplevel",
		"printf '__BRANCH__\\n'",
		"git branch --show-current || true",
		"printf '__REMOTES__\\n'",
		"git remote",
		"printf '__REFS__\\n'",
		"git for-each-ref '--format=%(refname)' refs/remotes",
		"printf '__WORKTREES__\\n'",
		"git worktree list --porcelain",
	}, "\n")
}

func parseRemoteDefaultsSnapshot(output string) remoteDefaultsSnapshot {
	var snapshot remoteDefaultsSnapshot
	section := ""
	for _, line := range strings.Split(output, "\n") {
		switch line {
		case "__ROOT__":
			section = "root"
			continue
		case "__BRANCH__":
			section = "branch"
			continue
		case "__REMOTES__":
			section = "remotes"
			continue
		case "__REFS__":
			section = "refs"
			continue
		case "__WORKTREES__":
			section = "worktrees"
			continue
		}
		switch section {
		case "root":
			if snapshot.root == "" {
				snapshot.root = strings.TrimSpace(line)
			}
		case "branch":
			if snapshot.branch == "" {
				snapshot.branch = strings.TrimSpace(line)
			}
		case "remotes":
			if strings.TrimSpace(line) != "" {
				snapshot.remotes = append(snapshot.remotes, strings.TrimSpace(line))
			}
		case "refs":
			snapshot.refsOutput += line + "\n"
		case "worktrees":
			snapshot.worktreesOutput += line + "\n"
		}
	}
	return snapshot
}

func remotePrepareSourceRef(project upstreamRemoteProject, req upstreamSourceRequest) (map[string]any, error) {
	root, err := remoteRepoRoot(project)
	if err != nil {
		return nil, err
	}
	remotes, err := remoteNames(project)
	if err != nil {
		return nil, err
	}
	if err := ensureRemoteExists(remotes, req.remote); err != nil {
		return nil, err
	}
	if req.fetch {
		if err := remoteFetchBranch(project, req.remote, req.baseBranch); err != nil {
			return nil, err
		}
	}
	displayRef := sourceRef(req.remote, req.baseBranch)
	qualifiedRef := qualifiedRemoteRef(req.remote, req.baseBranch)
	head, err := remoteEnsureSourceRef(project, qualifiedRef)
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": "ok", "remoteProject": true, "projectId": project.projectID, "hostId": project.hostID, "repoRoot": root, "sourceRef": displayRef, "qualifiedSourceRef": qualifiedRef, "sourceHead": head}, nil
}

func remoteCreateWorktree(project upstreamRemoteProject, req upstreamWorktreeRequest) (map[string]any, error) {
	root, err := remoteRepoRoot(project)
	if err != nil {
		return nil, err
	}
	remotes, err := remoteNames(project)
	if err != nil {
		return nil, err
	}
	if err := ensureRemoteExists(remotes, req.remote); err != nil {
		return nil, err
	}
	if err := remoteEnsureBranchAvailable(project, req.branchName); err != nil {
		return nil, err
	}
	worktreePath := req.worktreePath
	if !strings.HasPrefix(worktreePath, "/") {
		worktreePath = strings.TrimRight(root, "/") + "/" + strings.TrimPrefix(strings.TrimPrefix(worktreePath, "./"), "/")
	}
	if req.fetch {
		if err := remoteFetchBranch(project, req.remote, req.baseBranch); err != nil {
			return nil, err
		}
	}
	displayRef := sourceRef(req.remote, req.baseBranch)
	qualifiedRef := qualifiedRemoteRef(req.remote, req.baseBranch)
	head, err := remoteEnsureSourceRef(project, qualifiedRef)
	if err != nil {
		return nil, err
	}
	output, err := remoteGit(project, "worktree", "add", "-b", req.branchName, worktreePath, qualifiedRef)
	if err != nil {
		return nil, err
	}
	if !output.ok {
		return nil, upstreamWorktreeError{"worktree-create-failed", firstNonEmpty(output.stderr, "Failed to create remote worktree")}
	}
	return map[string]any{"status": "ok", "remoteProject": true, "projectId": project.projectID, "hostId": project.hostID, "repoRoot": root, "worktreePath": worktreePath, "branchName": req.branchName, "sourceRef": displayRef, "sourceHead": head}, nil
}

func remoteRepoRoot(project upstreamRemoteProject) (string, error) {
	output, err := remoteGit(project, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	if output.ok && output.stdout != "" {
		return output.stdout, nil
	}
	return "", upstreamWorktreeError{"not-git-repo", firstNonEmpty(output.stderr, "Remote path is not inside a Git repository")}
}

func remoteNames(project upstreamRemoteProject) ([]string, error) {
	output, err := remoteGit(project, "remote")
	if err != nil {
		return nil, err
	}
	if !output.ok {
		return nil, upstreamWorktreeError{"remote-missing", firstNonEmpty(output.stderr, "Cannot read remote Git remotes")}
	}
	return nonEmptyLines(output.stdout), nil
}

func remoteEnsureBranchAvailable(project upstreamRemoteProject, branchName string) error {
	output, err := remoteGit(project, "show-ref", "--verify", "--quiet", "refs/heads/"+branchName)
	if err != nil {
		return err
	}
	if output.ok {
		return upstreamWorktreeError{"branch-exists", "Branch already exists: " + branchName}
	}
	return nil
}

func remoteFetchBranch(project upstreamRemoteProject, remote, baseBranch string) error {
	refspec := "+refs/heads/" + baseBranch + ":refs/remotes/" + remote + "/" + baseBranch
	output, err := remoteGit(project, "fetch", remote, refspec)
	if err != nil {
		return err
	}
	if !output.ok {
		return upstreamWorktreeError{"fetch-failed", firstNonEmpty(output.stderr, "Failed to fetch "+remote+"/"+baseBranch)}
	}
	return nil
}

func remoteEnsureSourceRef(project upstreamRemoteProject, qualifiedRef string) (string, error) {
	output, err := remoteGit(project, "rev-parse", "--verify", qualifiedRef+"^{commit}")
	if err != nil {
		return "", err
	}
	if output.ok && output.stdout != "" {
		return output.stdout, nil
	}
	return "", upstreamWorktreeError{"base-branch-missing", "Base branch does not exist: " + qualifiedRef}
}

func remoteGit(project upstreamRemoteProject, args ...string) (gitCommandOutput, error) {
	target, err := resolveSSHTargetForHostID(project.hostID)
	if err != nil {
		return gitCommandOutput{}, upstreamWorktreeError{"git-missing", err.Error()}
	}
	command := append([]string{"git", "-C", project.remotePath}, args...)
	return runRemoteShell(target, strings.Join(quoteShellArgs(command), " "))
}

func remoteShell(project upstreamRemoteProject, script string) (gitCommandOutput, error) {
	target, err := resolveSSHTargetForHostID(project.hostID)
	if err != nil {
		return gitCommandOutput{}, upstreamWorktreeError{"git-missing", err.Error()}
	}
	return runRemoteShell(target, script)
}

func runRemoteShell(target sshTarget, remoteCommand string) (gitCommandOutput, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	destination := target.Host
	if strings.TrimSpace(target.User) != "" {
		destination = strings.TrimSpace(target.User) + "@" + target.Host
	}
	args := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=8"}
	if target.Port != nil {
		args = append(args, "-p", fmt.Sprint(*target.Port))
	}
	args = append(args, destination, remoteCommand)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	hideSubprocessWindow(cmd)
	output, err := cmd.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return gitCommandOutput{ok: false, stdout: strings.TrimSpace(string(output)), stderr: strings.TrimSpace(string(exit.Stderr))}, nil
		}
		return gitCommandOutput{}, upstreamWorktreeError{"git-missing", "Cannot run remote git over SSH: " + err.Error()}
	}
	return gitCommandOutput{ok: true, stdout: strings.TrimSpace(string(output)), stderr: ""}, nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func quoteShellArgs(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, shellQuote(value))
	}
	return out
}

func nonEmptyLines(value string) []string {
	var out []string
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
