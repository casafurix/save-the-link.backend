package blockcontroller_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	blockcontroller "github.com/smil-thakur/save-the-link/internals/controllers/block_controller"
	mongomodels "github.com/smil-thakur/save-the-link/internals/mongo_models"
	"github.com/smil-thakur/save-the-link/internals/repository"
	"github.com/smil-thakur/save-the-link/internals/routes"
	blockservice "github.com/smil-thakur/save-the-link/internals/services/block_service"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestCreateBlockWarnsForNormalizedSamePageDuplicateAndAllowsRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	mongoURI := os.Getenv("MONGO_TEST_URI")
	if mongoURI == "" {
		t.Skip("set MONGO_TEST_URI to run MongoDB integration tests")
	}

	client, err := mongo.Connect(options.Client().ApplyURI(mongoURI))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Disconnect(ctx) })

	ownerID := bson.NewObjectID()
	pageRepository := repository.NewPageRepository(client, ctx)
	page, err := pageRepository.CreatePage(ownerID.Hex(), "Duplicate test", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = client.Database("Auth").Collection("Block").DeleteMany(ctx, bson.M{"pageId": page.Id})
		_, _ = client.Database("Auth").Collection("Page").DeleteOne(ctx, bson.M{"_id": page.Id})
	})

	_, err = client.Database("Auth").Collection("Block").InsertOne(ctx, mongomodels.BlockMongo{
		PageId: page.Id,
		URL:    "http://example.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	blockRepository := repository.NewBlockRepository(client, ctx, pageRepository)
	controller := blockcontroller.NewBlockController(blockservice.NewBlockService(blockRepository), nil)
	router := gin.New()
	protected := router.Group("/", func(c *gin.Context) {
		c.Set("userId", ownerID.Hex())
		c.Next()
	})
	routes.BlockRoutes(protected, controller)

	request := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/pages/"+page.Id.Hex()+"/blocks", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}

	response := request(`{"url":"  HTTP://EXAMPLE.COM:80/#section  "}`)
	if response.Code != http.StatusConflict {
		t.Fatalf("expected duplicate warning status %d, got %d: %s", http.StatusConflict, response.Code, response.Body.String())
	}

	var warning struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &warning); err != nil {
		t.Fatal(err)
	}
	if warning.Code != "duplicate_url" {
		t.Fatalf("expected stable duplicate_url code, got %q", warning.Code)
	}

	response = request(`{"url":"  HTTP://EXAMPLE.COM:80/#section  ","allowDuplicate":true}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected override status %d, got %d: %s", http.StatusCreated, response.Code, response.Body.String())
	}

	count, err := client.Database("Auth").Collection("Block").CountDocuments(ctx, bson.M{"pageId": page.Id})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected duplicate override to create a second block, got %d blocks", count)
	}
}
