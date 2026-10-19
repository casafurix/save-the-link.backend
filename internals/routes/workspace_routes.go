package routes

import (
	"github.com/gin-gonic/gin"
	workspacecontroller "github.com/smil-thakur/save-the-link/internals/controllers/workspace_controller"
)

func WorkspaceRoutes(r gin.IRoutes, controller *workspacecontroller.WorkspaceController) {
	r.GET("/workspace/export", controller.Export)
	r.POST("/workspace/import", controller.Import)
}
