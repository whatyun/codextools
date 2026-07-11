package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const conversationHistoryRepairBackupKind = "conversation-history-repair"

const conversationHistoryRepairMinDiskMargin = 256 * 1024 * 1024

var detectConversationHistoryActiveProcesses = defaultConversationHistoryActiveProcesses
var detectConversationHistoryDirectProcesses = defaultConversationHistoryDirectProcesses
var conversationHistoryAvailableDiskBytes = availableConversationHistoryDiskBytes
var acquireConversationHistoryLauncherGuard = defaultConversationHistoryLauncherGuard

var errConversationHistoryWriterStateUnsafe = errors.New("无法确认对话历史处于停止写入状态")
var errConversationHistoryConcurrentMutation = errors.New("对话历史文件发生并发变化")

type conversationHistoryRepairResult struct {
	ScannedFiles    int
	ScannedRecords  int
	InvalidRecords  int
	ChangedFiles    int
	ChangedRecords  int
	RepairedFiles   int
	RepairedRecords int
	ChangedBytes    int64
	MaxChangedBytes int64
	RequiredSpace   uint64
	FreeSpace       uint64
	ActiveProcesses []string
	BackupDir       *string
}

type conversationHistoryRepairProgress struct {
	Phase          string
	Percent        int
	Detail         string
	ProcessedFiles int
	TotalFiles     int
	ProcessedBytes int64
	TotalBytes     int64
	CurrentFile    string
	Result         conversationHistoryRepairResult
}

type conversationHistoryRepairTask struct {
	ID              string
	Status          string
	Phase           string
	Percent         int
	Detail          string
	CancelRequested bool
	ProcessedFiles  int
	TotalFiles      int
	ProcessedBytes  int64
	TotalBytes      int64
	CurrentFile     string
	Result          conversationHistoryRepairResult
	cancel          context.CancelFunc
	done            chan struct{}
}

type conversationHistoryRepairProgressFunc func(conversationHistoryRepairProgress)

type conversationHistoryFileScan struct {
	Path           string
	Size           int64
	ModTimeUnixNs  int64
	Mode           os.FileMode
	Hash           [sha256.Size]byte
	ScannedRecords int
	InvalidRecords int
	ChangedRecords int
}

type conversationHistoryObjectMember struct {
	Key        string
	KeyStart   int
	ValueStart int
	ValueEnd   int
	PrevComma  int
	CommaAfter int
}

type conversationHistoryProgressReader struct {
	ctx     context.Context
	reader  io.Reader
	onBytes func(int64)
}

func (r conversationHistoryProgressReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(buffer)
	if n > 0 && r.onBytes != nil {
		r.onBytes(int64(n))
	}
	return n, err
}

func (s *server) repairConversationHistory() commandResult {
	s.conversationHistoryRepairMu.Lock()
	if current := s.conversationHistoryRepairTask; current != nil && (current.Status == "running" || current.Status == "cancelling") {
		payload := conversationHistoryRepairTaskPayload(current)
		s.conversationHistoryRepairMu.Unlock()
		return commandResultWithStatus("accepted", "对话历史兼容修复已在运行。", payload)
	}
	taskID, err := newConversationHistoryRepairTaskID()
	if err != nil {
		s.conversationHistoryRepairMu.Unlock()
		return failed("无法创建对话历史修复任务："+err.Error(), conversationHistoryRepairTaskPayload(nil))
	}
	ctx, cancel := context.WithCancel(context.Background())
	task := &conversationHistoryRepairTask{
		ID:      taskID,
		Status:  "running",
		Phase:   "starting",
		Percent: 0,
		Detail:  "正在启动对话历史兼容修复。",
		cancel:  cancel,
		done:    make(chan struct{}),
	}
	s.conversationHistoryRepairTask = task
	payload := conversationHistoryRepairTaskPayload(task)
	s.conversationHistoryRepairMu.Unlock()

	go s.runConversationHistoryRepairTask(ctx, task)
	return commandResultWithStatus("accepted", "对话历史兼容修复已开始。", payload)
}

func (s *server) conversationHistoryRepairStatus(args map[string]any) commandResult {
	requestedID := strings.TrimSpace(stringArg(args, "taskId"))
	s.conversationHistoryRepairMu.Lock()
	defer s.conversationHistoryRepairMu.Unlock()
	task := s.conversationHistoryRepairTask
	if task == nil {
		return ok("当前没有对话历史修复任务。", conversationHistoryRepairTaskPayload(nil))
	}
	if requestedID != "" && requestedID != task.ID {
		return failed("对话历史修复任务不存在或已被新任务替换。", conversationHistoryRepairTaskPayload(task))
	}
	return ok("对话历史修复任务状态已读取。", conversationHistoryRepairTaskPayload(task))
}

func (s *server) cancelConversationHistoryRepair(args map[string]any) commandResult {
	requestedID := strings.TrimSpace(stringArg(args, "taskId"))
	s.conversationHistoryRepairMu.Lock()
	task := s.conversationHistoryRepairTask
	if task == nil {
		payload := conversationHistoryRepairTaskPayload(nil)
		s.conversationHistoryRepairMu.Unlock()
		return ok("当前没有可取消的对话历史修复任务。", payload)
	}
	if requestedID != "" && requestedID != task.ID {
		payload := conversationHistoryRepairTaskPayload(task)
		s.conversationHistoryRepairMu.Unlock()
		return failed("任务编号不匹配，未取消当前对话历史修复任务。", payload)
	}
	if task.Status != "running" && task.Status != "cancelling" {
		payload := conversationHistoryRepairTaskPayload(task)
		s.conversationHistoryRepairMu.Unlock()
		return ok("对话历史修复任务已经结束。", payload)
	}
	task.Status = "cancelling"
	task.CancelRequested = true
	task.Detail = "正在安全停止；已完成的文件会保留，当前文件不会写入一半。"
	cancel := task.cancel
	payload := conversationHistoryRepairTaskPayload(task)
	s.conversationHistoryRepairMu.Unlock()
	if cancel != nil {
		cancel()
	}
	return commandResultWithStatus("accepted", "已请求取消对话历史兼容修复。", payload)
}

func (s *server) runConversationHistoryRepairTask(ctx context.Context, task *conversationHistoryRepairTask) {
	defer close(task.done)
	result, err := repairConversationHistoryNamespacesWithContext(ctx, codexHomeDir(), func(progress conversationHistoryRepairProgress) {
		s.conversationHistoryRepairMu.Lock()
		defer s.conversationHistoryRepairMu.Unlock()
		if s.conversationHistoryRepairTask != task {
			return
		}
		if !task.CancelRequested {
			task.Phase = progress.Phase
			task.Detail = progress.Detail
		}
		if progress.Percent > task.Percent {
			task.Percent = progress.Percent
		}
		task.ProcessedFiles = progress.ProcessedFiles
		task.TotalFiles = progress.TotalFiles
		task.ProcessedBytes = progress.ProcessedBytes
		task.TotalBytes = progress.TotalBytes
		task.CurrentFile = progress.CurrentFile
		task.Result = progress.Result
	})

	s.conversationHistoryRepairMu.Lock()
	defer s.conversationHistoryRepairMu.Unlock()
	if s.conversationHistoryRepairTask != task {
		return
	}
	task.Result = result
	task.cancel = nil
	if errors.Is(err, context.Canceled) {
		task.Status = "cancelled"
		task.Phase = "cancelled"
		task.Detail = fmt.Sprintf("修复已取消；已完成 %d 个文件、%d 条记录，可稍后重新执行继续修复。", result.RepairedFiles, result.RepairedRecords)
		return
	}
	if err != nil {
		task.Status = "failed"
		task.Phase = "failed"
		task.Detail = "对话历史兼容修复失败：" + err.Error()
		return
	}
	task.Status = "ok"
	task.Phase = "completed"
	task.Percent = 100
	if result.ChangedFiles == 0 {
		task.Detail = fmt.Sprintf("已扫描 %d 个会话文件、%d 条记录，没有发现需要移除的 namespace 字段。", result.ScannedFiles, result.ScannedRecords)
		return
	}
	task.Detail = fmt.Sprintf("修复完成：已扫描 %d 个文件、%d 条记录，修复 %d 个文件中的 %d 条记录；原文件已备份。", result.ScannedFiles, result.ScannedRecords, result.RepairedFiles, result.RepairedRecords)
}

func (s *server) shutdownConversationHistoryRepair(ctx context.Context) error {
	s.conversationHistoryRepairMu.Lock()
	task := s.conversationHistoryRepairTask
	if task == nil || (task.Status != "running" && task.Status != "cancelling") {
		s.conversationHistoryRepairMu.Unlock()
		return nil
	}
	task.Status = "cancelling"
	task.CancelRequested = true
	task.Detail = "管理工具正在退出，等待对话历史修复安全停止。"
	cancel := task.cancel
	done := task.done
	s.conversationHistoryRepairMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func newConversationHistoryRepairTaskID() (string, error) {
	var id [12]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(id[:]), nil
}

func conversationHistoryRepairTaskPayload(task *conversationHistoryRepairTask) map[string]any {
	status := "idle"
	phase := "idle"
	percent := 0
	detail := "尚未启动对话历史兼容修复。"
	taskID := ""
	cancelRequested := false
	processedFiles := 0
	totalFiles := 0
	processedBytes := int64(0)
	totalBytes := int64(0)
	currentFile := ""
	result := conversationHistoryRepairResult{}
	if task != nil {
		taskID = task.ID
		status = task.Status
		phase = task.Phase
		percent = task.Percent
		detail = task.Detail
		cancelRequested = task.CancelRequested
		processedFiles = task.ProcessedFiles
		totalFiles = task.TotalFiles
		processedBytes = task.ProcessedBytes
		totalBytes = task.TotalBytes
		currentFile = task.CurrentFile
		result = task.Result
	}
	return map[string]any{
		"taskId":              taskID,
		"taskStatus":          status,
		"phase":               phase,
		"percent":             percent,
		"detail":              detail,
		"cancelRequested":     cancelRequested,
		"processedFiles":      processedFiles,
		"totalFiles":          totalFiles,
		"processedBytes":      processedBytes,
		"totalBytes":          totalBytes,
		"currentFile":         currentFile,
		"scannedFiles":        result.ScannedFiles,
		"scannedRecords":      result.ScannedRecords,
		"invalidRecords":      result.InvalidRecords,
		"changedFiles":        result.ChangedFiles,
		"changedRecords":      result.ChangedRecords,
		"repairedFiles":       result.RepairedFiles,
		"repairedRecords":     result.RepairedRecords,
		"changedBytes":        result.ChangedBytes,
		"maxChangedFileBytes": result.MaxChangedBytes,
		"requiredSpaceBytes":  result.RequiredSpace,
		"freeSpaceBytes":      result.FreeSpace,
		"activeProcesses":     result.ActiveProcesses,
		"backupDir":           result.BackupDir,
		"codexHome":           codexHomeDir(),
	}
}

// repairConversationHistoryNamespaces removes only the top-level namespace key
// from response_item payloads whose type is function_call or custom_tool_call.
// Nested arguments/input fields and all other response item types are untouched.
func repairConversationHistoryNamespaces(home string) (conversationHistoryRepairResult, error) {
	return repairConversationHistoryNamespacesWithContext(context.Background(), home, nil)
}

func repairConversationHistoryNamespacesWithContext(ctx context.Context, home string, report conversationHistoryRepairProgressFunc) (conversationHistoryRepairResult, error) {
	result := conversationHistoryRepairResult{}
	if ctx == nil {
		ctx = context.Background()
	}
	emit := func(phase string, percent int, detail string, processedFiles, totalFiles int, processedBytes, totalBytes int64, currentFile string) {
		if report == nil {
			return
		}
		if percent < 0 {
			percent = 0
		} else if percent > 100 {
			percent = 100
		}
		report(conversationHistoryRepairProgress{
			Phase: phase, Percent: percent, Detail: detail,
			ProcessedFiles: processedFiles, TotalFiles: totalFiles,
			ProcessedBytes: processedBytes, TotalBytes: totalBytes,
			CurrentFile: currentFile, Result: result,
		})
	}
	emit("starting", 1, "正在检查 Codex 历史目录和运行状态。", 0, 0, 0, 0, "")
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !isDir(home) {
		return result, fmt.Errorf("Codex home 不存在：%s", home)
	}
	releaseLock, err := acquireConversationHistoryRepairLock(home)
	if err != nil {
		return result, err
	}
	defer releaseLock()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	activeProcesses, err := detectConversationHistoryActiveProcesses()
	if err != nil {
		return result, fmt.Errorf("检查 ChatGPT/Codex 运行状态失败：%w", err)
	}
	result.ActiveProcesses = activeProcesses
	if len(activeProcesses) > 0 {
		return result, fmt.Errorf("请先完全退出 ChatGPT 和 Codex 后再修复（仍在运行：%s）", strings.Join(activeProcesses, "、"))
	}

	emit("discovering", 3, "正在统计对话历史文件。", 0, 0, 0, 0, "")
	paths, err := conversationHistoryJSONLPathsWithContext(ctx, home)
	if err != nil {
		return result, err
	}
	var scanTotalBytes int64
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		info, statErr := os.Stat(path)
		if statErr != nil {
			return result, fmt.Errorf("统计 %s 失败：%w", path, statErr)
		}
		scanTotalBytes += info.Size()
	}
	emit("scanning", 5, fmt.Sprintf("准备扫描 %d 个对话历史文件。", len(paths)), 0, len(paths), 0, scanTotalBytes, "")
	scans := make([]conversationHistoryFileScan, 0, len(paths))
	var scannedBytes int64
	for index, path := range paths {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		fileBytes := int64(0)
		scan, scanErr := scanConversationHistoryFileWithContext(ctx, path, func(delta int64) {
			fileBytes += delta
			currentBytes := scannedBytes + fileBytes
			emit("scanning", conversationHistoryProgressPercent(5, 50, currentBytes, scanTotalBytes),
				fmt.Sprintf("正在扫描 %d/%d：%s", index+1, len(paths), filepath.Base(path)),
				index, len(paths), currentBytes, scanTotalBytes, path)
		})
		if scanErr != nil {
			return result, fmt.Errorf("扫描 %s 失败：%w", path, scanErr)
		}
		scannedBytes += fileBytes
		scans = append(scans, scan)
		result.ScannedFiles++
		result.ScannedRecords += scan.ScannedRecords
		result.InvalidRecords += scan.InvalidRecords
		if scan.ChangedRecords > 0 {
			result.ChangedFiles++
			result.ChangedRecords += scan.ChangedRecords
			result.ChangedBytes += scan.Size
			if scan.Size > result.MaxChangedBytes {
				result.MaxChangedBytes = scan.Size
			}
		}
		emit("scanning", conversationHistoryProgressPercent(5, 50, scannedBytes, scanTotalBytes),
			fmt.Sprintf("已扫描 %d/%d 个文件。", index+1, len(paths)),
			index+1, len(paths), scannedBytes, scanTotalBytes, path)
	}
	if result.ChangedFiles == 0 {
		emit("completed", 100, "扫描完成，没有发现需要修复的 namespace 字段。", len(paths), len(paths), scannedBytes, scanTotalBytes, "")
		return result, nil
	}
	emit("preparing", 57, fmt.Sprintf("发现 %d 个文件需要修复，正在执行写入前安全检查。", result.ChangedFiles),
		0, result.ChangedFiles, 0, result.ChangedBytes, "")
	if err := ctx.Err(); err != nil {
		return result, err
	}
	activeProcesses, err = detectConversationHistoryActiveProcesses()
	if err != nil {
		return result, fmt.Errorf("扫描后检查 ChatGPT/Codex 运行状态失败：%w", err)
	}
	result.ActiveProcesses = activeProcesses
	if len(activeProcesses) > 0 {
		return result, fmt.Errorf("扫描期间检测到 ChatGPT/Codex 启动，已停止且尚未修改文件（%s）", strings.Join(activeProcesses, "、"))
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	releaseLauncherGuard, err := acquireConversationHistoryLauncherGuard()
	if err != nil {
		return result, err
	}
	defer releaseLauncherGuard()
	result.RequiredSpace = conversationHistoryRequiredSpace(result.ChangedBytes, result.MaxChangedBytes)
	result.FreeSpace, err = conversationHistoryAvailableDiskBytes(home)
	if err != nil {
		return result, fmt.Errorf("检查备份磁盘空间失败：%w", err)
	}
	if result.FreeSpace < result.RequiredSpace {
		return result, fmt.Errorf("磁盘空间不足：完整备份和原子替换至少需要 %s，当前可用 %s", formatConversationHistoryBytes(result.RequiredSpace), formatConversationHistoryBytes(result.FreeSpace))
	}
	emit("preparing", 60,
		fmt.Sprintf("空间检查通过：需要 %s，可用 %s。", formatConversationHistoryBytes(result.RequiredSpace), formatConversationHistoryBytes(result.FreeSpace)),
		0, result.ChangedFiles, 0, result.ChangedBytes, "")
	if err := ctx.Err(); err != nil {
		return result, err
	}

	backupDir, err := createConversationHistoryRepairBackupDir(home)
	if err != nil {
		return result, fmt.Errorf("创建备份目录失败：%w", err)
	}
	result.BackupDir = &backupDir
	changedScans := make([]conversationHistoryFileScan, 0, result.ChangedFiles)
	backupFiles := make([]string, 0, result.ChangedFiles)
	for _, scan := range scans {
		if scan.ChangedRecords == 0 {
			continue
		}
		changedScans = append(changedScans, scan)
		relative, relErr := filepath.Rel(home, scan.Path)
		if relErr != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return result, fmt.Errorf("无法为会话文件创建安全备份路径：%s", scan.Path)
		}
		backupFiles = append(backupFiles, relative)
	}
	metadata := map[string]any{
		"managedBy":           "ChatGPT Codex Tools conversation history repair",
		"createdAt":           time.Now().Format(time.RFC3339Nano),
		"status":              "prepared",
		"scannedFiles":        result.ScannedFiles,
		"scannedRecords":      result.ScannedRecords,
		"invalidRecords":      result.InvalidRecords,
		"changedFiles":        result.ChangedFiles,
		"changedRecords":      result.ChangedRecords,
		"changedBytes":        result.ChangedBytes,
		"maxChangedFileBytes": result.MaxChangedBytes,
		"requiredSpaceBytes":  result.RequiredSpace,
		"freeSpaceBytes":      result.FreeSpace,
		"files":               backupFiles,
	}
	if err := atomicWriteJSON(filepath.Join(backupDir, "metadata.json"), metadata); err != nil {
		return result, fmt.Errorf("写入备份清单失败：%w", err)
	}
	var repairedBytes int64
	markCancelled := func() error {
		metadata["status"] = "cancelled"
		metadata["cancelledAt"] = time.Now().Format(time.RFC3339Nano)
		metadata["appliedFiles"] = result.RepairedFiles
		metadata["appliedRecords"] = result.RepairedRecords
		_ = atomicWriteJSON(filepath.Join(backupDir, "metadata.json"), metadata)
		emit("cancelled", conversationHistoryProgressPercent(60, 38, repairedBytes, result.ChangedBytes),
			fmt.Sprintf("已安全取消；保留已完成的 %d 个文件。", result.RepairedFiles),
			result.RepairedFiles, result.ChangedFiles, repairedBytes, result.ChangedBytes, "")
		if err := ctx.Err(); err != nil {
			return err
		}
		return context.Canceled
	}

	applied := make([]conversationHistoryFileScan, 0, len(changedScans))
	for index, scan := range changedScans {
		if err := ctx.Err(); err != nil {
			return result, markCancelled()
		}
		fileBytes := int64(0)
		repairErr := backupAndRepairConversationHistoryFileWithContext(ctx, home, backupDir, scan, func(delta int64) {
			fileBytes += delta
			currentBytes := repairedBytes + fileBytes
			emit("repairing", conversationHistoryProgressPercent(60, 38, currentBytes, result.ChangedBytes),
				fmt.Sprintf("正在备份并修复 %d/%d：%s", index+1, len(changedScans), filepath.Base(scan.Path)),
				index, len(changedScans), currentBytes, result.ChangedBytes, scan.Path)
		})
		if repairErr != nil {
			if errors.Is(repairErr, context.Canceled) || errors.Is(repairErr, context.DeadlineExceeded) {
				return result, markCancelled()
			}
			err := repairErr
			unsafeReason := error(nil)
			unsafeFromOperation := errors.Is(err, errConversationHistoryWriterStateUnsafe) || errors.Is(err, errConversationHistoryConcurrentMutation)
			if unsafeFromOperation {
				unsafeReason = err
			} else if checkErr := ensureConversationHistoryDirectProcessesStopped("准备回滚"); checkErr != nil {
				unsafeReason = checkErr
			}
			if unsafeReason != nil {
				metadata["status"] = "interrupted"
				metadata["interruptedAt"] = time.Now().Format(time.RFC3339Nano)
				metadata["appliedFiles"] = len(applied)
				metadata["error"] = err.Error()
				_ = atomicWriteJSON(filepath.Join(backupDir, "metadata.json"), metadata)
				riskDetail := ""
				if !unsafeFromOperation {
					riskDetail = "；" + unsafeReason.Error()
				}
				return result, fmt.Errorf("修复 %s 失败：%w%s。为避免覆盖新写入，未自动回滚已完成的 %d 个文件；完全退出 ChatGPT/Codex 后重试会继续修复其余文件", scan.Path, err, riskDetail, len(applied))
			}
			rollbackErr := rollbackConversationHistoryFiles(home, backupDir, applied)
			if rollbackErr != nil {
				metadata["status"] = "rollback_failed"
				metadata["failedAt"] = time.Now().Format(time.RFC3339Nano)
				metadata["error"] = err.Error()
				metadata["rollbackError"] = rollbackErr.Error()
				_ = atomicWriteJSON(filepath.Join(backupDir, "metadata.json"), metadata)
				return result, fmt.Errorf("修复 %s 失败：%w；回滚也失败：%v", scan.Path, err, rollbackErr)
			}
			metadata["status"] = "rolled_back"
			metadata["rolledBackAt"] = time.Now().Format(time.RFC3339Nano)
			metadata["error"] = err.Error()
			_ = atomicWriteJSON(filepath.Join(backupDir, "metadata.json"), metadata)
			result.RepairedFiles = 0
			result.RepairedRecords = 0
			return result, fmt.Errorf("修复 %s 失败：%w；已回滚本次已修改文件", scan.Path, err)
		}
		repairedBytes += fileBytes
		applied = append(applied, scan)
		result.RepairedFiles++
		result.RepairedRecords += scan.ChangedRecords
		emit("repairing", conversationHistoryProgressPercent(60, 38, repairedBytes, result.ChangedBytes),
			fmt.Sprintf("已完成 %d/%d 个文件。", index+1, len(changedScans)),
			index+1, len(changedScans), repairedBytes, result.ChangedBytes, scan.Path)
	}
	emit("finishing", 99, "正在写入完成状态。", len(changedScans), len(changedScans), repairedBytes, result.ChangedBytes, "")
	metadata["status"] = "completed"
	metadata["completedAt"] = time.Now().Format(time.RFC3339Nano)
	metadata["appliedFiles"] = len(applied)
	if err := atomicWriteJSON(filepath.Join(backupDir, "metadata.json"), metadata); err != nil {
		return result, fmt.Errorf("修复已完成，但更新备份清单失败：%w", err)
	}
	emit("completed", 100, "对话历史兼容修复完成。", len(changedScans), len(changedScans), repairedBytes, result.ChangedBytes, "")
	return result, nil
}

func conversationHistoryJSONLPaths(home string) ([]string, error) {
	return conversationHistoryJSONLPathsWithContext(context.Background(), home)
}

func conversationHistoryJSONLPathsWithContext(ctx context.Context, home string) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var paths []string
	for _, dirname := range []string{"sessions", "archived_sessions"} {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		root := filepath.Join(home, dirname)
		if !isDir(root) {
			continue
		}
		if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if walkErr != nil {
				return walkErr
			}
			name := entry.Name()
			if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasPrefix(name, "rollout-") || !strings.HasSuffix(name, ".jsonl") {
				return nil
			}
			paths = append(paths, path)
			return nil
		}); err != nil {
			return nil, fmt.Errorf("遍历 %s 失败：%w", root, err)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func scanConversationHistoryFile(path string) (conversationHistoryFileScan, error) {
	return scanConversationHistoryFileWithContext(context.Background(), path, nil)
}

func scanConversationHistoryFileWithContext(ctx context.Context, path string, onBytes func(int64)) (conversationHistoryFileScan, error) {
	scan := conversationHistoryFileScan{Path: path}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return scan, err
	}
	file, err := os.Open(path)
	if err != nil {
		return scan, err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return scan, err
	}
	scan.Size = before.Size()
	scan.ModTimeUnixNs = before.ModTime().UnixNano()
	scan.Mode = before.Mode()
	hasher := sha256.New()
	reader := bufio.NewReaderSize(conversationHistoryProgressReader{ctx: ctx, reader: file, onBytes: onBytes}, 64*1024)
	for {
		if err := ctx.Err(); err != nil {
			return scan, err
		}
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			_, _ = hasher.Write(line)
			if len(bytes.TrimSpace(conversationHistoryJSONLBody(line))) > 0 {
				scan.ScannedRecords++
				_, changed, repairErr := repairConversationHistoryJSONLine(line)
				if repairErr != nil {
					scan.InvalidRecords++
				} else if changed {
					scan.ChangedRecords++
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return scan, readErr
		}
	}
	if err := ctx.Err(); err != nil {
		return scan, err
	}
	after, err := file.Stat()
	if err != nil {
		return scan, err
	}
	if after.Size() != before.Size() || after.ModTime().UnixNano() != before.ModTime().UnixNano() {
		return scan, errors.New("文件在扫描期间发生变化，请关闭正在写入该会话的 Codex 后重试")
	}
	copy(scan.Hash[:], hasher.Sum(nil))
	return scan, nil
}

func createConversationHistoryRepairBackupDir(home string) (string, error) {
	root := filepath.Join(home, "backups_state", conversationHistoryRepairBackupKind)
	name := time.Now().Format("20060102-150405.000")
	backupDir := filepath.Join(root, name)
	for suffix := 2; fileExists(backupDir); suffix++ {
		backupDir = filepath.Join(root, fmt.Sprintf("%s-%d", name, suffix))
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return "", err
	}
	return backupDir, nil
}

func acquireConversationHistoryRepairLock(home string) (func(), error) {
	release, err := acquireProviderSyncLock(home, "conversation-history-repair")
	if err != nil {
		return nil, fmt.Errorf("另一个历史会话维护任务正在运行，请稍后重试：%w", err)
	}
	return release, nil
}

func defaultConversationHistoryLauncherGuard() (func(), error) {
	guard, acquired, err := acquireLauncherSingleInstanceLock(defaultWatcherDebugPort)
	if err != nil {
		return nil, fmt.Errorf("锁定 ChatGPT Codex 启动入口失败：%w", err)
	}
	if !acquired {
		return nil, errors.New("扫描后检测到 ChatGPT Codex 正在启动或运行，已停止且尚未修改文件")
	}
	return guard.release, nil
}

func defaultConversationHistoryActiveProcesses() ([]string, error) {
	names, err := detectConversationHistoryDirectProcesses()
	if err != nil {
		return nil, err
	}
	if tcpPortAccepting(launcherGuardPort) {
		names = append(names, "ChatGPT Codex 后端")
	}
	return uniqueConversationHistoryProcessNames(names), nil
}

func defaultConversationHistoryDirectProcesses() ([]string, error) {
	var names []string
	if runtime.GOOS == "windows" {
		output, err := runConversationHistoryProcessCommand("tasklist", "/FO", "CSV", "/NH")
		if err != nil {
			return nil, err
		}
		reader := csv.NewReader(bytes.NewReader(output))
		for {
			record, readErr := reader.Read()
			if errors.Is(readErr, io.EOF) {
				break
			}
			if readErr != nil || len(record) == 0 {
				continue
			}
			if name := conversationHistoryTargetProcessName(record[0]); name != "" {
				names = append(names, name)
			}
		}
	} else {
		output, err := runConversationHistoryProcessCommand("ps", "-axo", "comm=")
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(string(output), "\n") {
			if name := conversationHistoryTargetProcessName(filepath.Base(strings.TrimSpace(line))); name != "" {
				names = append(names, name)
			}
		}
	}
	return uniqueConversationHistoryProcessNames(names), nil
}

func ensureConversationHistoryDirectProcessesStopped(stage string) error {
	active, err := detectConversationHistoryDirectProcesses()
	if err != nil {
		return fmt.Errorf("%w：%s时检查 ChatGPT/Codex 运行状态失败：%v", errConversationHistoryWriterStateUnsafe, stage, err)
	}
	if len(active) > 0 {
		return fmt.Errorf("%w：%s时检测到正在运行的 %s", errConversationHistoryWriterStateUnsafe, stage, strings.Join(active, "、"))
	}
	return nil
}

func runConversationHistoryProcessCommand(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	hideSubprocessWindow(cmd)
	output, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return output, err
}

func conversationHistoryTargetProcessName(value string) string {
	name := strings.ToLower(strings.TrimSuffix(filepath.Base(strings.TrimSpace(value)), filepath.Ext(value)))
	switch name {
	case "chatgpt":
		return "ChatGPT"
	case "codex":
		return "Codex"
	default:
		return ""
	}
}

func uniqueConversationHistoryProcessNames(names []string) []string {
	seen := map[string]bool{}
	unique := make([]string, 0, len(names))
	for _, name := range names {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		unique = append(unique, name)
	}
	sort.Strings(unique)
	return unique
}

func conversationHistoryRequiredSpace(changedBytes, maxChangedBytes int64) uint64 {
	if changedBytes <= 0 || maxChangedBytes <= 0 {
		return 0
	}
	sum := uint64(changedBytes)
	maxFile := uint64(maxChangedBytes)
	if sum > ^uint64(0)-maxFile {
		return ^uint64(0)
	}
	peak := sum + maxFile
	margin := peak / 10
	if margin < conversationHistoryRepairMinDiskMargin {
		margin = conversationHistoryRepairMinDiskMargin
	}
	if peak > ^uint64(0)-margin {
		return ^uint64(0)
	}
	return peak + margin
}

func conversationHistoryProgressPercent(start, span int, done, total int64) int {
	if span < 0 {
		span = 0
	}
	if total <= 0 {
		return start + span
	}
	if done < 0 {
		done = 0
	} else if done > total {
		done = total
	}
	return start + int(done*int64(span)/total)
}

func formatConversationHistoryBytes(value uint64) string {
	const gib = uint64(1024 * 1024 * 1024)
	const mib = uint64(1024 * 1024)
	if value >= gib {
		return fmt.Sprintf("%.2f GiB", float64(value)/float64(gib))
	}
	return fmt.Sprintf("%.1f MiB", float64(value)/float64(mib))
}

func backupAndRepairConversationHistoryFile(home, backupDir string, scan conversationHistoryFileScan) error {
	return backupAndRepairConversationHistoryFileWithContext(context.Background(), home, backupDir, scan, nil)
}

func backupAndRepairConversationHistoryFileWithContext(ctx context.Context, home, backupDir string, scan conversationHistoryFileScan, onBytes func(int64)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	relative, err := filepath.Rel(home, scan.Path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("会话文件不在 Codex home 内")
	}
	backupPath := filepath.Join(backupDir, relative)
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(scan.Path), 0o755); err != nil {
		return err
	}

	source, err := os.Open(scan.Path)
	if err != nil {
		return err
	}
	defer source.Close()
	backupTemp, err := os.CreateTemp(filepath.Dir(backupPath), ".history-backup-*")
	if err != nil {
		return err
	}
	backupTempPath := backupTemp.Name()
	defer os.Remove(backupTempPath)
	repairTemp, err := os.CreateTemp(filepath.Dir(scan.Path), ".history-repair-*")
	if err != nil {
		_ = backupTemp.Close()
		return err
	}
	repairTempPath := repairTemp.Name()
	defer os.Remove(repairTempPath)

	reader := bufio.NewReaderSize(conversationHistoryProgressReader{ctx: ctx, reader: source, onBytes: onBytes}, 64*1024)
	hasher := sha256.New()
	changedRecords := 0
	for {
		if err := ctx.Err(); err != nil {
			_ = repairTemp.Close()
			_ = backupTemp.Close()
			return err
		}
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			if err := writeConversationHistoryOriginal(backupTemp, hasher, line); err != nil {
				_ = repairTemp.Close()
				_ = backupTemp.Close()
				return err
			}
			updated := line
			if len(bytes.TrimSpace(conversationHistoryJSONLBody(line))) > 0 {
				repaired, changed, repairErr := repairConversationHistoryJSONLine(line)
				if repairErr == nil && changed {
					updated = repaired
					changedRecords++
				}
			}
			if _, err := repairTemp.Write(updated); err != nil {
				_ = repairTemp.Close()
				_ = backupTemp.Close()
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			_ = repairTemp.Close()
			_ = backupTemp.Close()
			return readErr
		}
	}
	if err := ctx.Err(); err != nil {
		_ = repairTemp.Close()
		_ = backupTemp.Close()
		return err
	}
	current, err := source.Stat()
	if err != nil {
		_ = repairTemp.Close()
		_ = backupTemp.Close()
		return err
	}
	if current.Size() != scan.Size || current.ModTime().UnixNano() != scan.ModTimeUnixNs || !bytes.Equal(hasher.Sum(nil), scan.Hash[:]) {
		_ = repairTemp.Close()
		_ = backupTemp.Close()
		return fmt.Errorf("%w：文件在备份前发生变化，请重试", errConversationHistoryConcurrentMutation)
	}
	if changedRecords != scan.ChangedRecords {
		_ = repairTemp.Close()
		_ = backupTemp.Close()
		return fmt.Errorf("%w：待修复记录数量发生变化，请重试", errConversationHistoryConcurrentMutation)
	}
	if err := ctx.Err(); err != nil {
		_ = repairTemp.Close()
		_ = backupTemp.Close()
		return err
	}
	if err := finishConversationHistoryTemp(backupTemp, backupTempPath, backupPath, scan.Mode); err != nil {
		_ = repairTemp.Close()
		return fmt.Errorf("写入备份失败：%w", err)
	}
	if err := ctx.Err(); err != nil {
		_ = repairTemp.Close()
		return err
	}
	if err := prepareConversationHistoryTemp(repairTemp, scan.Mode); err != nil {
		return fmt.Errorf("同步修复临时文件失败：%w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ensureConversationHistoryDirectProcessesStopped("替换会话文件"); err != nil {
		return err
	}
	pathInfo, err := os.Stat(scan.Path)
	if err != nil || !os.SameFile(current, pathInfo) || pathInfo.Size() != scan.Size || pathInfo.ModTime().UnixNano() != scan.ModTimeUnixNs {
		return fmt.Errorf("%w：文件路径在原子替换前发生变化，请重试", errConversationHistoryConcurrentMutation)
	}
	// Windows does not allow replacing an open file. Close the source handle
	// explicitly immediately before the atomic rename instead of relying on
	// the defer. The repaired temp file is already synced and closed above.
	if err := source.Close(); err != nil {
		return fmt.Errorf("关闭原会话文件失败：%w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := replaceFile(repairTempPath, scan.Path); err != nil {
		return fmt.Errorf("原子替换会话文件失败：%w", err)
	}
	return nil
}

func writeConversationHistoryOriginal(target io.Writer, hasher hash.Hash, data []byte) error {
	if _, err := target.Write(data); err != nil {
		return err
	}
	_, err := hasher.Write(data)
	return err
}

func finishConversationHistoryTemp(file *os.File, tempPath, targetPath string, mode os.FileMode) error {
	if err := prepareConversationHistoryTemp(file, mode); err != nil {
		return err
	}
	return replaceFile(tempPath, targetPath)
}

func prepareConversationHistoryTemp(file *os.File, mode os.FileMode) error {
	if err := file.Chmod(mode.Perm()); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return nil
}

func rollbackConversationHistoryFiles(home, backupDir string, scans []conversationHistoryFileScan) error {
	var rollbackErrors []string
	for index := len(scans) - 1; index >= 0; index-- {
		scan := scans[index]
		relative, err := filepath.Rel(home, scan.Path)
		if err != nil {
			rollbackErrors = append(rollbackErrors, err.Error())
			continue
		}
		backupPath := filepath.Join(backupDir, relative)
		if err := atomicCopyConversationHistoryFile(backupPath, scan.Path, scan.Mode); err != nil {
			rollbackErrors = append(rollbackErrors, scan.Path+": "+err.Error())
			if errors.Is(err, errConversationHistoryWriterStateUnsafe) {
				break
			}
		}
	}
	if len(rollbackErrors) > 0 {
		return errors.New(strings.Join(rollbackErrors, "；"))
	}
	return nil
}

func atomicCopyConversationHistoryFile(sourcePath, targetPath string, mode os.FileMode) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	temp, err := os.CreateTemp(filepath.Dir(targetPath), ".history-restore-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := io.Copy(temp, source); err != nil {
		_ = temp.Close()
		return err
	}
	if err := prepareConversationHistoryTemp(temp, mode); err != nil {
		return err
	}
	if err := ensureConversationHistoryDirectProcessesStopped("回滚会话文件"); err != nil {
		return err
	}
	return replaceFile(tempPath, targetPath)
}

func repairConversationHistoryJSONLine(line []byte) ([]byte, bool, error) {
	body := conversationHistoryJSONLBody(line)
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return line, false, err
	}
	if envelope.Type != "response_item" {
		return line, false, nil
	}
	outerMembers, err := parseConversationHistoryObjectMembers(body)
	if err != nil {
		return line, false, err
	}
	var payload *conversationHistoryObjectMember
	for index := range outerMembers {
		if outerMembers[index].Key == "payload" {
			payload = &outerMembers[index]
		}
	}
	if payload == nil {
		return line, false, nil
	}
	payloadJSON := body[payload.ValueStart:payload.ValueEnd]
	var payloadEnvelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(payloadJSON, &payloadEnvelope); err != nil {
		return line, false, err
	}
	if payloadEnvelope.Type != "function_call" && payloadEnvelope.Type != "custom_tool_call" {
		return line, false, nil
	}
	updatedPayload, removed, err := removeConversationHistoryObjectKey(payloadJSON, "namespace")
	if err != nil {
		return line, false, err
	}
	if removed == 0 {
		return line, false, nil
	}
	updated := make([]byte, 0, len(line)-len(payloadJSON)+len(updatedPayload))
	updated = append(updated, body[:payload.ValueStart]...)
	updated = append(updated, updatedPayload...)
	updated = append(updated, body[payload.ValueEnd:]...)
	updated = append(updated, line[len(body):]...)
	return updated, true, nil
}

func conversationHistoryJSONLBody(line []byte) []byte {
	end := len(line)
	if end > 0 && line[end-1] == '\n' {
		end--
		if end > 0 && line[end-1] == '\r' {
			end--
		}
	}
	return line[:end]
}

func removeConversationHistoryObjectKey(data []byte, key string) ([]byte, int, error) {
	updated := data
	removed := 0
	for {
		members, err := parseConversationHistoryObjectMembers(updated)
		if err != nil {
			return data, 0, err
		}
		matchIndex := -1
		for index := range members {
			if members[index].Key == key {
				matchIndex = index
				break
			}
		}
		if matchIndex < 0 {
			return updated, removed, nil
		}
		member := members[matchIndex]
		start := member.KeyStart
		end := member.ValueEnd
		if member.PrevComma >= 0 {
			start = member.PrevComma
		} else if member.CommaAfter >= 0 {
			end = member.CommaAfter + 1
		}
		next := make([]byte, 0, len(updated)-(end-start))
		next = append(next, updated[:start]...)
		next = append(next, updated[end:]...)
		updated = next
		removed++
	}
}

func parseConversationHistoryObjectMembers(data []byte) ([]conversationHistoryObjectMember, error) {
	i := skipConversationHistoryJSONSpace(data, 0)
	if i >= len(data) || data[i] != '{' {
		return nil, errors.New("JSON 值不是对象")
	}
	i++
	prevComma := -1
	members := []conversationHistoryObjectMember{}
	for {
		i = skipConversationHistoryJSONSpace(data, i)
		if i >= len(data) {
			return nil, errors.New("JSON 对象未闭合")
		}
		if data[i] == '}' {
			i = skipConversationHistoryJSONSpace(data, i+1)
			if i != len(data) {
				return nil, errors.New("JSON 对象后存在多余内容")
			}
			return members, nil
		}
		keyStart := i
		keyEnd, err := scanConversationHistoryJSONString(data, i)
		if err != nil {
			return nil, err
		}
		var key string
		if err := json.Unmarshal(data[keyStart:keyEnd], &key); err != nil {
			return nil, err
		}
		i = skipConversationHistoryJSONSpace(data, keyEnd)
		if i >= len(data) || data[i] != ':' {
			return nil, errors.New("JSON 对象成员缺少冒号")
		}
		valueStart := skipConversationHistoryJSONSpace(data, i+1)
		valueEnd, err := scanConversationHistoryJSONValue(data, valueStart)
		if err != nil {
			return nil, err
		}
		i = skipConversationHistoryJSONSpace(data, valueEnd)
		member := conversationHistoryObjectMember{
			Key: key, KeyStart: keyStart, ValueStart: valueStart, ValueEnd: valueEnd,
			PrevComma: prevComma, CommaAfter: -1,
		}
		if i < len(data) && data[i] == ',' {
			member.CommaAfter = i
			members = append(members, member)
			prevComma = i
			i++
			continue
		}
		if i < len(data) && data[i] == '}' {
			members = append(members, member)
			i = skipConversationHistoryJSONSpace(data, i+1)
			if i != len(data) {
				return nil, errors.New("JSON 对象后存在多余内容")
			}
			return members, nil
		}
		return nil, errors.New("JSON 对象成员后缺少逗号或右花括号")
	}
}

func scanConversationHistoryJSONString(data []byte, start int) (int, error) {
	if start >= len(data) || data[start] != '"' {
		return 0, errors.New("JSON 对象键不是字符串")
	}
	for i := start + 1; i < len(data); i++ {
		switch data[i] {
		case '\\':
			i++
			if i >= len(data) {
				return 0, errors.New("JSON 字符串转义未完成")
			}
		case '"':
			return i + 1, nil
		}
	}
	return 0, errors.New("JSON 字符串未闭合")
}

func scanConversationHistoryJSONValue(data []byte, start int) (int, error) {
	if start >= len(data) {
		return 0, errors.New("JSON 值为空")
	}
	if data[start] == '"' {
		return scanConversationHistoryJSONString(data, start)
	}
	if data[start] == '{' || data[start] == '[' {
		stack := []byte{matchingConversationHistoryJSONCloser(data[start])}
		for i := start + 1; i < len(data); i++ {
			switch data[i] {
			case '"':
				end, err := scanConversationHistoryJSONString(data, i)
				if err != nil {
					return 0, err
				}
				i = end - 1
			case '{', '[':
				stack = append(stack, matchingConversationHistoryJSONCloser(data[i]))
			case '}', ']':
				if len(stack) == 0 || data[i] != stack[len(stack)-1] {
					return 0, errors.New("JSON 容器括号不匹配")
				}
				stack = stack[:len(stack)-1]
				if len(stack) == 0 {
					return i + 1, nil
				}
			}
		}
		return 0, errors.New("JSON 容器未闭合")
	}
	for i := start; i < len(data); i++ {
		switch data[i] {
		case ' ', '\t', '\r', '\n', ',', '}', ']':
			if i == start {
				return 0, errors.New("JSON 值无效")
			}
			return i, nil
		}
	}
	return len(data), nil
}

func matchingConversationHistoryJSONCloser(value byte) byte {
	if value == '{' {
		return '}'
	}
	return ']'
}

func skipConversationHistoryJSONSpace(data []byte, start int) int {
	for start < len(data) {
		switch data[start] {
		case ' ', '\t', '\r', '\n':
			start++
		default:
			return start
		}
	}
	return start
}
