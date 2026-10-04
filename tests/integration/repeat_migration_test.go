//go:build integration

package integration_test

import (
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"os"
	"testing"
)

func TestRepeatReceiptMigration(t *testing.T) {
	d := newDatabase(t, true)
	for _, file := range []string{"007_repeat_commits.down.sql", "007_repeat_commits.up.sql"} {
		sql, err := os.ReadFile("../../migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		d.exec(t, string(sql))
		want := int64(1)
		if file == "007_repeat_commits.down.sql" {
			want = 0
		}
		if d.number(t, `SELECT count(*) FROM information_schema.tables WHERE table_name='repeat_commits'`) != want || d.number(t, `SELECT count(*) FROM orders`) != 1 || d.number(t, `SELECT count(*) FROM cart_items`) != 4 {
			t.Fatal("migration affected existing state")
		}
	}
	valid := `INSERT INTO repeat_commits(user_id,idempotency_key,request_digest,source_order_id,receipt) VALUES('usr-demo','repeat-migration-key',repeat('a',64),'ord-2026-002','{"sourceOrderId":"ord-2026-002"}')`
	d.exec(t, valid)
	for name, sql := range map[string]string{
		"key without digest":      `UPDATE repeat_commits SET request_digest=NULL`,
		"digest without key":      `UPDATE repeat_commits SET idempotency_key=NULL`,
		"empty digest":            `UPDATE repeat_commits SET request_digest=''`,
		"bad digest":              `UPDATE repeat_commits SET request_digest=repeat('z',64)`,
		"bad key":                 `UPDATE repeat_commits SET idempotency_key='short'`,
		"receipt source mismatch": `UPDATE repeat_commits SET source_order_id='other'`,
		"receipt null source":     `UPDATE repeat_commits SET receipt='{"sourceOrderId":null}'`,
		"duplicate key":           valid,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := d.pool.Exec(d.ctx, sql)
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || (pg.Code != "23502" && pg.Code != "23514" && pg.Code != "23505") {
				t.Fatal(err)
			}
		})
	}
	d.customer(t, "other-user")
	d.exec(t, `INSERT INTO repeat_commits(user_id,idempotency_key,request_digest,source_order_id,receipt) SELECT 'other-user',idempotency_key,request_digest,source_order_id,receipt FROM repeat_commits WHERE user_id='usr-demo'`)
	if d.number(t, `SELECT count(*) FROM repeat_commits`) != 2 {
		t.Fatal("keys not user scoped")
	}
}
