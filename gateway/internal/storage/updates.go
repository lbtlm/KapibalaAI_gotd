package storage

import (
	"context"
	"database/sql"
	"github.com/gotd/td/telegram/updates"
)

type UpdateState struct {
	DB        *sql.DB
	AccountID string
	Owner     interface {
		ExecFenced(context.Context, string, ...any) (sql.Result, error)
	}
	Gate interface{ Err() error }
}

func (d UpdateState) exec(ctx context.Context, query string, args ...any) error {
	if d.Gate != nil {
		if e := d.Gate.Err(); e != nil {
			return e
		}
	}
	if d.Owner == nil {
		return ErrWriterLeaseLost
	}
	result, e := d.Owner.ExecFenced(ctx, query, args...)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (d UpdateState) GetState(ctx context.Context, uid int64) (updates.State, bool, error) {
	var s updates.State
	e := d.DB.QueryRowContext(ctx, "SELECT pts,qts,date,seq FROM gateway_update_state WHERE account_id=$1 AND user_id=$2", d.AccountID, uid).Scan(&s.Pts, &s.Qts, &s.Date, &s.Seq)
	if e == sql.ErrNoRows {
		return s, false, nil
	}
	return s, e == nil, e
}
func (d UpdateState) SetState(ctx context.Context, uid int64, s updates.State) error {
	return d.exec(ctx, "INSERT INTO gateway_update_state(account_id,user_id,pts,qts,date,seq) SELECT $1,$2,$3,$4,$5,$6 WHERE EXISTS(SELECT 1 FROM gateway_account_configs WHERE account_id=$1) AND NOT EXISTS(SELECT 1 FROM gateway_retired_accounts WHERE account_id=$1) ON CONFLICT(account_id,user_id) DO UPDATE SET pts=$3,qts=$4,date=$5,seq=$6", d.AccountID, uid, s.Pts, s.Qts, s.Date, s.Seq)
}
func (d UpdateState) field(ctx context.Context, uid int64, column string, value int) error {
	return d.exec(ctx, "UPDATE gateway_update_state SET "+column+"=$3 WHERE account_id=$1 AND user_id=$2", d.AccountID, uid, value)
}
func (d UpdateState) SetPts(ctx context.Context, uid int64, v int) error {
	return d.field(ctx, uid, "pts", v)
}
func (d UpdateState) SetQts(ctx context.Context, uid int64, v int) error {
	return d.field(ctx, uid, "qts", v)
}
func (d UpdateState) SetDate(ctx context.Context, uid int64, v int) error {
	return d.field(ctx, uid, "date", v)
}
func (d UpdateState) SetSeq(ctx context.Context, uid int64, v int) error {
	return d.field(ctx, uid, "seq", v)
}
func (d UpdateState) SetDateSeq(ctx context.Context, uid int64, date, seq int) error {
	return d.exec(ctx, "UPDATE gateway_update_state SET date=$3,seq=$4 WHERE account_id=$1 AND user_id=$2", d.AccountID, uid, date, seq)
}
func (d UpdateState) GetChannelPts(ctx context.Context, uid, cid int64) (int, bool, error) {
	var pts int
	e := d.DB.QueryRowContext(ctx, "SELECT pts FROM gateway_channel_state WHERE account_id=$1 AND user_id=$2 AND channel_id=$3", d.AccountID, uid, cid).Scan(&pts)
	if e == sql.ErrNoRows {
		return 0, false, nil
	}
	return pts, e == nil && pts > 0, e
}
func (d UpdateState) SetChannelPts(ctx context.Context, uid, cid int64, pts int) error {
	return d.exec(ctx, "INSERT INTO gateway_channel_state(account_id,user_id,channel_id,pts) SELECT $1,$2,$3,$4 WHERE EXISTS(SELECT 1 FROM gateway_account_configs WHERE account_id=$1) AND NOT EXISTS(SELECT 1 FROM gateway_retired_accounts WHERE account_id=$1) ON CONFLICT(account_id,user_id,channel_id) DO UPDATE SET pts=$4", d.AccountID, uid, cid, pts)
}
func (d UpdateState) ForEachChannels(ctx context.Context, uid int64, f func(context.Context, int64, int) error) error {
	rows, e := d.DB.QueryContext(ctx, "SELECT channel_id,pts FROM gateway_channel_state WHERE account_id=$1 AND user_id=$2 AND pts>0", d.AccountID, uid)
	if e != nil {
		return e
	}
	defer rows.Close()
	for rows.Next() {
		var cid int64
		var pts int
		if e = rows.Scan(&cid, &pts); e != nil {
			return e
		}
		if e = f(ctx, cid, pts); e != nil {
			return e
		}
	}
	return rows.Err()
}
func (d UpdateState) SetChannelAccessHash(ctx context.Context, uid, cid, hash int64) error {
	return d.exec(ctx, "INSERT INTO gateway_channel_state(account_id,user_id,channel_id,access_hash) SELECT $1,$2,$3,$4 WHERE EXISTS(SELECT 1 FROM gateway_account_configs WHERE account_id=$1) AND NOT EXISTS(SELECT 1 FROM gateway_retired_accounts WHERE account_id=$1) ON CONFLICT(account_id,user_id,channel_id) DO UPDATE SET access_hash=$4", d.AccountID, uid, cid, hash)
}
func (d UpdateState) GetChannelAccessHash(ctx context.Context, uid, cid int64) (int64, bool, error) {
	var hash int64
	e := d.DB.QueryRowContext(ctx, "SELECT access_hash FROM gateway_channel_state WHERE account_id=$1 AND user_id=$2 AND channel_id=$3", d.AccountID, uid, cid).Scan(&hash)
	if e == sql.ErrNoRows {
		return 0, false, nil
	}
	return hash, e == nil && hash != 0, e
}
