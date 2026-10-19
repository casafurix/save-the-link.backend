package authcontroller_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	authcontroller "github.com/smil-thakur/save-the-link/internals/controllers/auth_controller"
	"github.com/smil-thakur/save-the-link/internals/crypto"
	customerrors "github.com/smil-thakur/save-the-link/internals/custom_errors"
	jwtservice "github.com/smil-thakur/save-the-link/internals/jwt_service"
	"github.com/smil-thakur/save-the-link/internals/repository"
	"github.com/smil-thakur/save-the-link/internals/routes"
	authservice "github.com/smil-thakur/save-the-link/internals/services/auth_service"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestRegistrationValidationNormalizationDuplicateAndNormalizedLogin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("set MONGO_TEST_URI to run MongoDB integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Disconnect(context.Background()) })
	if warnings, err := repository.EnsureIndexes(ctx, client); err != nil || len(warnings) != 0 {
		t.Fatalf("index setup failed: warnings=%v err=%v", warnings, err)
	}

	email := fmt.Sprintf("registration-%s@example.com", bson.NewObjectID().Hex())
	t.Cleanup(func() {
		_, _ = client.Database("Auth").Collection("User").DeleteOne(context.Background(), bson.M{"email": email})
	})
	userRepository := repository.NewUserRepository(client, context.Background(), crypto.NewPasswordManager())
	controller := authcontroller.NewAuthController(authservice.NewAuthService(userRepository), nil, jwtservice.NewJWTService("test-secret"))
	router := gin.New()
	routes.AuthRoutes(router, controller)

	request := func(path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}

	invalid := request("/register", `{"username":"   ","email":"bad","password":"short"}`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", invalid.Code, invalid.Body.String())
	}
	var validation struct {
		Code   string            `json:"code"`
		Fields map[string]string `json:"fields"`
	}
	if err := json.Unmarshal(invalid.Body.Bytes(), &validation); err != nil {
		t.Fatal(err)
	}
	if validation.Code != "validation_error" || len(validation.Fields) != 3 {
		t.Fatalf("expected stable validation payload, got %s", invalid.Body.String())
	}
	oneCharacter := request("/register", `{"username":"x","email":"one@example.com","password":"password1"}`)
	if oneCharacter.Code != http.StatusBadRequest || !strings.Contains(oneCharacter.Body.String(), "at least 2 characters") {
		t.Fatalf("expected username minimum validation, got %d: %s", oneCharacter.Code, oneCharacter.Body.String())
	}

	tooLong := request("/register", fmt.Sprintf(`{"username":"name","email":"other-%s@example.com","password":"%s"}`, bson.NewObjectID().Hex(), strings.Repeat("x", 73)))
	if tooLong.Code != http.StatusBadRequest || !strings.Contains(tooLong.Body.String(), "between 8 and 72 bytes") {
		t.Fatalf("expected byte-length validation, got %d: %s", tooLong.Code, tooLong.Body.String())
	}

	created := request("/register", fmt.Sprintf(`{"username":"  Alice  ","email":"  %s  ","password":"password1"}`, strings.ToUpper(email)))
	if created.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", created.Code, created.Body.String())
	}
	var stored struct {
		Username string `bson:"username"`
		Email    string `bson:"email"`
	}
	if err := client.Database("Auth").Collection("User").FindOne(ctx, bson.M{"email": email}).Decode(&stored); err != nil {
		t.Fatal(err)
	}
	if stored.Username != "Alice" || stored.Email != email {
		t.Fatalf("registration was not normalized: %+v", stored)
	}

	duplicate := request("/register", fmt.Sprintf(`{"username":"Other","email":"%s","password":"password2"}`, strings.ToUpper(email)))
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("expected duplicate-key conflict, got %d: %s", duplicate.Code, duplicate.Body.String())
	}
	login := request("/login", fmt.Sprintf(`{"email":"  %s  ","password":"password1"}`, strings.ToUpper(email)))
	if login.Code != http.StatusOK {
		t.Fatalf("expected normalized login to succeed, got %d: %s", login.Code, login.Body.String())
	}

	legacyEmail := fmt.Sprintf("Legacy-%s@Example.com", bson.NewObjectID().Hex())
	hashedPassword, err := crypto.NewPasswordManager().HashPassword("legacy-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Database("Auth").Collection("User").InsertOne(ctx, bson.M{"username": "Legacy", "email": legacyEmail, "password": hashedPassword}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = client.Database("Auth").Collection("User").DeleteMany(context.Background(), bson.M{"email": bson.M{"$in": []string{legacyEmail, strings.ToLower(legacyEmail)}}})
	})
	legacyLogin := request("/login", fmt.Sprintf(`{"email":" %s ","password":"legacy-password"}`, strings.ToLower(legacyEmail)))
	if legacyLogin.Code != http.StatusOK {
		t.Fatalf("expected legacy mixed-case login to succeed, got %d: %s", legacyLogin.Code, legacyLogin.Body.String())
	}
	legacyUser, err := userRepository.FindUserByEmail(strings.ToLower(legacyEmail))
	if err != nil || legacyUser.Email != legacyEmail {
		t.Fatalf("expected collaborator lookup to find legacy mixed-case email, user=%+v err=%v", legacyUser, err)
	}
	legacyDuplicate := request("/register", fmt.Sprintf(`{"username":"Legacy Duplicate","email":"%s","password":"password2"}`, strings.ToLower(legacyEmail)))
	if legacyDuplicate.Code != http.StatusConflict {
		t.Fatalf("expected legacy case-variant registration conflict, got %d: %s", legacyDuplicate.Code, legacyDuplicate.Body.String())
	}

	variantEmail := fmt.Sprintf("variant-%s@example.com", bson.NewObjectID().Hex())
	upperPassword, err := crypto.NewPasswordManager().HashPassword("upper-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Database("Auth").Collection("User").InsertMany(ctx, []any{
		bson.M{"username": "Variant A", "email": variantEmail, "password": hashedPassword},
		bson.M{"username": "Variant B", "email": strings.ToUpper(variantEmail), "password": upperPassword},
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = client.Database("Auth").Collection("User").DeleteMany(context.Background(), bson.M{"email": bson.M{"$in": []string{variantEmail, strings.ToUpper(variantEmail)}}})
	})
	if warnings, err := repository.EnsureIndexes(ctx, client); err != nil || len(warnings) != 0 {
		t.Fatalf("exact normalized-email index must tolerate historical case variants: warnings=%v err=%v", warnings, err)
	}
	if response := request("/login", fmt.Sprintf(`{"email":" %s ","password":"legacy-password"}`, variantEmail)); response.Code != http.StatusOK {
		t.Fatalf("expected exact lowercase legacy login, got %d: %s", response.Code, response.Body.String())
	}
	if response := request("/login", fmt.Sprintf(`{"email":" %s ","password":"upper-password"}`, strings.ToUpper(variantEmail))); response.Code != http.StatusOK {
		t.Fatalf("expected exact uppercase legacy login, got %d: %s", response.Code, response.Body.String())
	}
	ambiguousEmail := "VaRiAnT" + variantEmail[len("variant"):]
	if response := request("/login", fmt.Sprintf(`{"email":"%s","password":"legacy-password"}`, ambiguousEmail)); response.Code != http.StatusUnauthorized {
		t.Fatalf("expected ambiguous case-insensitive login rejection, got %d: %s", response.Code, response.Body.String())
	}
	if _, err := userRepository.FindUserByEmail(variantEmail); !errors.Is(err, customerrors.ErrorUserNotFound) {
		t.Fatalf("ambiguous collaborator lookup must not select a user, got %v", err)
	}
}
