//go:build integration

package integration_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	postgres "sweetshop/internal/repository/postgres"
)

func TestMigrationsAndDemoFixture(t *testing.T) {
	d := newDatabase(t, false)
	if err := postgres.Migrate(d.ctx, d.pool, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	for table, want := range map[string]int64{"schema_migrations": 14, "repeat_commits": 0, "staff_audit_events": 0, "users": 1, "categories": 10, "manufacturers": 8, "brands": 12, "products": 24, "collections": 4, "collection_products": 18, "cart_items": 4, "favorites": 5, "orders": 1, "order_items": 4} {
		if got := d.number(t, "SELECT count(*) FROM "+table); got != want {
			t.Errorf("%s count=%d want=%d", table, got, want)
		}
	}
	if got := d.number(t, "SELECT total_minor FROM orders WHERE id='ord-2026-002'"); got != 2017500 {
		t.Fatalf("demo snapshot total=%d", got)
	}
	if got := d.number(t, "SELECT stock FROM products WHERE id='p18'"); got != 0 {
		t.Fatal("fixture must retain unavailable product")
	}
	before := d.cart(t, "usr-demo")
	d.exec(t, "UPDATE products SET stock=23 WHERE id='p01'")
	if err := postgres.Migrate(d.ctx, d.pool, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	if got := d.number(t, "SELECT stock FROM products WHERE id='p01'"); got != 23 {
		t.Fatal("replay reset live data")
	}
	after := d.cart(t, "usr-demo")
	// The stock changed deliberately; cart persistence must not be reseeded.
	if !before.UpdatedAt.Equal(after.UpdatedAt) || quantity(after, "p01") != 2 {
		t.Fatal("replay changed cart fixture")
	}
	var versions []string
	rows, err := d.pool.Query(d.ctx, "SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(versions, []string{"001_init", "002_demo_seed", "003_catalog_scale_indexes", "004_brand_manufacturer_integrity", "005_session_transport", "006_order_idempotency", "007_repeat_commits", "008_customer_account_status", "009_staff_identity", "010_customer_self_registration", "011_customer_password_resets", "012_staff_audit", "013_product_revisions", "014_order_revisions"}) {
		t.Fatal(versions)
	}
}

func TestDatabaseConstraints(t *testing.T) {
	d := newDatabase(t, true)
	cases := []struct{ name, sql, code string }{
		{"nonnegative stock", "UPDATE products SET stock=-1 WHERE id='p01'", "23514"},
		{"minor price", "UPDATE products SET price_minor=-1 WHERE id='p01'", "23514"},
		{"positive minimum", "UPDATE products SET minimum_quantity=0 WHERE id='p01'", "23514"},
		{"positive step", "UPDATE products SET quantity_step=0 WHERE id='p01'", "23514"},
		{"positive cart quantity", "UPDATE cart_items SET quantity=0 WHERE product_id='p01'", "23514"},
		{"cart uniqueness", "INSERT INTO cart_items(user_id,product_id,quantity) VALUES('usr-demo','p01',1)", "23505"},
		{"favorite uniqueness", "INSERT INTO favorites(user_id,product_id) VALUES('usr-demo','p02')", "23505"},
		{"case insensitive email", "INSERT INTO users(id,name,email,password_hash) VALUES('duplicate','Test','DEMO@SUGAR.RU','x')", "23505"},
		{"case insensitive sku", "UPDATE products SET sku='cha-701' WHERE id='p02'", "23505"},
		{"brand manufacturer ownership", "UPDATE products SET brand_id='b-caffarel' WHERE id='p01'", "23503"},
		{"product FK", "INSERT INTO favorites(user_id,product_id) VALUES('usr-demo','missing')", "23503"},
		{"idempotency key without digest", "UPDATE orders SET idempotency_key='half-set-key-000001' WHERE id='ord-2026-002'", "23514"},
		{"idempotency digest without key", "UPDATE orders SET request_digest='abc' WHERE id='ord-2026-002'", "23514"},
		{"order FK", "INSERT INTO order_items(order_id,position,product_id,sku,name,quantity,unit_price_minor,line_total_minor) VALUES('missing',1,'legacy','SKU','Name',1,1,1)", "23503"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := d.pool.Exec(d.ctx, c.sql)
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || pg.Code != c.code {
				t.Fatalf("constraint error=%v want SQLSTATE %s", err, c.code)
			}
		})
	}
	if got := d.number(t, "SELECT count(*) FROM pg_constraint WHERE conname='products_brand_manufacturer_fk' AND condeferrable AND condeferred"); got != 1 {
		t.Fatal("ownership FK must be initially deferred")
	}
	if got := d.number(t, "SELECT count(*) FROM information_schema.columns WHERE table_name='products' AND column_name='brand_id' AND column_default IS NULL"); got != 1 {
		t.Fatal("brand must have no unsafe default")
	}
	for _, index := range []string{"products_search_idx", "products_tags_idx", "products_sku_lower_uidx", "products_name_trgm_idx", "orders_user_created_idx", "orders_user_idempotency_key_idx"} {
		if got := d.number(t, "SELECT count(*) FROM pg_indexes WHERE indexname=$1", index); got != 1 {
			t.Errorf("missing index %s", index)
		}
	}
}

func TestMigrationFailureRollbackAndRetry(t *testing.T) {
	d := newDatabase(t, false)
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("020_second.up.sql", "INSERT INTO migration_probe VALUES (2); CREATE TABLE failed_probe(id int); SELECT deliberately_missing_function();")
	write("010_first.up.sql", "CREATE TABLE migration_probe(id int); INSERT INTO migration_probe VALUES (1);")
	write("001_ignored.down.sql", "SELECT deliberately_missing_function();")
	if err := postgres.Migrate(d.ctx, d.pool, dir); err == nil {
		t.Fatal("broken migration succeeded")
	}
	if got := d.number(t, "SELECT count(*) FROM migration_probe"); got != 1 {
		t.Fatalf("partial migration committed: %d", got)
	}
	if got := d.number(t, "SELECT count(*) FROM schema_migrations"); got != 1 {
		t.Fatal("failed version was recorded")
	}
	if got := d.number(t, "SELECT count(*) FROM information_schema.tables WHERE table_name='failed_probe'"); got != 0 {
		t.Fatal("DDL not rolled back")
	}
	write("020_second.up.sql", "INSERT INTO migration_probe VALUES (2);")
	if err := postgres.Migrate(d.ctx, d.pool, dir); err != nil {
		t.Fatal(err)
	}
	// Changed SQL of an applied migration is a checksum mismatch, never skipped.
	write("010_first.up.sql", "SELECT deliberately_missing_function();")
	if err := postgres.Migrate(d.ctx, d.pool, dir); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("edited applied migration: %v", err)
	}
	if got := d.number(t, "SELECT sum(id) FROM migration_probe"); got != 3 {
		t.Fatal("retry duplicated or omitted statements")
	}
	if err := postgres.Migrate(d.ctx, d.pool, filepath.Join(dir, "absent")); err == nil {
		t.Fatal("missing directory accepted")
	}
}
