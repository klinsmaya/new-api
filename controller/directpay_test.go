package controller

import (
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	direct "github.com/QuantumNous/new-api/service/directpay"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestDirectPayControls(t *testing.T) {
	if os.Getenv("DIRECTPAY_TEST_DB") != "" {
		require.True(t, strings.Contains(os.Getenv("DIRECTPAY_TEST_DSN"), "127.0.0.1") && strings.Contains(os.Getenv("DIRECTPAY_TEST_DSN"), "directpay"), "dedicated loopback test DB required")
	}
	old := model.DB
	oldRuntime := direct.Active
	t.Cleanup(func() { model.DB = old; direct.Active = oldRuntime })
	var driver gorm.Dialector = sqlite.Open(":memory:")
	switch os.Getenv("DIRECTPAY_TEST_DB") {
	case "mysql":
		driver = mysql.Open(os.Getenv("DIRECTPAY_TEST_DSN"))
	case "postgres":
		driver = postgres.Open(os.Getenv("DIRECTPAY_TEST_DSN"))
	}
	db, err := gorm.Open(driver, &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.DirectPayOrder{}, &model.DirectPayEvent{}))
	require.NoError(t, db.Where(&model.Option{Key: "directpay.create_enabled"}).Delete(&model.Option{}).Error)
	direct.Active = &direct.Runtime{Config: direct.Configuration{CreateEnabled: true, AllowedUsers: []int{7}}}
	require.NoError(t, db.Create(&model.Option{Key: "directpay.create_enabled", Value: "true"}).Error)
	assert.True(t, direct.Active.CanCreate(7))
	assert.False(t, direct.Active.CanCreate(8))
	// A failed stop switch cannot report success while continuing to create orders.
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("directpay_switch_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "options" {
			tx.AddError(errors.New("injected persistence failure"))
		}
	}))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("PUT", "/api/user/direct-pay/admin", strings.NewReader(`{"enabled":false}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", 7)
	AdminDirectPaySwitch(c)
	c.Writer.WriteHeaderNow()
	assert.Equal(t, 503, w.Code)
	require.NoError(t, db.Callback().Create().Remove("directpay_switch_failure"))
	// Missing order and another user's order both disclose nothing.
	o := model.DirectPayOrder{UserID: 8, MerchantOrderNo: "private-order", RequestKey: "private-key", TopUpID: 999}
	require.NoError(t, db.Create(&o).Error)
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Set("id", 7)
	c.Params = gin.Params{{Key: "order", Value: "private-order"}}
	GetDirectPay(c)
	c.Writer.WriteHeaderNow()
	assert.Equal(t, 404, w.Code)
	require.NoError(t, db.Delete(&o).Error)
	// Turning off creation does not require removing account configuration.
	require.NoError(t, db.Model(&model.Option{}).Where(&model.Option{Key: "directpay.create_enabled"}).Update("value", "false").Error)
	assert.False(t, direct.Active.CanCreate(7))
}
