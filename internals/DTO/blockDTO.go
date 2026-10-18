package dto

type CreateBlockDTO struct {
	URL            string `json:"url" binding:"required"`
	AllowDuplicate bool   `json:"allowDuplicate"`
}

type ImportBlocksDTO struct {
	URLs []string `json:"urls"`
}

type ImportBlockIssueDTO struct {
	Index   int    `json:"index"`
	URL     string `json:"url"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ImportBlocksResponseDTO struct {
	Created  []BlockResponseDTO    `json:"created"`
	Warnings []ImportBlockIssueDTO `json:"warnings"`
	Failed   []ImportBlockIssueDTO `json:"failed"`
}

type UpdateBlockDTO struct {
	Title       *string   `json:"title"`
	Description *string   `json:"description"`
	Tags        *[]string `json:"tags"`
	Order       *float64  `json:"order"`
}

type BlockResponseDTO struct {
	Id          string   `json:"id"`
	PageId      string   `json:"pageId"`
	URL         string   `json:"url"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	CoverImage  string   `json:"coverImage,omitempty"`
	Favicon     string   `json:"favicon,omitempty"`
	SiteName    string   `json:"siteName,omitempty"`
	FetchStatus string   `json:"fetchStatus"`
	Tags        []string `json:"tags"`
	Order       float64  `json:"order"`
	CreatedAt   string   `json:"createdAt"`
	UpdatedAt   string   `json:"updatedAt"`
}
