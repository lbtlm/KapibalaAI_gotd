package gateway

import (
	"context"
	"database/sql"
	"github.com/gotd/td/telegram/updates"
)

type updateStorage struct {
	db    *sql.DB
	id    string
	owner interface {
		ExecFenced(context.Context, string, ...any) (sql.Result, error)
	}
	gate *ingestGate
}

func (d updateStorage) exec(ctx context.Context, query string, args ...any) error {
	if d.gate != nil {
		if e := d.gate.err(); e != nil {
			return e
		}
	}
	if d.owner == nil {
		return ErrWriterLeaseLost
	}
	result, e := d.owner.ExecFenced(ctx, query, args...)
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

func (d updateStorage) GetState(ctx context.Context, uid int64) (updates.State, bool, error) {
	var s updates.State
	e := d.db.QueryRowContext(ctx, "SELECT pts,qts,date,seq FROM gateway_update_state WHERE account_id=$1 AND user_id=$2", d.id, uid).Scan(&s.Pts, &s.Qts, &s.Date, &s.Seq)
	if e == sql.ErrNoRows {
		return s, false, nil
	}
	return s, e == nil, e
}
func (d updateStorage) SetState(ctx context.Context, uid int64, s updates.State) error {
	return d.exec(ctx, "INSERT INTO gateway_update_state(account_id,user_id,pts,qts,date,seq) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(account_id,user_id) DO UPDATE SET pts=$3,qts=$4,date=$5,seq=$6", d.id, uid, s.Pts, s.Qts, s.Date, s.Seq)
}
func (d updateStorage) field(ctx context.Context, uid int64, column string, value int) error {
	return d.exec(ctx, "UPDATE gateway_update_state SET "+column+"=$3 WHERE account_id=$1 AND user_id=$2", d.id, uid, value)
}
func (d updateStorage) SetPts(ctx context.Context, uid int64, v int) error {
	return d.field(ctx, uid, "pts", v)
}
func (d updateStorage) SetQts(ctx context.Context, uid int64, v int) error {
	return d.field(ctx, uid, "qts", v)
}
func (d updateStorage) SetDate(ctx context.Context, uid int64, v int) error {
	return d.field(ctx, uid, "date", v)
}
func (d updateStorage) SetSeq(ctx context.Context, uid int64, v int) error {
	return d.field(ctx, uid, "seq", v)
}
func (d updateStorage) SetDateSeq(ctx context.Context, uid int64, date, seq int) error {
	return d.exec(ctx, "UPDATE gateway_update_state SET date=$3,seq=$4 WHERE account_id=$1 AND user_id=$2", d.id, uid, date, seq)
}
func (d updateStorage) GetChannelPts(ctx context.Context, uid, cid int64) (int, bool, error) {
	var pts int
	e := d.db.QueryRowContext(ctx, "SELECT pts FROM gateway_channel_state WHERE account_id=$1 AND user_id=$2 AND channel_id=$3", d.id, uid, cid).Scan(&pts)
	if e == sql.ErrNoRows {
		return 0, false, nil
	}
	return pts, e == nil && pts > 0, e
}
func (d updateStorage) SetChannelPts(ctx context.Context, uid, cid int64, pts int) error {
	return d.exec(ctx, "INSERT INTO gateway_channel_state(account_id,user_id,channel_id,pts) VALUES($1,$2,$3,$4) ON CONFLICT(account_id,user_id,channel_id) DO UPDATE SET pts=$4", d.id, uid, cid, pts)
}
func (d updateStorage) ForEachChannels(ctx context.Context, uid int64, f func(context.Context, int64, int) error) error {
	rows, e := d.db.QueryContext(ctx, "SELECT channel_id,pts FROM gateway_channel_state WHERE account_id=$1 AND user_id=$2 AND pts>0", d.id, uid)
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
func (d updateStorage) SetChannelAccessHash(ctx context.Context, uid, cid, hash int64) error {
	return d.exec(ctx, "INSERT INTO gateway_channel_state(account_id,user_id,channel_id,access_hash) VALUES($1,$2,$3,$4) ON CONFLICT(account_id,user_id,channel_id) DO UPDATE SET access_hash=$4", d.id, uid, cid, hash)
}
func (d updateStorage) GetChannelAccessHash(ctx context.Context, uid, cid int64) (int64, bool, error) {
	var hash int64
	e := d.db.QueryRowContext(ctx, "SELECT access_hash FROM gateway_channel_state WHERE account_id=$1 AND user_id=$2 AND channel_id=$3", d.id, uid, cid).Scan(&hash)
	if e == sql.ErrNoRows {
		return 0, false, nil
	}
	return hash, e == nil && hash != 0, e
}
