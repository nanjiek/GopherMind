package httpcontracts

import "time"

// UploadDocumentData is returned after a document upload is accepted.
type UploadDocumentData struct {
	DocumentID string    `json:"document_id"`
	JobID      string    `json:"job_id"`
	Status     string    `json:"status"`
	Filename   string    `json:"filename"`
	CreatedAt  time.Time `json:"created_at"`
}

// DocumentData describes the current state of an indexed document.
type DocumentData struct {
	DocumentID   string    `json:"document_id"`
	JobID        string    `json:"job_id"`
	Status       string    `json:"status"`
	Filename     string    `json:"filename"`
	ErrorMessage string    `json:"error_message,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}
