package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/giomhern/psst/internal/domain"
)

var errAmbiguousSend = errors.New("ambiguous outgoing message match")

// SendCheckpoint captures a high-water mark before automation. It never writes.
func (r *Reader) SendCheckpoint(ctx context.Context) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(ROWID),0) FROM message").Scan(&id)
	return id, err
}

// MatchSend considers at most 100 new rows in the exact chat and time window.
// No match returns an unknown receipt; ambiguity or overflow returns a safe error.
// Exact text is compared in memory and never returned or logged.
func (r *Reader) MatchSend(ctx context.Context, chatID, chatGUID, text string, after int64, started, ended time.Time) (domain.SendReceipt, error) {
	if err := domain.ValidateMessageQuery(chatID, 1); err != nil {
		return domain.SendReceipt{}, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.SendReceipt{}, err
	}
	defer tx.Rollback()
	columns, err := columnNames(ctx, tx, "message")
	if err != nil {
		return domain.SendReceipt{}, err
	}
	body := "NULL"
	if columns["attributedbody"] {
		body = `CASE WHEN typeof(m.attributedBody)='blob' AND length(m.attributedBody) BETWEEN 1 AND ` + strconv.Itoa(maxAttributedBodyBytes) + ` AND (typeof(m.text)!='text' OR length(CAST(m.text AS BLOB))=0) THEN m.attributedBody END`
	}
	rows, err := tx.QueryContext(ctx, `SELECT m.ROWID,CASE WHEN typeof(m.guid)='text' AND length(CAST(m.guid AS BLOB)) BETWEEN 1 AND 1024 THEN m.guid ELSE '' END,CASE WHEN typeof(m.text)='text' AND length(CAST(m.text AS BLOB))<=16000 THEN m.text END,`+body+`,`+messageDateSQL+`
 FROM message m WHERE m.ROWID>? AND m.is_from_me=1 AND m.ROWID IN
 (SELECT j.message_id FROM chat_message_join j JOIN chat c ON c.ROWID=j.chat_id WHERE c.ROWID=? AND c.guid=?) ORDER BY m.ROWID LIMIT 101`, after, chatID, chatGUID)
	if err != nil {
		return domain.SendReceipt{}, err
	}
	count, matches := 0, 0
	var matched int64
	var matchedGUID string
	for rows.Next() {
		var id int64
		var candidateGUID string
		var plain sql.NullString
		var body []byte
		var stamp sql.NullInt64
		if err = rows.Scan(&id, &candidateGUID, &plain, &body, &stamp); err != nil {
			rows.Close()
			return domain.SendReceipt{}, err
		}
		count++
		content := plain.String
		if content == "" && len(body) > 0 {
			content, _ = decodeAttributedBody(body)
		}
		if !stamp.Valid || !utf8.ValidString(content) || content != text {
			continue
		}
		sent := time.Unix(appleEpochUnixSeconds+stamp.Int64/1e9, stamp.Int64%1e9)
		if sent.Before(started.Add(-2*time.Second)) || sent.After(ended.Add(2*time.Second)) {
			continue
		}
		matches++
		matched = id
		matchedGUID = candidateGUID
	}
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return domain.SendReceipt{}, err
	}
	if err = tx.Commit(); err != nil {
		return domain.SendReceipt{}, err
	}
	if count > 100 || matches > 1 {
		return domain.SendReceipt{}, errAmbiguousSend
	}
	if matches != 1 || matchedGUID == "" || !utf8.ValidString(matchedGUID) {
		return domain.SendReceipt{}, nil
	}
	return r.readReceipt(ctx, chatID, chatGUID, matched, matchedGUID)
}

// SendReceipt reads only the matched outgoing GUID in its original chat.
// Optional status columns are detected independently on each snapshot.
func (r *Reader) SendReceipt(ctx context.Context, chatID, guid string) (domain.SendReceipt, error) {
	if err := domain.ValidateMessageQuery(chatID, 1); err != nil {
		return domain.SendReceipt{}, err
	}
	if len(guid) == 0 || len(guid) > 1024 || !utf8.ValidString(guid) {
		return domain.SendReceipt{}, domain.ErrSendUnavailable
	}
	return r.readReceipt(ctx, chatID, "", 0, guid)
}

func (r *Reader) readReceipt(ctx context.Context, chatID, chatGUID string, id int64, guid string) (domain.SendReceipt, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.SendReceipt{}, err
	}
	defer tx.Rollback()
	columns, err := columnNames(ctx, tx, "message")
	if err != nil {
		return domain.SendReceipt{}, err
	}
	// Only fixed allowlisted identifiers enter SQL; all caller values are bound.
	flag := func(column string) string {
		if !columns[column] {
			return "0"
		}
		return "CASE WHEN typeof(m." + column + ")='integer' AND m." + column + "=1 THEN 1 ELSE 0 END"
	}
	positive := func(column string) string {
		if !columns[column] {
			return "0"
		}
		return "CASE WHEN typeof(m." + column + ")='integer' AND (m." + column + " BETWEEN 1 AND 6279811200 OR m." + column + " BETWEEN 1000000000000 AND 6279811200000000000) THEN 1 ELSE 0 END"
	}
	failure := "0"
	if columns["error"] {
		failure = "CASE WHEN typeof(m.error)='integer' AND m.error!=0 THEN 1 ELSE 0 END"
	}
	var receipt domain.SendReceipt
	var failed, sent, delivered, known bool
	knownSQL := "0"
	if columns["is_sent"] && columns["error"] {
		knownSQL = "typeof(m.is_sent)='integer' AND m.is_sent IN(0,1) AND typeof(m.error)='integer'"
	}
	rows, err := tx.QueryContext(ctx, `SELECT m.ROWID,m.guid,`+failure+`,`+flag("is_sent")+`,(`+flag("is_delivered")+` OR `+positive("date_delivered")+`),(`+knownSQL+`) FROM message m
 WHERE m.is_from_me=1 AND m.guid=? AND m.ROWID IN
 (SELECT j.message_id FROM chat_message_join j JOIN chat c ON c.ROWID=j.chat_id WHERE c.ROWID=? AND (?='' OR c.guid=?)) LIMIT 2`, guid, chatID, chatGUID, chatGUID)
	if err != nil {
		return domain.SendReceipt{}, err
	}
	count := 0
	var rowID int64
	for rows.Next() {
		count++
		if err = rows.Scan(&rowID, &receipt.GUID, &failed, &sent, &delivered, &known); err != nil {
			rows.Close()
			return domain.SendReceipt{}, err
		}
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return domain.SendReceipt{}, err
	}
	if count != 1 || (id > 0 && rowID != id) || !utf8.ValidString(receipt.GUID) || receipt.GUID == "" || len(receipt.GUID) > 1024 {
		return domain.SendReceipt{}, nil
	}
	// Contradictory delivery/error evidence cannot safely establish either state.
	switch {
	case failed && delivered:
		receipt.Status = domain.DeliveryUnknown
	case failed:
		receipt.Status = domain.DeliveryFailed
	case delivered:
		receipt.Status = domain.DeliveryDelivered
	case sent:
		receipt.Status = domain.DeliverySent
	case known:
		receipt.Status = domain.DeliveryPending
	}
	return receipt, tx.Commit()
}
