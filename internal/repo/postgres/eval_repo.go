package postgres

import (
	"context"

	"gorm.io/gorm"

	"gophermind/internal/core/model"
)

// EvalRunRepository persists LLM-as-a-Judge outputs.
type EvalRunRepository struct {
	db *gorm.DB
}

// NewEvalRunRepository builds EvalRunRepository.
func NewEvalRunRepository(db *gorm.DB) *EvalRunRepository {
	return &EvalRunRepository{db: db}
}

// Create stores one eval run.
func (r *EvalRunRepository) Create(ctx context.Context, run model.EvalRun) error {
	return r.db.WithContext(ctx).Create(&EvalRunModel{
		ID:                 run.ID,
		RequestID:          run.RequestID,
		TraceID:            run.TraceID,
		AnswerCorrectness:  run.AnswerCorrectness,
		AnswerCompleteness: run.AnswerCompleteness,
		Groundedness:       run.Groundedness,
		CitationSupport:    run.CitationSupport,
		HallucinationRisk:  run.HallucinationRisk,
		MedicalSafetyFlag:  run.MedicalSafetyFlag,
	}).Error
}
