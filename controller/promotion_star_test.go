package controller

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	sqlmysql "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// The optional database matrix connects only to the explicitly supplied test
// database. It never creates or drops databases, nor drops existing tables.
func setupPromotionStarAPI(t *testing.T) *gin.Engine {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis, previousSecret := common.RedisEnabled, common.SessionSecret
	previousPassword, previousEncryption := common.PasswordLoginEnabled, common.PasswordLoginEncryptionEnabled
	kind := os.Getenv("TEST_SECURITY_DIALECT")
	if kind == "" {
		kind = "sqlite"
	}
	var dialector gorm.Dialector
	databaseType := common.DatabaseTypeSQLite
	switch kind {
	case "sqlite":
		dialector = sqlite.Open(t.TempDir() + "/promotion-api.db?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate")
	case "mysql":
		dsn := os.Getenv("TEST_MYSQL_DSN")
		require.NotEmpty(t, dsn)
		config, err := sqlmysql.ParseDSN(dsn)
		require.NoError(t, err)
		host, _, err := net.SplitHostPort(config.Addr)
		require.NoError(t, err)
		require.True(t, net.ParseIP(host).IsLoopback())
		require.Equal(t, "pstar_api", config.DBName, "use the isolated API test database")
		dialector, databaseType = mysql.Open(dsn), common.DatabaseTypeMySQL
	case "postgres":
		dsn := os.Getenv("TEST_POSTGRES_DSN")
		require.NotEmpty(t, dsn)
		parsed, err := url.Parse(dsn)
		require.NoError(t, err)
		require.True(t, net.ParseIP(parsed.Hostname()).IsLoopback())
		require.Equal(t, "/pstar_api", parsed.Path, "use the isolated API test database")
		dialector, databaseType = postgres.Open(dsn), common.DatabaseTypePostgreSQL
	default:
		t.Fatalf("unsupported test database %q", kind)
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(databaseType, databaseType)
	common.RedisEnabled, common.SessionSecret = false, "promotion-star-api-test-secret"
	common.PasswordLoginEnabled, common.PasswordLoginEncryptionEnabled = true, false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		common.RedisEnabled, common.SessionSecret = previousRedis, previousSecret
		common.PasswordLoginEnabled, common.PasswordLoginEncryptionEnabled = previousPassword, previousEncryption
		connection, err := db.DB()
		if err == nil {
			_ = connection.Close()
		}
	})
	var version string
	versionSQL := "SELECT VERSION()"
	if kind == "sqlite" {
		versionSQL = "SELECT sqlite_version()"
	}
	require.NoError(t, db.Raw(versionSQL).Scan(&version).Error)
	t.Logf("database: %s %s", kind, version)
	for range 2 {
		require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}, &model.AuthFlow{}, &model.TwoFA{},
			&model.PasskeyCredential{}, &model.UserAccessToken{}, &model.Option{}, &model.AuditLog{},
			&model.PromotionStarProfile{}, &model.PromotionStarRule{}, &model.PromotionStarEvent{}, &model.PromotionStarDaily{}, &model.PromotionStarLedgerEntry{}))
		require.NoError(t, model.InitializePromotionStar(db))
	}
	require.NoError(t, model.EnsureLegacyAccessTokenRetireAt(time.Now().Unix()))
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	self := engine.Group("/api/promotion-star", middleware.DisableCache(), middleware.UserAuth())
	self.GET("/overview", GetPromotionStarOverview)
	self.GET("/invitees", ListPromotionStarInvitees)
	self.GET("/ledger", ListPromotionStarLedger)
	admin := engine.Group("/api/promotion-star/admin", middleware.DisableCache(), middleware.AdminAuth())
	admin.GET("/profiles", ListPromotionStarProfiles)
	admin.GET("/profiles/:user_id", GetPromotionStarProfile)
	admin.GET("/profiles/:user_id/overview", GetPromotionStarOverview)
	admin.GET("/profiles/:user_id/invitees", ListPromotionStarInvitees)
	admin.GET("/profiles/:user_id/ledger", ListPromotionStarLedger)
	admin.GET("/profiles/:user_id/rules", ListPromotionStarRules)
	admin.GET("/review-events", ListPromotionStarReviewEvents)
	admin.POST("/profiles/:user_id/grant", GrantPromotionStar)
	admin.PATCH("/profiles/:user_id/rate", ChangePromotionStarRate)
	admin.POST("/profiles/:user_id/suspend", SuspendPromotionStar)
	admin.POST("/profiles/:user_id/adjustments", AdjustPromotionStarCommission)
	admin.POST("/review-events/:id/resolve", ResolvePromotionStarReviewEvent)
	engine.GET("/api/user/self", middleware.UserAuth(), GetSelf)
	engine.POST("/api/verify", middleware.UserAuth(), UniversalVerify)
	return engine
}

func promotionStarAPIUser(t *testing.T, role, inviterID int) (*model.User, service.AuthIdentity, string) {
	t.Helper()
	name := fmt.Sprintf("ps-%x", time.Now().UnixNano())
	password, err := common.Password2Hash("promotion-star-test-password")
	require.NoError(t, err)
	user := &model.User{Username: name, DisplayName: name, AffCode: name, Password: password, Role: role,
		Status: common.UserStatusEnabled, AuthVersion: 1, Group: "default", InviterId: inviterID}
	require.NoError(t, model.DB.Create(user).Error)
	identity, token := promotionStarAPISession(t, user.Id)
	return user, identity, token
}

func promotionStarAPISession(t *testing.T, userID int) (service.AuthIdentity, string) {
	t.Helper()
	user, err := model.GetUserById(userID, false)
	require.NoError(t, err)
	sid := fmt.Sprintf("ps-%d-%d", userID, time.Now().UnixNano())
	identity := service.AuthIdentity{UserID: userID, SessionID: sid, UserAuthVersion: user.AuthVersion, SessionVersion: 1}
	require.NoError(t, model.DB.Create(&model.UserSession{SID: sid, UserID: userID, UserAuthVersion: user.AuthVersion,
		Version: 1, Status: model.UserSessionStatusActive, RefreshHash: strings.Repeat("a", 64), LoginMethod: "password",
		CreatedAt: time.Now().Unix(), LastActiveAt: time.Now().Unix(), ExpiresAt: time.Now().Add(time.Hour).Unix()}).Error)
	token, _, err := service.IssueAccessToken(identity)
	require.NoError(t, err)
	return identity, token
}

func promotionStarAPIRequest(engine *gin.Engine, method, path, token, proof, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if proof != "" {
		request.Header.Set("X-Security-Proof", proof)
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	return response
}

func promotionStarAPIProof(t *testing.T, identity service.AuthIdentity, scope string, context any) string {
	t.Helper()
	payload, err := common.Marshal(context)
	require.NoError(t, err)
	proof, err := service.VerifySecurityInput(identity, service.VerificationInput{
		Method: service.VerificationMethodPassword, Scope: scope, Context: payload, Password: "promotion-star-test-password",
	})
	require.NoError(t, err)
	return proof.ProofToken
}

func TestPromotionStarAPIAuthorizationAndPrivacy(t *testing.T) {
	engine := setupPromotionStarAPI(t)
	promoter, _, token := promotionStarAPIUser(t, common.RoleCommonUser, 0)
	direct, _, _ := promotionStarAPIUser(t, common.RoleCommonUser, promoter.Id)
	indirect, _, _ := promotionStarAPIUser(t, common.RoleCommonUser, direct.Id)
	other, _, _ := promotionStarAPIUser(t, common.RoleCommonUser, 0)
	unrelated, _, _ := promotionStarAPIUser(t, common.RoleCommonUser, other.Id)
	admin, _, adminToken := promotionStarAPIUser(t, common.RoleAdminUser, 0)
	at := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC).UnixMilli()
	for _, user := range []*model.User{promoter, direct, other} {
		_, err := model.SetPromotionStarProfile(model.PromotionStarProfileInput{UserID: user.Id, Status: model.PromotionStarStatusActive,
			RateBPS: 1250, EffectiveAtMS: at - 1000, OperatorID: admin.Id, Reason: "Manual grant"})
		require.NoError(t, err)
	}
	for i, input := range []struct {
		userID int
		quota  string
	}{{direct.Id, "100"}, {direct.Id, "25"}, {indirect.Id, "900"}, {unrelated.Id, "700"}} {
		_, err := model.RecordPromotionStarEvent(nil, model.PromotionStarEventInput{EventKey: fmt.Sprintf("api-%d-%d", promoter.Id, i),
			Kind: model.PromotionStarKindConsumption, Source: model.PromotionStarSourceWalletUsage, UserID: input.userID,
			Quota: input.quota, SettledAtMS: at + int64(i), State: model.PromotionStarEventPending, InternalEvidence: "PRIVATE request_id token model_name raw_log"})
		require.NoError(t, err)
	}
	require.NoError(t, model.ProcessPromotionStarEvents(100))
	dateRange := "?start_date=2026-11-20&end_date=2026-11-20"
	response := promotionStarAPIRequest(engine, http.MethodGet, "/api/promotion-star/invitees"+dateRange, token, "", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var invitees struct {
		Data struct {
			Items []map[string]any `json:"items"`
			Total int64            `json:"total"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &invitees))
	require.Len(t, invitees.Data.Items, 1)
	assert.EqualValues(t, 1, invitees.Data.Total)
	assert.EqualValues(t, direct.Id, invitees.Data.Items[0]["user_id"])
	assert.Equal(t, "125", invitees.Data.Items[0]["wallet_consumption"])
	assert.Equal(t, "15.625", invitees.Data.Items[0]["commission_net"])
	assert.NotContains(t, invitees.Data.Items[0], "username")
	assert.NotContains(t, invitees.Data.Items[0], "email")

	response = promotionStarAPIRequest(engine, http.MethodGet, "/api/promotion-star/ledger"+dateRange, token, "", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var ledger struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &ledger))
	require.Len(t, ledger.Data.Items, 1, "same-day usage is summarized, never one row per request")
	assert.Equal(t, "125", ledger.Data.Items[0]["basis_quota"])
	allowed := map[string]bool{"id": true, "day": true, "user_id": true, "invitee_user_id": true, "source": true, "kind": true, "rule_id": true, "rate_bps": true, "basis_quota": true, "commission_quota": true, "public_reason": true}
	for key := range ledger.Data.Items[0] {
		assert.True(t, allowed[key], "unexpected private field %s", key)
	}
	for _, private := range []string{"PRIVATE", "request_id", "token", "model_name", "event_key", "internal_evidence", "raw_log"} {
		assert.NotContains(t, response.Body.String(), private)
	}

	response = promotionStarAPIRequest(engine, http.MethodGet, "/api/promotion-star/overview"+dateRange, token, "", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var overview struct {
		Data struct {
			Totals promotionStarTotals `json:"totals"`
			Daily  []map[string]any    `json:"daily"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &overview))
	assert.Equal(t, "125", overview.Data.Totals.WalletConsumption)
	assert.Equal(t, "15.625", overview.Data.Totals.CommissionNet)
	require.Len(t, overview.Data.Daily, 1)
	assert.Equal(t, "2026-11-20", overview.Data.Daily[0]["date"])

	for _, query := range []string{"user_id=1", "inviter_id=1", "invitee_id=1", "p=-1", "page_size=0", "page_size=101", "p=1&p=2", "start_date=2026-13-01", "start_date=2025-01-01&end_date=2026-11-20"} {
		response := promotionStarAPIRequest(engine, http.MethodGet, "/api/promotion-star/ledger?"+query, token, "", "")
		assert.Equal(t, http.StatusBadRequest, response.Code, query)
	}
	for _, path := range []string{"overview", "invitees", "ledger"} {
		assert.Equal(t, http.StatusForbidden, promotionStarAPIRequest(engine, http.MethodGet, "/api/promotion-star/"+path, adminToken, "", "").Code, "admin role alone grants no self identity")
		assert.Equal(t, http.StatusUnauthorized, promotionStarAPIRequest(engine, http.MethodGet, "/api/promotion-star/"+path, "", "", "").Code)
	}
	assert.Equal(t, http.StatusForbidden, promotionStarAPIRequest(engine, http.MethodGet, "/api/promotion-star/admin/profiles", token, "", "").Code)
	assert.Equal(t, http.StatusOK, promotionStarAPIRequest(engine, http.MethodGet, fmt.Sprintf("/api/promotion-star/admin/profiles/%d/overview%s", promoter.Id, dateRange), adminToken, "", "").Code)

	self := buildSelfUserData(promoter)
	assert.Equal(t, true, self["promotion_star_enabled"])
	assert.Equal(t, common.RoleCommonUser, self["role"])
	require.NoError(t, model.DB.Model(&model.PromotionStarProfile{}).Where("user_id = ?", promoter.Id).Update("status", model.PromotionStarStatusSuspended).Error)
	assert.Equal(t, false, buildSelfUserData(promoter)["promotion_star_enabled"])
	for _, path := range []string{"overview", "invitees", "ledger"} {
		assert.Equal(t, http.StatusForbidden, promotionStarAPIRequest(engine, http.MethodGet, "/api/promotion-star/"+path, token, "", "").Code)
	}
	require.NoError(t, model.DB.Model(promoter).Update("status", common.UserStatusDisabled).Error)
	assert.Equal(t, http.StatusUnauthorized, promotionStarAPIRequest(engine, http.MethodGet, "/api/promotion-star/overview", token, "", "").Code)
}

func TestPromotionStarAPIProofBindingAndSessionInvalidation(t *testing.T) {
	engine := setupPromotionStarAPI(t)
	_, identity, adminToken := promotionStarAPIUser(t, common.RoleAdminUser, 0)
	target, _, oldToken := promotionStarAPIUser(t, common.RoleCommonUser, 0)
	other, _, _ := promotionStarAPIUser(t, common.RoleCommonUser, 0)
	path := fmt.Sprintf("/api/promotion-star/admin/profiles/%d", target.Id)
	body := `{"rate_bps":1250,"reason":"Manual grant"}`
	response := promotionStarAPIRequest(engine, http.MethodPost, path+"/grant", adminToken, "", body)
	assert.Equal(t, http.StatusForbidden, response.Code)
	assert.Contains(t, response.Body.String(), "SECURITY_PROOF_REQUIRED")
	context := service.PromotionStarRateContext{UserID: target.Id, RateBPS: 1250, Reason: "Manual grant"}
	proof := promotionStarAPIProof(t, identity, service.VerificationScopePromotionStarGrant, context)
	for _, contentType := range []string{"application/x-www-form-urlencoded", "text/plain"} {
		request := httptest.NewRequest(http.MethodPost, path+"/grant", strings.NewReader(body))
		request.Header.Set("Content-Type", contentType)
		request.Header.Set("Authorization", "Bearer "+adminToken)
		request.Header.Set("X-Security-Proof", proof)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		assert.Equal(t, http.StatusBadRequest, response.Code, "simple request media types cannot perform mutations")
	}
	cookieOnly := httptest.NewRequest(http.MethodPost, path+"/grant", strings.NewReader(body))
	cookieOnly.Header.Set("Content-Type", "application/json")
	cookieOnly.AddCookie(&http.Cookie{Name: service.RefreshCookieName, Value: "unusable-cookie"})
	cookieOnly.Header.Set("X-Security-Proof", proof)
	cookieResponse := httptest.NewRecorder()
	engine.ServeHTTP(cookieResponse, cookieOnly)
	assert.Equal(t, http.StatusUnauthorized, cookieResponse.Code, "ambient cookies cannot authorize Promotion Star writes")
	for _, attempt := range []struct{ path, body string }{
		{fmt.Sprintf("/api/promotion-star/admin/profiles/%d/grant", other.Id), body},
		{path + "/grant", `{"rate_bps":9999,"reason":"Manual grant"}`},
		{path + "/grant", `{"rate_bps":1250,"reason":"Changed reason"}`},
	} {
		response := promotionStarAPIRequest(engine, http.MethodPost, attempt.path, adminToken, proof, attempt.body)
		assert.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
		assert.Contains(t, response.Body.String(), "SECURITY_PROOF_CONTEXT_MISMATCH")
	}
	for _, invalid := range []string{`{"reason":"Manual grant"}`, `{"rate_bps":null,"reason":"Manual grant"}`, `{"rate_bps":10001,"reason":"Manual grant"}`, `{"rate_bps":1250,"reason":"Manual grant","user_id":1}`, `{"rate_bps":1250,"reason":"Manual grant","operator_id":1}`} {
		assert.Equal(t, http.StatusBadRequest, promotionStarAPIRequest(engine, http.MethodPost, path+"/grant", adminToken, proof, invalid).Code, invalid)
	}
	response = promotionStarAPIRequest(engine, http.MethodPost, path+"/grant", adminToken, proof, body)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Equal(t, http.StatusForbidden, promotionStarAPIRequest(engine, http.MethodPost, path+"/grant", adminToken, proof, body).Code)
	stored, err := model.GetUserById(target.Id, false)
	require.NoError(t, err)
	assert.Equal(t, common.RoleCommonUser, stored.Role)
	assert.EqualValues(t, 2, stored.AuthVersion)
	assert.Equal(t, http.StatusUnauthorized, promotionStarAPIRequest(engine, http.MethodGet, "/api/promotion-star/overview", oldToken, "", "").Code)
	_, activeToken := promotionStarAPISession(t, target.Id)
	assert.Equal(t, http.StatusOK, promotionStarAPIRequest(engine, http.MethodGet, "/api/promotion-star/overview", activeToken, "", "").Code)

	rateContext := service.PromotionStarRateContext{UserID: target.Id, RateBPS: 0, Reason: "Zero rate"}
	rateProof := promotionStarAPIProof(t, identity, service.VerificationScopePromotionStarRate, rateContext)
	wrongScope := promotionStarAPIRequest(engine, http.MethodPost, path+"/suspend", adminToken, rateProof, `{"reason":"Zero rate"}`)
	assert.Equal(t, http.StatusForbidden, wrongScope.Code)
	assert.Contains(t, wrongScope.Body.String(), "SECURITY_PROOF_SCOPE_MISMATCH")
	response = promotionStarAPIRequest(engine, http.MethodPatch, path+"/rate", adminToken, rateProof, `{"rate_bps":0,"reason":"Zero rate"}`)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Contains(t, promotionStarAPIRequest(engine, http.MethodPatch, path+"/rate", adminToken, rateProof, `{"rate_bps":0,"reason":"Zero rate"}`).Body.String(), "SECURITY_PROOF_CONSUMED")
	suspendProof := promotionStarAPIProof(t, identity, service.VerificationScopePromotionStarSuspend, service.PromotionStarSuspendContext{UserID: target.Id, Reason: "Suspend access"})
	response = promotionStarAPIRequest(engine, http.MethodPost, path+"/suspend", adminToken, suspendProof, `{"reason":"Suspend access"}`)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Equal(t, http.StatusUnauthorized, promotionStarAPIRequest(engine, http.MethodGet, "/api/promotion-star/ledger", activeToken, "", "").Code)
	_, suspendedToken := promotionStarAPISession(t, target.Id)
	assert.Equal(t, http.StatusForbidden, promotionStarAPIRequest(engine, http.MethodGet, "/api/promotion-star/ledger", suspendedToken, "", "").Code)

	expired := promotionStarAPIProof(t, identity, service.VerificationScopePromotionStarGrant, context)
	require.NoError(t, model.DB.Model(&model.AuthFlow{}).Where("user_id = ? AND consumed_at IS NULL", identity.UserID).Update("expires_at", time.Now().Add(-time.Minute)).Error)
	response = promotionStarAPIRequest(engine, http.MethodPost, path+"/grant", adminToken, expired, body)
	assert.Equal(t, http.StatusForbidden, response.Code)
	assert.Contains(t, response.Body.String(), "SECURITY_PROOF_EXPIRED")
}

func TestPromotionStarAPIDeniesTokensAndStaleAdministrator(t *testing.T) {
	engine := setupPromotionStarAPI(t)
	admin, identity, token := promotionStarAPIUser(t, common.RoleAdminUser, 0)
	target, _, _ := promotionStarAPIUser(t, common.RoleCommonUser, 0)
	raw := model.AccessTokenPrefix + fmt.Sprintf("promotion-star-token-%d", admin.Id)
	pat := &model.UserAccessToken{UserId: admin.Id, Name: "Admin token", TokenHash: model.AccessTokenFingerprint(raw), TokenHint: "star"}
	require.NoError(t, pat.SetScopes([]string{"user:write", "user:read", "wallet:read", "wallet:write"}))
	require.NoError(t, model.DB.Create(pat).Error)
	legacy := fmt.Sprintf("promotion-star-legacy-%d", admin.Id)
	require.NoError(t, model.DB.Model(admin).Update("access_token", legacy).Error)
	for _, credential := range []string{raw, legacy} {
		for _, path := range []string{"/api/promotion-star/overview", "/api/promotion-star/admin/profiles"} {
			response := promotionStarAPIRequest(engine, http.MethodGet, path, credential, "", "")
			assert.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
			assert.Contains(t, response.Body.String(), "AUTH_SESSION_REQUIRED")
		}
	}
	patIdentity := service.AuthIdentity{UserID: admin.Id, SessionID: model.AccessTokenSessionID(pat.Id), UserAuthVersion: admin.AuthVersion, SessionVersion: model.AccessTokenSessionVersion}
	_, err := service.GetVerificationRequirements(patIdentity, service.VerificationScopePromotionStarGrant)
	assert.ErrorIs(t, err, service.ErrVerificationForbidden)
	context := service.PromotionStarRateContext{UserID: target.Id, RateBPS: 1000, Reason: "Concurrent demotion"}
	proof := promotionStarAPIProof(t, identity, service.VerificationScopePromotionStarGrant, context)
	payload, err := common.Marshal(context)
	require.NoError(t, err)
	authorization, err := service.ConsumeOperationProof(proof, identity, service.VerificationOperation{Scope: service.VerificationScopePromotionStarGrant, Context: payload})
	require.NoError(t, err)
	// Even if a concurrent caller did not bump AuthVersion, the write rechecks
	// the authoritative role under its transaction rather than trusting Gin.
	require.NoError(t, model.DB.Model(admin).Update("role", common.RoleCommonUser).Error)
	_, err = model.SetPromotionStarProfileForAdmin(authorization, "grant", model.PromotionStarProfileInput{UserID: target.Id, RateBPS: 1000, Reason: context.Reason})
	assert.ErrorIs(t, err, model.ErrPromotionStarForbidden)
	assert.Equal(t, http.StatusForbidden, promotionStarAPIRequest(engine, http.MethodGet, "/api/promotion-star/admin/profiles", token, "", "").Code)
	_, err = service.GetVerificationRequirements(identity, service.VerificationScopePromotionStarGrant)
	assert.ErrorIs(t, err, service.ErrVerificationForbidden)
}

func TestPromotionStarAPIAdjustmentAndReviewProofs(t *testing.T) {
	engine := setupPromotionStarAPI(t)
	admin, identity, token := promotionStarAPIUser(t, common.RoleAdminUser, 0)
	promoter, _, promoterToken := promotionStarAPIUser(t, common.RoleCommonUser, 0)
	invitee, _, _ := promotionStarAPIUser(t, common.RoleCommonUser, promoter.Id)
	at := time.Now().Add(-time.Minute).UnixMilli()
	_, err := model.SetPromotionStarProfile(model.PromotionStarProfileInput{UserID: promoter.Id, Status: model.PromotionStarStatusActive,
		RateBPS: 1000, EffectiveAtMS: at - 1000, OperatorID: admin.Id, Reason: "Manual grant"})
	require.NoError(t, err)
	path := fmt.Sprintf("/api/promotion-star/admin/profiles/%d/adjustments", promoter.Id)
	context := service.PromotionStarAdjustmentContext{UserID: promoter.Id, CommissionQuota: "-0.0125", Reason: "Public correction", Evidence: "PRIVATE reconciliation reference", IdempotencyKey: "correction-one"}
	body := `{"commission_quota":"-0.0125","reason":"Public correction","evidence":"PRIVATE reconciliation reference","idempotency_key":"correction-one"}`
	proof := promotionStarAPIProof(t, identity, service.VerificationScopePromotionStarAdjust, context)
	for _, replacement := range []struct{ old, next string }{{"-0.0125", "1"}, {"Public correction", "Other correction"}, {"PRIVATE reconciliation reference", "Other reference"}, {"correction-one", "correction-two"}} {
		response := promotionStarAPIRequest(engine, http.MethodPost, path, token, proof, strings.Replace(body, replacement.old, replacement.next, 1))
		assert.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
		assert.Contains(t, response.Body.String(), "SECURITY_PROOF_CONTEXT_MISMATCH")
	}
	for _, amount := range []string{"0", "1e12", "1.12345", "9007199254740992", "NaN"} {
		response := promotionStarAPIRequest(engine, http.MethodPost, path, token, proof, strings.Replace(body, "-0.0125", amount, 1))
		assert.Equal(t, http.StatusBadRequest, response.Code, amount)
	}
	response := promotionStarAPIRequest(engine, http.MethodPost, path, token, proof, body)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Contains(t, promotionStarAPIRequest(engine, http.MethodPost, path, token, proof, body).Body.String(), "SECURITY_PROOF_CONSUMED")
	retryProof := promotionStarAPIProof(t, identity, service.VerificationScopePromotionStarAdjust, context)
	response = promotionStarAPIRequest(engine, http.MethodPost, path, token, retryProof, body)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var count int64
	require.NoError(t, model.DB.Model(&model.PromotionStarLedgerEntry{}).Where("user_id = ? AND kind = ?", promoter.Id, model.PromotionStarKindAdjustment).Count(&count).Error)
	assert.EqualValues(t, 1, count, "new proof for same idempotency key cannot duplicate correction")
	response = promotionStarAPIRequest(engine, http.MethodGet, "/api/promotion-star/ledger", promoterToken, "", "")
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), "Public correction")
	assert.NotContains(t, response.Body.String(), "PRIVATE")

	var events []*model.PromotionStarEvent
	for _, input := range []model.PromotionStarEventInput{
		{EventKey: fmt.Sprintf("api-review-%d", promoter.Id), State: model.PromotionStarEventReview, Quota: "100", SettledAtMS: at, InternalEvidence: "PRIVATE funding reference"},
		{EventKey: fmt.Sprintf("api-provisional-%d", promoter.Id), State: model.PromotionStarEventProvisional, Quota: "100", InternalEvidence: "PRIVATE pending intent"},
		{EventKey: fmt.Sprintf("api-realtime-%d", promoter.Id), State: model.PromotionStarEventReview, Quota: "100", SettledAtMS: at, InternalEvidence: "Realtime wallet debits require reconciliation"},
	} {
		input.UserID, input.Kind, input.Source = invitee.Id, model.PromotionStarKindConsumption, model.PromotionStarSourceWalletUsage
		event, err := model.RecordPromotionStarEvent(nil, input)
		require.NoError(t, err)
		events = append(events, event)
	}
	resolvePath := fmt.Sprintf("/api/promotion-star/admin/review-events/%d/resolve", events[0].ID)
	reviewContext := service.PromotionStarReviewContext{EventKey: events[0].EventKey, State: "pending", Reason: "Verified final wallet debit",
		ExpectedUpdatedAtMS: events[0].UpdatedAtMS, ExpectedQuota: events[0].Quota, ExpectedSettledAtMS: events[0].SettledAtMS}
	reviewProof := promotionStarAPIProof(t, identity, service.VerificationScopePromotionStarReview, reviewContext)
	reviewBody := fmt.Sprintf(`{"state":"pending","reason":"Verified final wallet debit","expected_updated_at_ms":%d,"expected_quota":"%s","expected_settled_at_ms":%d}`, events[0].UpdatedAtMS, events[0].Quota, events[0].SettledAtMS)
	response = promotionStarAPIRequest(engine, http.MethodPost, fmt.Sprintf("/api/promotion-star/admin/review-events/%d/resolve", events[1].ID), token, reviewProof, reviewBody)
	assert.Equal(t, http.StatusForbidden, response.Code)
	assert.Contains(t, response.Body.String(), "SECURITY_PROOF_CONTEXT_MISMATCH")
	response = promotionStarAPIRequest(engine, http.MethodPost, resolvePath, token, reviewProof, strings.Replace(reviewBody, "pending", "ignored", 1))
	assert.Equal(t, http.StatusForbidden, response.Code)
	for _, mutation := range []struct {
		name, column string
		value        any
	}{
		{"newer snapshot", "updated_at_ms", events[0].UpdatedAtMS + 1},
		{"same-clock amount change", "quota", "101"},
		{"same-clock settlement change", "settled_at_ms", events[0].SettledAtMS + 1},
	} {
		proof := promotionStarAPIProof(t, identity, service.VerificationScopePromotionStarReview, reviewContext)
		require.NoError(t, model.DB.Model(&model.PromotionStarEvent{}).Where("id = ?", events[0].ID).Update(mutation.column, mutation.value).Error)
		response := promotionStarAPIRequest(engine, http.MethodPost, resolvePath, token, proof, reviewBody)
		assert.Equal(t, http.StatusConflict, response.Code, mutation.name)
		assert.Contains(t, response.Body.String(), "PROMOTION_STAR_CONFLICT")
		require.NoError(t, model.DB.Model(&model.PromotionStarEvent{}).Where("id = ?", events[0].ID).Updates(map[string]any{
			"updated_at_ms": events[0].UpdatedAtMS, "quota": events[0].Quota, "settled_at_ms": events[0].SettledAtMS,
		}).Error)
	}
	response = promotionStarAPIRequest(engine, http.MethodPost, resolvePath, token, reviewProof, reviewBody)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Contains(t, promotionStarAPIRequest(engine, http.MethodPost, resolvePath, token, reviewProof, reviewBody).Body.String(), "SECURITY_PROOF_CONSUMED")
	stored, err := model.GetPromotionStarEventByID(events[0].ID)
	require.NoError(t, err)
	assert.Equal(t, "100", stored.Quota)
	assert.Equal(t, at, stored.SettledAtMS)
	assert.Contains(t, stored.InternalEvidence, "PRIVATE funding reference")
	assert.Contains(t, stored.InternalEvidence, fmt.Sprintf("Administrator %d", admin.Id))
	for _, event := range events[1:] {
		context := service.PromotionStarReviewContext{EventKey: event.EventKey, State: "pending", Reason: "Cannot establish final amount",
			ExpectedUpdatedAtMS: event.UpdatedAtMS, ExpectedQuota: event.Quota, ExpectedSettledAtMS: event.SettledAtMS}
		proof := promotionStarAPIProof(t, identity, service.VerificationScopePromotionStarReview, context)
		body := fmt.Sprintf(`{"state":"pending","reason":"Cannot establish final amount","expected_updated_at_ms":%d,"expected_quota":"%s","expected_settled_at_ms":%d}`, event.UpdatedAtMS, event.Quota, event.SettledAtMS)
		response := promotionStarAPIRequest(engine, http.MethodPost, fmt.Sprintf("/api/promotion-star/admin/review-events/%d/resolve", event.ID), token, proof, body)
		assert.Equal(t, http.StatusBadRequest, response.Code, "unknown settlement time and realtime debit mismatch cannot be approved")
	}
	contextIgnored := service.PromotionStarReviewContext{EventKey: events[1].EventKey, State: "ignored", Reason: "Confirmed no billable wallet debit",
		ExpectedUpdatedAtMS: events[1].UpdatedAtMS, ExpectedQuota: events[1].Quota, ExpectedSettledAtMS: events[1].SettledAtMS}
	ignoredProof := promotionStarAPIProof(t, identity, service.VerificationScopePromotionStarReview, contextIgnored)
	ignoredBody := fmt.Sprintf(`{"state":"ignored","reason":"Confirmed no billable wallet debit","expected_updated_at_ms":%d,"expected_quota":"%s","expected_settled_at_ms":%d}`, events[1].UpdatedAtMS, events[1].Quota, events[1].SettledAtMS)
	response = promotionStarAPIRequest(engine, http.MethodPost, fmt.Sprintf("/api/promotion-star/admin/review-events/%d/resolve", events[1].ID), token, ignoredProof, ignoredBody)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	response = promotionStarAPIRequest(engine, http.MethodGet, "/api/promotion-star/admin/review-events?page_size=100", token, "", "")
	require.Equal(t, http.StatusOK, response.Code)
	var queue struct {
		Data struct {
			Items []struct {
				model.PromotionStarAdminEvent
				CanConfirm bool `json:"can_confirm"`
			} `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &queue))
	for _, event := range queue.Data.Items {
		assert.Contains(t, []string{model.PromotionStarEventProvisional, model.PromotionStarEventReview}, event.State)
		if event.ID == events[2].ID {
			assert.False(t, event.CanConfirm, "realtime discrepancy is never confirmable")
			assert.Equal(t, model.PromotionStarReviewRealtime, event.ReviewReason)
		}
	}
	var audits []model.AuditLog
	require.NoError(t, model.LOG_DB.Where("user_id = ?", admin.Id).Find(&audits).Error)
	require.NotEmpty(t, audits)
	encoded, err := common.Marshal(audits)
	require.NoError(t, err)
	for _, secret := range []string{proof, retryProof, reviewProof, ignoredProof, "promotion-star-test-password", "PRIVATE reconciliation reference"} {
		assert.NotContains(t, string(encoded), secret, "audit must only contain non-secret operation metadata")
	}
}

func TestPromotionStarAPIRequiresEnrolledFactor(t *testing.T) {
	setupPromotionStarAPI(t)
	admin, identity, _ := promotionStarAPIUser(t, common.RoleAdminUser, 0)
	target, _, _ := promotionStarAPIUser(t, common.RoleCommonUser, 0)
	require.NoError(t, model.DB.Create(&model.TwoFA{UserId: admin.Id, Secret: "JBSWY3DPEHPK3PXP", IsEnabled: true}).Error)
	context, err := common.Marshal(service.PromotionStarRateContext{UserID: target.Id, RateBPS: 1000, Reason: "Manual grant"})
	require.NoError(t, err)
	_, err = service.VerifySecurityInput(identity, service.VerificationInput{Scope: service.VerificationScopePromotionStarGrant,
		Context: context, Method: service.VerificationMethodPassword, Password: "promotion-star-test-password"})
	assert.ErrorIs(t, err, service.ErrProofMethod, "enrolled MFA cannot be bypassed using the password fallback")
}
