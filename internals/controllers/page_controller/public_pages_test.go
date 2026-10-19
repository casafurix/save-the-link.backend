package pagecontroller_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	blockcontroller "github.com/smil-thakur/save-the-link/internals/controllers/block_controller"
	pagecontroller "github.com/smil-thakur/save-the-link/internals/controllers/page_controller"
	"github.com/smil-thakur/save-the-link/internals/crypto"
	"github.com/smil-thakur/save-the-link/internals/repository"
	"github.com/smil-thakur/save-the-link/internals/routes"
	blockservice "github.com/smil-thakur/save-the-link/internals/services/block_service"
	pageservice "github.com/smil-thakur/save-the-link/internals/services/page_service"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestPublicPagesIncludeCreatorWithoutLeakingOwnerData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mongoURI := os.Getenv("MONGO_TEST_URI")
	if mongoURI == "" {
		t.Skip("set MONGO_TEST_URI to run MongoDB integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(mongoURI))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Disconnect(context.Background()) })

	database := client.Database("Auth")
	users := database.Collection("User")
	pages := database.Collection("Page")
	blocks := database.Collection("Block")
	creatorID := bson.NewObjectID()
	viewerID := bson.NewObjectID()
	missingCreatorID := bson.NewObjectID()
	publicPageID := bson.NewObjectID()
	privatePageID := bson.NewObjectID()
	missingCreatorPageID := bson.NewObjectID()
	unique := bson.NewObjectID().Hex()
	publicSlug := "public-attribution-" + unique
	privateSlug := "private-attribution-" + unique
	missingCreatorSlug := "missing-creator-" + unique
	now := time.Now().Add(24 * time.Hour)

	_, err = users.InsertMany(ctx, []any{
		bson.M{"_id": creatorID, "username": "Public Author", "email": "private@example.com", "password": "private-hash"},
		bson.M{"_id": viewerID, "username": "Viewer", "email": "viewer@example.com", "password": "viewer-hash", "bookmarkedPageIds": bson.A{publicPageID, privatePageID, missingCreatorPageID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = pages.InsertMany(ctx, []any{
		publicPageDocument(publicPageID, creatorID, publicSlug, "Public page", "public", now),
		publicPageDocument(privatePageID, creatorID, privateSlug, "Private page", "private", now.Add(time.Second)),
		publicPageDocument(missingCreatorPageID, missingCreatorID, missingCreatorSlug, "Orphaned page", "public", now.Add(2*time.Second)),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = blocks.InsertMany(ctx, []any{
		bson.M{"_id": bson.NewObjectID(), "pageId": publicPageID, "url": "https://example.com/one"},
		bson.M{"_id": bson.NewObjectID(), "pageId": publicPageID, "url": "https://example.com/two"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = blocks.DeleteMany(cleanupCtx, bson.M{"pageId": bson.M{"$in": bson.A{publicPageID, privatePageID, missingCreatorPageID}}})
		_, _ = pages.DeleteMany(cleanupCtx, bson.M{"_id": bson.M{"$in": bson.A{publicPageID, privatePageID, missingCreatorPageID}}})
		_, _ = users.DeleteMany(cleanupCtx, bson.M{"_id": bson.M{"$in": bson.A{creatorID, viewerID}}})
	})

	pageRepository := repository.NewPageRepository(client, context.Background())
	userRepository := repository.NewUserRepository(client, context.Background(), crypto.NewPasswordManager())
	blockRepository := repository.NewBlockRepository(client, context.Background(), pageRepository)
	pageService := pageservice.NewPageService(pageRepository, blockRepository, userRepository)
	pageController := pagecontroller.NewPageController(pageService)
	blockController := blockcontroller.NewBlockController(blockservice.NewBlockService(blockRepository), pageService)
	router := gin.New()
	routes.PublicPageRoutes(router, pageController)
	routes.PublicBlockRoutes(router, blockController)
	router.GET("/bookmarks", func(c *gin.Context) {
		c.Set("userId", viewerID.Hex())
		c.Next()
	}, pageController.ListBookmarks)

	list := requestJSON(t, router, "/public/pages")
	if list.Code != http.StatusOK {
		t.Fatalf("expected public list status 200, got %d: %s", list.Code, list.Body.String())
	}
	var summaries []map[string]any
	decodeJSON(t, list, &summaries)
	publicSummary := summaryBySlug(t, summaries, publicSlug)
	if publicSummary["creatorUsername"] != "Public Author" || publicSummary["linkCount"] != float64(2) {
		t.Fatalf("unexpected attributed summary: %#v", publicSummary)
	}
	assertNoPrivateCreatorFields(t, publicSummary)
	if hasSummaryWithSlug(summaries, privateSlug) {
		t.Fatal("private page appeared in public list")
	}
	missingSummary := summaryBySlug(t, summaries, missingCreatorSlug)
	if missingSummary["creatorUsername"] != repository.UnknownCreatorUsername {
		t.Fatalf("expected missing creator fallback, got %#v", missingSummary)
	}

	detail := requestJSON(t, router, "/public/pages/"+publicSlug)
	if detail.Code != http.StatusOK {
		t.Fatalf("expected public detail status 200, got %d: %s", detail.Code, detail.Body.String())
	}
	var publicDetail map[string]any
	decodeJSON(t, detail, &publicDetail)
	if publicDetail["creatorUsername"] != "Public Author" {
		t.Fatalf("unexpected detail attribution: %#v", publicDetail)
	}
	assertNoPrivateCreatorFields(t, publicDetail)

	missingDetail := requestJSON(t, router, "/public/pages/"+missingCreatorSlug)
	if missingDetail.Code != http.StatusOK {
		t.Fatalf("expected orphaned public detail status 200, got %d: %s", missingDetail.Code, missingDetail.Body.String())
	}
	var orphanedDetail map[string]any
	decodeJSON(t, missingDetail, &orphanedDetail)
	if orphanedDetail["creatorUsername"] != repository.UnknownCreatorUsername {
		t.Fatalf("expected detail fallback, got %#v", orphanedDetail)
	}

	publicBlocks := requestJSON(t, router, "/public/pages/"+publicSlug+"/blocks")
	if publicBlocks.Code != http.StatusOK {
		t.Fatalf("expected public blocks route status 200, got %d: %s", publicBlocks.Code, publicBlocks.Body.String())
	}
	var publicBlockPayloads []map[string]any
	decodeJSON(t, publicBlocks, &publicBlockPayloads)
	if len(publicBlockPayloads) != 2 {
		t.Fatalf("expected two public blocks, got %#v", publicBlockPayloads)
	}
	blockURLs := make(map[string]bool, len(publicBlockPayloads))
	for _, block := range publicBlockPayloads {
		if block["pageId"] != publicPageID.Hex() {
			t.Fatalf("public block returned unexpected page data: %#v", block)
		}
		url, ok := block["url"].(string)
		if !ok {
			t.Fatalf("public block omitted URL: %#v", block)
		}
		blockURLs[url] = true
	}
	for _, expectedURL := range []string{"https://example.com/one", "https://example.com/two"} {
		if !blockURLs[expectedURL] {
			t.Fatalf("public blocks omitted %q: %#v", expectedURL, publicBlockPayloads)
		}
	}

	bookmarks := requestJSON(t, router, "/bookmarks")
	if bookmarks.Code != http.StatusOK {
		t.Fatalf("expected bookmarks status 200, got %d: %s", bookmarks.Code, bookmarks.Body.String())
	}
	var bookmarkSummaries []map[string]any
	decodeJSON(t, bookmarks, &bookmarkSummaries)
	if summaryBySlug(t, bookmarkSummaries, publicSlug)["creatorUsername"] != "Public Author" {
		t.Fatalf("bookmark attribution missing: %#v", bookmarkSummaries)
	}
	if summaryBySlug(t, bookmarkSummaries, missingCreatorSlug)["creatorUsername"] != repository.UnknownCreatorUsername {
		t.Fatalf("bookmark fallback missing: %#v", bookmarkSummaries)
	}
	if hasSummaryWithSlug(bookmarkSummaries, privateSlug) {
		t.Fatal("private page appeared in bookmarks")
	}
}

func publicPageDocument(id, ownerID bson.ObjectID, slug, title, visibility string, updatedAt time.Time) bson.M {
	return bson.M{
		"_id": id, "ownerId": ownerID, "slug": slug, "title": title,
		"visibility": visibility, "collaboration": "view", "layout": "grid",
		"collaboratorEmails": bson.A{"invitee@example.com"},
		"order":              1, "createdAt": updatedAt, "updatedAt": updatedAt,
	}
}

func requestJSON(t *testing.T, router http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func decodeJSON(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, response.Body.String())
	}
}

func summaryBySlug(t *testing.T, summaries []map[string]any, slug string) map[string]any {
	t.Helper()
	for _, summary := range summaries {
		if summary["slug"] == slug {
			return summary
		}
	}
	t.Fatalf("summary with slug %q not found in %#v", slug, summaries)
	return nil
}

func hasSummaryWithSlug(summaries []map[string]any, slug string) bool {
	for _, summary := range summaries {
		if summary["slug"] == slug {
			return true
		}
	}
	return false
}

func assertNoPrivateCreatorFields(t *testing.T, response map[string]any) {
	t.Helper()
	for _, field := range []string{"email", "password", "ownerId", "creatorId", "collaboratorEmails"} {
		if _, exists := response[field]; exists {
			t.Errorf("response exposed %s: %s", field, fmt.Sprint(response[field]))
		}
	}
}
