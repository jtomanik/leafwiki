package importer

import (
	"context"
	"errors"
	"io"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	coreimporter "github.com/perber/wiki/internal/importer"
)

// ─── CreateImportPlanUseCase ─────────────────────────────────────────────────

type CreateImportPlanInput struct {
	File           io.Reader
	TargetBasePath string
}

type CreateImportPlanOutput struct {
	Plan *coreimporter.CurrentPlanState
}

type importPlanCreator interface {
	CreateImportPlanFromZipUpload(io.Reader, string) (*coreimporter.PlanResult, error)
	GetCurrentPlan() (*coreimporter.CurrentPlanState, error)
}

type CreateImportPlanUseCase struct {
	svc importPlanCreator
}

func NewCreateImportPlanUseCase(svc *coreimporter.ImporterService) *CreateImportPlanUseCase {
	return &CreateImportPlanUseCase{svc: svc}
}

func (uc *CreateImportPlanUseCase) Execute(_ context.Context, in CreateImportPlanInput) (*CreateImportPlanOutput, error) {
	if _, err := uc.svc.CreateImportPlanFromZipUpload(in.File, in.TargetBasePath); err != nil {
		return nil, err
	}
	plan, err := uc.svc.GetCurrentPlan()
	if err != nil {
		return nil, err
	}
	return &CreateImportPlanOutput{Plan: plan}, nil
}

// ─── GetImportPlanUseCase ────────────────────────────────────────────────────

type GetImportPlanOutput struct {
	Plan *coreimporter.CurrentPlanState
}

type importPlanGetter interface {
	GetCurrentPlan() (*coreimporter.CurrentPlanState, error)
}

type GetImportPlanUseCase struct {
	svc importPlanGetter
}

func NewGetImportPlanUseCase(svc *coreimporter.ImporterService) *GetImportPlanUseCase {
	return &GetImportPlanUseCase{svc: svc}
}

func (uc *GetImportPlanUseCase) Execute(_ context.Context) (*GetImportPlanOutput, error) {
	plan, err := uc.svc.GetCurrentPlan()
	if err != nil {
		if errors.Is(err, coreimporter.ErrNoPlan) {
			return nil, sharederrors.NewLocalizedErrorFromCode(ErrCodeImporterNoPlan, err)
		}
		return nil, err
	}
	return &GetImportPlanOutput{Plan: plan}, nil
}

// ─── ExecuteImportUseCase ────────────────────────────────────────────────────

type ExecuteImportInput struct {
	UserID tree.UserID
}

type ExecuteImportOutput struct {
	State   *coreimporter.CurrentPlanState
	Started bool
}

type importPlanExecutor interface {
	StartCurrentPlanExecution(tree.UserID) (*coreimporter.CurrentPlanState, bool, error)
}

type ExecuteImportUseCase struct {
	svc importPlanExecutor
}

func NewExecuteImportUseCase(svc *coreimporter.ImporterService) *ExecuteImportUseCase {
	return &ExecuteImportUseCase{svc: svc}
}

func (uc *ExecuteImportUseCase) Execute(_ context.Context, in ExecuteImportInput) (*ExecuteImportOutput, error) {
	state, started, err := uc.svc.StartCurrentPlanExecution(in.UserID)
	if err != nil {
		if errors.Is(err, coreimporter.ErrImportExecutionRunning) {
			return nil, sharederrors.NewLocalizedErrorFromCode(ErrCodeImporterExecutionRunning, err)
		}
		if errors.Is(err, coreimporter.ErrNoPlan) {
			return nil, sharederrors.NewLocalizedErrorFromCode(ErrCodeImporterNoPlan, err)
		}
		if errors.Is(err, coreimporter.ErrImportStateUnavailable) {
			return nil, sharederrors.NewLocalizedErrorFromCode(ErrCodeImporterStateUnavailable, err)
		}
		return nil, err
	}
	return &ExecuteImportOutput{State: state, Started: started}, nil
}

// ─── ClearImportPlanUseCase ──────────────────────────────────────────────────

type ClearImportPlanUseCase struct {
	svc importPlanClearer
}

func NewClearImportPlanUseCase(svc *coreimporter.ImporterService) *ClearImportPlanUseCase {
	return &ClearImportPlanUseCase{svc: svc}
}

type importPlanClearer interface {
	CancelCurrentPlan() (*coreimporter.CurrentPlanState, bool, error)
	ClearCurrentPlan() error
}

func (uc *ClearImportPlanUseCase) Execute(_ context.Context) (*coreimporter.CurrentPlanState, error) {
	state, _, err := uc.svc.CancelCurrentPlan()
	if err == nil && state != nil && state.ExecutionStatus == coreimporter.ExecutionStatusRunning && state.CancelRequested {
		return state, nil
	}
	if err != nil && !errors.Is(err, coreimporter.ErrNoPlan) {
		if errors.Is(err, coreimporter.ErrImportStateUnavailable) {
			return nil, sharederrors.NewLocalizedErrorFromCode(ErrCodeImporterStateUnavailable, err)
		}
		return nil, err
	}
	if err := uc.svc.ClearCurrentPlan(); err != nil {
		return nil, err
	}
	var clearedPlan *coreimporter.CurrentPlanState
	return clearedPlan, nil
}
