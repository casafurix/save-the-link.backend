package dto

import "time"

type WorkspaceArchiveDTO struct {
	Version    int                `json:"version"`
	ExportedAt time.Time          `json:"exportedAt,omitempty"`
	Pages      []WorkspacePageDTO `json:"pages"`
}

type WorkspacePageDTO struct {
	SourceID       string              `json:"sourceId"`
	ParentSourceID *string             `json:"parentSourceId,omitempty"`
	Title          string              `json:"title"`
	Icon           *string             `json:"icon,omitempty"`
	Layout         string              `json:"layout"`
	Order          float64             `json:"order"`
	Blocks         []WorkspaceBlockDTO `json:"blocks"`
}

type WorkspaceBlockDTO struct {
	URL         string   `json:"url"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	CoverImage  string   `json:"coverImage,omitempty"`
	Favicon     string   `json:"favicon,omitempty"`
	SiteName    string   `json:"siteName,omitempty"`
	FetchStatus string   `json:"fetchStatus"`
	Tags        []string `json:"tags"`
	Order       float64  `json:"order"`
}

type WorkspaceImportResponseDTO struct {
	PagesCreated  int `json:"pagesCreated"`
	BlocksCreated int `json:"blocksCreated"`
}
