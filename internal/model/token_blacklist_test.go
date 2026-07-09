package model

import (
	"reflect"
	"strings"
	"testing"
)

func TestTokenBlacklistDoesNotRequirePostgresUUIDExtension(t *testing.T) {
	field, ok := reflect.TypeOf(TokenBlacklist{}).FieldByName("ID")
	if !ok {
		t.Fatal("TokenBlacklist.ID field not found")
	}

	if strings.Contains(field.Tag.Get("gorm"), "uuid_generate_v4") {
		t.Fatal("TokenBlacklist.ID must not require PostgreSQL uuid-ossp extension")
	}
}
