package pagecontroller

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	dto "github.com/smil-thakur/save-the-link/internals/DTO"
	customerrors "github.com/smil-thakur/save-the-link/internals/custom_errors"
	mongomodels "github.com/smil-thakur/save-the-link/internals/mongo_models"
	"github.com/smil-thakur/save-the-link/internals/repository"
	pageservice "github.com/smil-thakur/save-the-link/internals/services/page_service"
)

type PageController struct {
	pageService *pageservice.PageService
}

func NewPageController(pageService *pageservice.PageService) *PageController {
	return &PageController{
		pageService: pageService,
	}
}

func ToPageResponseDTO(page mongomodels.PageMongo) dto.PageResponseDTO {
	var parentPageId *string

	if page.ParentPageId != nil {
		hex := page.ParentPageId.Hex()
		parentPageId = &hex
	}

	var deletedAt *string

	if page.DeletedAt != nil {
		formatted := page.DeletedAt.Format(time.RFC3339)
		deletedAt = &formatted
	}

	return dto.PageResponseDTO{
		Id:                 page.Id.Hex(),
		ParentPageId:       parentPageId,
		Title:              page.Title,
		Icon:               page.Icon,
		Layout:             page.Layout,
		Visibility:         page.Visibility,
		Collaboration:      page.Collaboration,
		Slug:               page.Slug,
		CollaboratorEmails: page.CollaboratorEmails,
		Order:              page.Order,
		CreatedAt:          page.CreatedAt.Format(time.RFC3339),
		UpdatedAt:          page.UpdatedAt.Format(time.RFC3339),
		DeletedAt:          deletedAt,
	}
}

func (c *PageController) respondWithError(ctx *gin.Context, err error) {
	switch {
	case errors.Is(err, customerrors.ErrorPageNotFound):
		ctx.AbortWithStatusJSON(http.StatusNotFound, gin.H{"message": err.Error()})
	case errors.Is(err, customerrors.ErrorForbidden):
		ctx.AbortWithStatusJSON(http.StatusForbidden, gin.H{"message": err.Error()})
	case errors.Is(err, customerrors.ErrorInvalidPage):
		ctx.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"message": err.Error()})
	case errors.Is(err, customerrors.ErrorCannotBookmarkOwnPage):
		ctx.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"message": err.Error()})
	default:
		ctx.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"message": fmt.Sprintf("%v", err),
		})
	}
}

func (c *PageController) CreatePage(ctx *gin.Context) {
	ownerId := ctx.MustGet("userId").(string)

	var body dto.CreatePageDTO

	if err := ctx.ShouldBindBodyWithJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	page, err := c.pageService.CreatePage(ownerId, body)

	if err != nil {
		c.respondWithError(ctx, err)
		return
	}

	ctx.JSON(http.StatusCreated, ToPageResponseDTO(*page))
}

func (c *PageController) ListPages(ctx *gin.Context) {
	ownerId := ctx.MustGet("userId").(string)

	pages, err := c.pageService.GetPages(ownerId)

	if err != nil {
		c.respondWithError(ctx, err)
		return
	}

	response := make([]dto.PageResponseDTO, len(pages))

	for i, page := range pages {
		response[i] = ToPageResponseDTO(page)
	}

	ctx.JSON(http.StatusOK, response)
}

func (c *PageController) GetPage(ctx *gin.Context) {
	ownerId := ctx.MustGet("userId").(string)
	id := ctx.Param("id")

	page, err := c.pageService.GetPage(id, ownerId)

	if err != nil {
		c.respondWithError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, ToPageResponseDTO(*page))
}

func (c *PageController) UpdatePage(ctx *gin.Context) {
	ownerId := ctx.MustGet("userId").(string)
	id := ctx.Param("id")

	var body dto.UpdatePageDTO

	if err := ctx.ShouldBindBodyWithJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	page, err := c.pageService.UpdatePage(id, ownerId, body)

	if err != nil {
		c.respondWithError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, ToPageResponseDTO(*page))
}

func (c *PageController) DeletePage(ctx *gin.Context) {
	ownerId := ctx.MustGet("userId").(string)
	id := ctx.Param("id")

	if err := c.pageService.DeletePage(id, ownerId); err != nil {
		c.respondWithError(ctx, err)
		return
	}

	ctx.Status(http.StatusNoContent)
}

func (c *PageController) PublishPage(ctx *gin.Context) {
	ownerId := ctx.MustGet("userId").(string)
	id := ctx.Param("id")

	var body dto.PublishPageDTO

	if err := ctx.ShouldBindBodyWithJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	page, err := c.pageService.PublishPage(id, ownerId, body)

	if err != nil {
		c.respondWithError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, ToPageResponseDTO(*page))
}

func (c *PageController) UnpublishPage(ctx *gin.Context) {
	ownerId := ctx.MustGet("userId").(string)
	id := ctx.Param("id")

	page, err := c.pageService.UnpublishPage(id, ownerId)

	if err != nil {
		c.respondWithError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, ToPageResponseDTO(*page))
}

func (c *PageController) GetPublicPage(ctx *gin.Context) {
	slug := ctx.Param("slug")

	page, err := c.pageService.GetPublicPageWithCreator(slug)

	if err != nil {
		c.respondWithError(ctx, err)
		return
	}

	// The collaborator list is only for the owner's Share dialog — anyone with
	// the link shouldn't be able to see who else was specifically invited.
	response := ToPageResponseDTO(*page.Page)
	response.CreatorUsername = page.CreatorUsername
	response.CollaboratorEmails = nil

	ctx.JSON(http.StatusOK, response)
}

func (c *PageController) ListTrash(ctx *gin.Context) {
	ownerId := ctx.MustGet("userId").(string)

	pages, err := c.pageService.GetTrash(ownerId)

	if err != nil {
		c.respondWithError(ctx, err)
		return
	}

	response := make([]dto.PageResponseDTO, len(pages))

	for i, page := range pages {
		response[i] = ToPageResponseDTO(page)
	}

	ctx.JSON(http.StatusOK, response)
}

func (c *PageController) RestorePage(ctx *gin.Context) {
	ownerId := ctx.MustGet("userId").(string)
	id := ctx.Param("id")

	page, err := c.pageService.RestorePage(id, ownerId)

	if err != nil {
		c.respondWithError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, ToPageResponseDTO(*page))
}

func (c *PageController) PermanentlyDeletePage(ctx *gin.Context) {
	ownerId := ctx.MustGet("userId").(string)
	id := ctx.Param("id")

	if err := c.pageService.PermanentlyDeletePage(id, ownerId); err != nil {
		c.respondWithError(ctx, err)
		return
	}

	ctx.Status(http.StatusNoContent)
}

func (c *PageController) SetCollaborators(ctx *gin.Context) {
	ownerId := ctx.MustGet("userId").(string)
	id := ctx.Param("id")

	var body dto.SetCollaboratorsDTO

	if err := ctx.ShouldBindBodyWithJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	page, notFound, err := c.pageService.SetCollaborators(id, ownerId, body.Emails)

	if err != nil {
		c.respondWithError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, dto.SetCollaboratorsResponseDTO{
		Page:           ToPageResponseDTO(*page),
		NotFoundEmails: notFound,
	})
}

func (c *PageController) ListPublicPages(ctx *gin.Context) {
	options, err := publicPageListOptions(ctx)

	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	summaries, err := c.pageService.ListPublicPages(options)

	if err != nil {
		c.respondWithError(ctx, err)
		return
	}

	response := make([]dto.PublicPageSummaryDTO, len(summaries))

	for i, summary := range summaries {
		response[i] = dto.PublicPageSummaryDTO{
			Id:              summary.Id.Hex(),
			Title:           summary.Title,
			Icon:            summary.Icon,
			Slug:            summary.Slug,
			CreatorUsername: summary.CreatorUsername,
			LinkCount:       summary.LinkCount,
		}
	}

	ctx.JSON(http.StatusOK, response)
}

const (
	defaultPublicPageLimit  = int64(12)
	maxPublicPageLimit      = int64(50)
	maxPublicPageOffset     = int64(10_000) + maxPublicPageLimit
	maxPublicPageQueryRunes = 100
)

func publicPageListOptions(ctx *gin.Context) (repository.PublicPageListOptions, error) {
	limit, err := parseBoundedQueryInt(ctx, "limit", defaultPublicPageLimit, 1, maxPublicPageLimit)
	if err != nil {
		return repository.PublicPageListOptions{}, err
	}

	offset, err := parseBoundedQueryInt(ctx, "offset", 0, 0, maxPublicPageOffset)
	if err != nil {
		return repository.PublicPageListOptions{}, err
	}

	rawQuery := ctx.Query("q")
	if !utf8.ValidString(rawQuery) {
		return repository.PublicPageListOptions{}, errors.New("q must be valid UTF-8")
	}

	query := strings.TrimSpace(rawQuery)
	queryRunes := utf8.RuneCountInString(query)
	if queryRunes == 1 {
		return repository.PublicPageListOptions{}, errors.New("q must be at least 2 characters")
	}
	if queryRunes > maxPublicPageQueryRunes {
		return repository.PublicPageListOptions{}, errors.New("q must be at most 100 characters")
	}

	return repository.PublicPageListOptions{Limit: limit, Offset: offset, Query: query}, nil
}

func parseBoundedQueryInt(ctx *gin.Context, name string, defaultValue, minValue, maxValue int64) (int64, error) {
	values, present := ctx.Request.URL.Query()[name]
	if !present {
		return defaultValue, nil
	}
	if len(values) != 1 || values[0] == "" {
		return 0, fmt.Errorf("%s must be an integer between %d and %d", name, minValue, maxValue)
	}

	value, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil || value < minValue || value > maxValue {
		return 0, fmt.Errorf("%s must be an integer between %d and %d", name, minValue, maxValue)
	}

	return value, nil
}

func (c *PageController) BookmarkPage(ctx *gin.Context) {
	userId := ctx.MustGet("userId").(string)
	id := ctx.Param("id")

	if err := c.pageService.Bookmark(id, userId); err != nil {
		c.respondWithError(ctx, err)
		return
	}

	ctx.Status(http.StatusNoContent)
}

func (c *PageController) UnbookmarkPage(ctx *gin.Context) {
	userId := ctx.MustGet("userId").(string)
	id := ctx.Param("id")

	if err := c.pageService.Unbookmark(id, userId); err != nil {
		c.respondWithError(ctx, err)
		return
	}

	ctx.Status(http.StatusNoContent)
}

func (c *PageController) ListBookmarks(ctx *gin.Context) {
	userId := ctx.MustGet("userId").(string)

	summaries, err := c.pageService.ListBookmarks(userId)

	if err != nil {
		c.respondWithError(ctx, err)
		return
	}

	response := make([]dto.PublicPageSummaryDTO, len(summaries))

	for i, summary := range summaries {
		response[i] = dto.PublicPageSummaryDTO{
			Id:              summary.Id.Hex(),
			Title:           summary.Title,
			Icon:            summary.Icon,
			Slug:            summary.Slug,
			CreatorUsername: summary.CreatorUsername,
			LinkCount:       summary.LinkCount,
		}
	}

	ctx.JSON(http.StatusOK, response)
}
