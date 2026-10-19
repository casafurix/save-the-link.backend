package pagecontroller_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
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

func TestPublicPagesPaginationAndSearch(t *testing.T) {
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
	generalCreatorID := bson.NewObjectID()
	searchCreatorID := bson.NewObjectID()
	pageIDs := make([]bson.ObjectID, 0, 17)
	documents := make([]any, 0, 17)
	unique := bson.NewObjectID().Hex()
	baseTime := time.Date(2200, time.January, 1, 0, 0, 0, 0, time.UTC)

	_, err = users.InsertMany(ctx, []any{
		bson.M{"_id": generalCreatorID, "username": "General Author " + unique, "email": "general-" + unique + "@example.com", "password": "secret"},
		bson.M{"_id": searchCreatorID, "username": "Needle Creator " + unique, "email": "needle-" + unique + "@example.com", "password": "secret"},
	})
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 15; i++ {
		id := bson.NewObjectID()
		ownerID := generalCreatorID
		title := fmt.Sprintf("Pagination page %02d %s", i, unique)
		if i == 0 {
			title = "Deep [Archive] " + unique
		}
		if i == 1 {
			ownerID = searchCreatorID
		}
		pageIDs = append(pageIDs, id)
		documents = append(documents, publicPageDocument(id, ownerID, fmt.Sprintf("pagination-%s-%02d", unique, i), title, "public", baseTime.Add(time.Duration(i)*time.Minute)))
	}

	privateID := bson.NewObjectID()
	deletedID := bson.NewObjectID()
	pageIDs = append(pageIDs, privateID, deletedID)
	documents = append(documents,
		publicPageDocument(privateID, generalCreatorID, "private-search-"+unique, "Hidden Needle "+unique, "private", baseTime.Add(20*time.Minute)),
		publicPageDocument(deletedID, generalCreatorID, "deleted-search-"+unique, "Deleted Needle "+unique, "public", baseTime.Add(21*time.Minute)),
	)
	documents[len(documents)-1].(bson.M)["deletedAt"] = baseTime.Add(22 * time.Minute)

	if _, err = pages.InsertMany(ctx, documents); err != nil {
		t.Fatal(err)
	}
	if _, err = blocks.InsertOne(ctx, bson.M{"_id": bson.NewObjectID(), "pageId": pageIDs[0], "url": "https://example.com/archive"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = blocks.DeleteMany(cleanupCtx, bson.M{"pageId": bson.M{"$in": pageIDs}})
		_, _ = pages.DeleteMany(cleanupCtx, bson.M{"_id": bson.M{"$in": pageIDs}})
		_, _ = users.DeleteMany(cleanupCtx, bson.M{"_id": bson.M{"$in": bson.A{generalCreatorID, searchCreatorID}}})
	})

	pageRepository := repository.NewPageRepository(client, context.Background())
	userRepository := repository.NewUserRepository(client, context.Background(), crypto.NewPasswordManager())
	blockRepository := repository.NewBlockRepository(client, context.Background(), pageRepository)
	pageService := pageservice.NewPageService(pageRepository, blockRepository, userRepository)
	pageController := pagecontroller.NewPageController(pageService)
	router := gin.New()
	routes.PublicPageRoutes(router, pageController)

	defaultResponse := requestJSON(t, router, "/public/pages")
	if defaultResponse.Code != http.StatusOK {
		t.Fatalf("expected default status 200, got %d: %s", defaultResponse.Code, defaultResponse.Body.String())
	}
	var defaultSummaries []map[string]any
	decodeJSON(t, defaultResponse, &defaultSummaries)
	if len(defaultSummaries) != 12 {
		t.Fatalf("default response must remain an array of 12, got %d", len(defaultSummaries))
	}
	if defaultSummaries[0]["slug"] != fmt.Sprintf("pagination-%s-14", unique) || defaultSummaries[11]["slug"] != fmt.Sprintf("pagination-%s-03", unique) {
		t.Fatalf("default response was not ordered by most recent update: %#v", defaultSummaries)
	}
	assertSummaryFieldsArePublic(t, defaultSummaries)

	firstPage := requestJSON(t, router, "/public/pages?limit=5&offset=0")
	secondPage := requestJSON(t, router, "/public/pages?limit=5&offset=5")
	var firstSummaries, secondSummaries []map[string]any
	decodeJSON(t, firstPage, &firstSummaries)
	decodeJSON(t, secondPage, &secondSummaries)
	if len(firstSummaries) != 5 || len(secondSummaries) != 5 {
		t.Fatalf("expected two full pages, got %d and %d", len(firstSummaries), len(secondSummaries))
	}
	seen := map[string]bool{}
	for _, summary := range firstSummaries {
		seen[summary["id"].(string)] = true
	}
	for _, summary := range secondSummaries {
		if seen[summary["id"].(string)] {
			t.Fatalf("offset page overlapped first page at %#v", summary)
		}
	}

	titleSearch := requestJSON(t, router, "/public/pages?q="+url.QueryEscape("  [archive]  "))
	var titleMatches []map[string]any
	decodeJSON(t, titleSearch, &titleMatches)
	if len(titleMatches) != 1 || titleMatches[0]["slug"] != fmt.Sprintf("pagination-%s-00", unique) || titleMatches[0]["linkCount"] != float64(1) {
		t.Fatalf("title search did not find the older literal-regex title: %#v", titleMatches)
	}

	creatorSearch := requestJSON(t, router, "/public/pages?q="+url.QueryEscape(strings.ToUpper("needle creator "+unique)))
	var creatorMatches []map[string]any
	decodeJSON(t, creatorSearch, &creatorMatches)
	if len(creatorMatches) != 1 || creatorMatches[0]["slug"] != fmt.Sprintf("pagination-%s-01", unique) {
		t.Fatalf("creator search did not find the older page: %#v", creatorMatches)
	}
	if creatorMatches[0]["linkCount"] != float64(0) {
		t.Fatalf("page without blocks should have linkCount 0: %#v", creatorMatches[0])
	}
	assertSummaryFieldsArePublic(t, append(titleMatches, creatorMatches...))

	maximum := requestJSON(t, router, "/public/pages?limit=50")
	if maximum.Code != http.StatusOK {
		t.Fatalf("maximum limit should be accepted, got %d: %s", maximum.Code, maximum.Body.String())
	}
	lookahead := requestJSON(t, router, "/public/pages?offset=10008")
	if lookahead.Code != http.StatusOK {
		t.Fatalf("one lookahead page beyond the scan cap should be accepted, got %d: %s", lookahead.Code, lookahead.Body.String())
	}
	var lookaheadSummaries []map[string]any
	decodeJSON(t, lookahead, &lookaheadSummaries)
	if len(lookaheadSummaries) != 0 {
		t.Fatalf("lookahead beyond available results should be empty: %#v", lookaheadSummaries)
	}

	for _, path := range []string{
		"/public/pages?limit=0",
		"/public/pages?limit=51",
		"/public/pages?limit=wat",
		"/public/pages?limit=",
		"/public/pages?limit=1&limit=2",
		"/public/pages?offset=-1",
		"/public/pages?offset=10051",
		"/public/pages?offset=wat",
		"/public/pages?q=a",
		"/public/pages?q=" + url.QueryEscape("  a  "),
		"/public/pages?q=%FF",
		"/public/pages?q=" + strings.Repeat("a", 101),
	} {
		response := requestJSON(t, router, path)
		if response.Code != http.StatusBadRequest {
			t.Errorf("expected stable 400 for %s, got %d: %s", path, response.Code, response.Body.String())
		}
		var payload map[string]any
		decodeJSON(t, response, &payload)
		if _, ok := payload["message"].(string); !ok {
			t.Errorf("400 response has no stable message for %s: %#v", path, payload)
		}
	}

	for query := range map[string]bool{"Hidden Needle " + unique: true, "Deleted Needle " + unique: true} {
		response := requestJSON(t, router, "/public/pages?q="+url.QueryEscape(query))
		var matches []map[string]any
		decodeJSON(t, response, &matches)
		if len(matches) != 0 {
			t.Errorf("private or deleted page leaked for query %q: %#v", query, matches)
		}
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

func assertSummaryFieldsArePublic(t *testing.T, summaries []map[string]any) {
	t.Helper()
	allowed := map[string]bool{"id": true, "title": true, "icon": true, "slug": true, "creatorUsername": true, "linkCount": true}
	for _, summary := range summaries {
		for field := range summary {
			if !allowed[field] {
				t.Errorf("public summary exposed %s: %#v", field, summary)
			}
		}
		assertNoPrivateCreatorFields(t, summary)
	}
}
