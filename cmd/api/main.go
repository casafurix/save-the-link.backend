package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/smil-thakur/save-the-link/internals/config"
	authcontroller "github.com/smil-thakur/save-the-link/internals/controllers/auth_controller"
	blockcontroller "github.com/smil-thakur/save-the-link/internals/controllers/block_controller"
	pagecontroller "github.com/smil-thakur/save-the-link/internals/controllers/page_controller"
	searchcontroller "github.com/smil-thakur/save-the-link/internals/controllers/search_controller"
	workspacecontroller "github.com/smil-thakur/save-the-link/internals/controllers/workspace_controller"
	"github.com/smil-thakur/save-the-link/internals/crypto"
	jwtservice "github.com/smil-thakur/save-the-link/internals/jwt_service"
	"github.com/smil-thakur/save-the-link/internals/middleware"
	"github.com/smil-thakur/save-the-link/internals/repository"
	"github.com/smil-thakur/save-the-link/internals/routes"
	authservice "github.com/smil-thakur/save-the-link/internals/services/auth_service"
	blockservice "github.com/smil-thakur/save-the-link/internals/services/block_service"
	pageservice "github.com/smil-thakur/save-the-link/internals/services/page_service"
	searchservice "github.com/smil-thakur/save-the-link/internals/services/search_service"
	workspaceservice "github.com/smil-thakur/save-the-link/internals/services/workspace_service"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func main() {
	r := gin.Default()
	config := config.GetConfig()

	client, err := mongo.Connect(options.Client().ApplyURI(config.Mongo_db_connection_string))
	if err != nil {
		log.Fatalf("Unable to connect to mongo db %v", err)
	}

	pingCtx, cancelPing := context.WithTimeout(context.Background(), 10*time.Second)
	if err := client.Ping(pingCtx, nil); err != nil {
		cancelPing()
		log.Fatalf("Unable to ping mongo db %v", err)
	}
	cancelPing()

	indexCtx, cancelIndexes := context.WithTimeout(context.Background(), 2*time.Minute)
	indexWarnings, err := repository.EnsureIndexes(indexCtx, client)
	cancelIndexes()
	if err != nil {
		log.Fatalf("Unable to create mongo indexes %v", err)
	}
	for _, warning := range indexWarnings {
		log.Printf("WARNING: %v; remove duplicate email records so the uniqueness constraint can be enabled", warning)
	}
	passwordManager := crypto.NewPasswordManager()
	jwtService := jwtservice.NewJWTService(config.JWT_secret)

	userRepository := repository.NewUserRepository(client, context.Background(), passwordManager)
	authService := authservice.NewAuthService(userRepository)

	pageRepository := repository.NewPageRepository(client, context.Background())
	blockRepository := repository.NewBlockRepository(client, context.Background(), pageRepository)

	pageService := pageservice.NewPageService(pageRepository, blockRepository, userRepository)
	pageController := pagecontroller.NewPageController(pageService)

	blockService := blockservice.NewBlockService(blockRepository)
	blockController := blockcontroller.NewBlockController(blockService, pageService)

	searchService := searchservice.NewSearchService(pageRepository, blockRepository)
	searchController := searchcontroller.NewSearchController(searchService)
	workspaceService := workspaceservice.NewWorkspaceService(pageRepository, blockRepository)
	workspaceController := workspacecontroller.NewWorkspaceController(workspaceService)

	authController := authcontroller.NewAuthController(authService, pageService, jwtService)

	// Registered before any routes: Gin bakes the middleware chain in at route
	// registration time, so CORS added after a route (e.g. after /ping) would
	// never actually apply to it.
	r.Use(cors.New(cors.Config{
		AllowOrigins:     config.AllowedOrigins,
		AllowMethods:     []string{"GET", "POST", "PATCH", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: true,
	}))

	r.GET("/ping", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{
			"message": "pong",
		})
	})

	routes.AuthRoutes(r, authController)

	r.GET("/me", middleware.AuthMiddleWare(jwtService), authController.Me)
	r.DELETE("/me", middleware.AuthMiddleWare(jwtService), authController.DeleteAccount)
	r.GET("/users/search", middleware.AuthMiddleWare(jwtService), authController.SearchUsers)

	routes.PublicPageRoutes(r, pageController)
	routes.PublicBlockRoutes(r, blockController)

	protected := r.Group("/", middleware.AuthMiddleWare(jwtService))
	routes.PageRoutes(protected, pageController)
	routes.BlockRoutes(protected, blockController)
	routes.SearchRoutes(protected, searchController)
	routes.WorkspaceRoutes(protected, workspaceController)

	if err := r.Run(":" + config.Port); err != nil {
		log.Fatal(err)
	}
}
