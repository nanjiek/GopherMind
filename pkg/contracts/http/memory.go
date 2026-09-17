package httpcontracts

import "time"

// CreateMemoryRequest is used to create a long-term memory item.
type CreateMemoryRequest struct {
	Content string   `json:"content" binding:"required"`
	Tags    []string `json:"tags,omitempty"`
}

// MemoryItemResponse represents one user-managed memory.
type MemoryItemResponse struct {
	MemoryID  string    `json:"memory_id"`
	Content   string    `json:"content"`
	Tags      []string  `json:"tags,omitempty"`
	Enabled   bool      `json:"enabled"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// MemoryListData describes GET /memories payload.
type MemoryListData struct {
	Items []MemoryItemResponse `json:"items"`
}
