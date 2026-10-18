package workspacecontroller

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	dto "github.com/smil-thakur/save-the-link/internals/DTO"
	customerrors "github.com/smil-thakur/save-the-link/internals/custom_errors"
	workspaceservice "github.com/smil-thakur/save-the-link/internals/services/workspace_service"
)

const maxImportBodyBytes = 10 << 20

type WorkspaceController struct {
	service *workspaceservice.WorkspaceService
}

func NewWorkspaceController(service *workspaceservice.WorkspaceService) *WorkspaceController {
	return &WorkspaceController{service: service}
}

func (c *WorkspaceController) Export(ctx *gin.Context) {
	archive, err := c.service.Export(ctx.MustGet("userId").(string))
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"message": fmt.Sprintf("%v", err)})
		return
	}
	ctx.JSON(http.StatusOK, archive)
}

func (c *WorkspaceController) Import(ctx *gin.Context) {
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, maxImportBodyBytes)
	var archive dto.WorkspaceArchiveDTO
	if err := ctx.ShouldBindJSON(&archive); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_workspace", "message": customerrors.ErrorInvalidWorkspace.Error()})
		return
	}
	result, err := c.service.Import(ctx.MustGet("userId").(string), archive)
	if err != nil {
		if errors.Is(err, customerrors.ErrorInvalidWorkspace) {
			ctx.JSON(http.StatusBadRequest, gin.H{"code": "invalid_workspace", "message": err.Error()})
			return
		}
		ctx.JSON(http.StatusInternalServerError, gin.H{"message": fmt.Sprintf("%v", err)})
		return
	}
	ctx.JSON(http.StatusCreated, result)
}
