package proxy

import (
	"context"
	"errors"
	"log/slog"
)

type CanvasRemovalError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type CanvasRemovalStatus struct {
	SchemeID string              `json:"schemeId"`
	State    string              `json:"state"`
	Error    *CanvasRemovalError `json:"error,omitempty"`
	Result   *CanvasRemoveResult `json:"result,omitempty"`
}
type canvasRemovalRun struct {
	done   chan struct{}
	result CanvasRemoveResult
	err    error
}

// Outcomes are in memory and lost on panel restart. A missing scheme (404)
// also signals successful removal to clients after the outcome has been lost.
// This lock is independent of canvasMu, which the executing removal holds.
func (s *Service) CanvasRemovalStatus(id string) CanvasRemovalStatus {
	s.removalMu.Lock()
	defer s.removalMu.Unlock()
	status := CanvasRemovalStatus{SchemeID: id, State: "idle"}
	run := s.removalRuns[id]
	if run == nil {
		return status
	}
	select {
	case <-run.done:
		status.Result = &run.result
		status.State = "succeeded"
		if run.err != nil {
			status.State = "failed"
			status.Error = canvasRemovalError(run.err)
		}
	default:
		status.State = "removing"
	}
	return status
}

func canvasRemovalError(err error) *CanvasRemovalError {
	code, message := "invalid_request", "撤除失败，请刷新影响预览并查看面板日志"
	var phase *canvasRemovalPhaseError
	if errors.As(err, &phase) {
		message = phase.message
	}
	var conflict *CanvasRemoveConflict
	switch {
	case errors.Is(err, ErrStaleScheme):
		code, message = "stale_scheme", "方案已在其他地方修改，请刷新"
	case errors.Is(err, ErrRelayDependency):
		code = "dependency_not_ready"
	case errors.Is(err, ErrRemovalApply):
		code = "remove_apply_failed"
	case errors.Is(err, ErrNotFound):
		code, message = "not_found", "方案不存在"
	case errors.As(err, &conflict):
		code, message = "canvas_remove_conflict", conflict.Error()
	case errors.Is(err, context.Canceled):
		code, message = "canvas_remove_canceled", "面板正在关闭，撤除已中止，请查看节点恢复状态"
	}
	return &CanvasRemovalError{Code: code, Message: message}
}

func (s *Service) startCanvasRemoval(ctx context.Context, id string, input CanvasRemoveInput, author string, users AccessReader, finish func(context.Context, CanvasRemoveResult, error) error) (*canvasRemovalRun, bool, error) {
	s.removalMu.Lock()
	defer s.removalMu.Unlock()
	if s.removalClosed {
		return nil, false, errors.New("面板正在关闭，无法开始撤除")
	}
	if run := s.removalRuns[id]; run != nil {
		select {
		case <-run.done:
		default:
			return run, true, nil
		}
	}
	if s.removalContext == nil {
		s.removalContext, s.removalCancel = context.WithCancel(context.Background())
	}
	if s.removalRuns == nil {
		s.removalRuns = make(map[string]*canvasRemovalRun)
	}
	run := &canvasRemovalRun{done: make(chan struct{})}
	s.removalRuns[id] = run
	work, cancel := context.WithCancel(context.WithoutCancel(ctx))
	stop := context.AfterFunc(s.removalContext, cancel)
	s.removalJobs.Add(1)
	go func() {
		defer s.removalJobs.Done()
		defer cancel()
		defer stop()
		run.result, run.err = s.RemoveCanvasScheme(work, id, input, author, users)
		if run.err != nil {
			slog.Error("canvas removal failed", "scheme_id", id, "error", run.err)
		}
		if finish != nil {
			run.err = errors.Join(run.err, finish(context.WithoutCancel(work), run.result, run.err))
		}
		close(run.done)
	}()
	return run, false, nil
}

// StopCanvasRemovals must finish before closing the database: cancellation
// restores local intent and persists rollback commands for outstanding receipts.
func (s *Service) StopCanvasRemovals() {
	s.removalMu.Lock()
	s.removalClosed = true
	if s.removalCancel != nil {
		s.removalCancel()
	}
	s.removalMu.Unlock()
	s.removalJobs.Wait()
}
