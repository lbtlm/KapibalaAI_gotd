package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

type importFiles struct {
	basename            string
	session, metadata   []byte
	sessions, metadatas int
	invalid             bool
}
type importFailure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type importResult struct {
	Basename  string         `json:"basename"`
	AccountID *string        `json:"accountId"`
	Success   bool           `json:"success"`
	Error     *importFailure `json:"error"`
}
type registration struct {
	AccountID     string `json:"accountId"`
	Session       string `json:"session"`
	APIID         int    `json:"apiId"`
	APIHash       string `json:"apiHash"`
	DeviceModel   string `json:"deviceModel,omitempty"`
	SystemVersion string `json:"systemVersion,omitempty"`
	AppVersion    string `json:"appVersion,omitempty"`
}
type importProfile struct{ phone, username, firstName, lastName *string }

func digestBytes(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func metadataString(raw map[string]json.RawMessage, key string) (string, error) {
	v := raw[key]
	if len(v) == 0 || bytes.Equal(v, []byte("null")) {
		return "", nil
	}
	var s string
	if json.Unmarshal(v, &s) != nil {
		return "", fail(400, "INVALID_METADATA")
	}
	return s, nil
}
func metadataID(raw map[string]json.RawMessage, key string) (int, error) {
	v := raw[key]
	if len(v) == 0 || bytes.Equal(v, []byte("null")) {
		return 0, nil
	}
	var s string
	if json.Unmarshal(v, &s) != nil {
		s = string(v)
	}
	i, e := strconv.Atoi(s)
	if e != nil || i < 1 || i > 2147483647 {
		return 0, fail(400, "INVALID_API_CREDENTIALS")
	}
	return i, nil
}

// Accept phone-shaped filenames, never arbitrary digits from labels or long IDs.
var importFilenamePhone = regexp.MustCompile(`(?:^|[_ (])\+?([0-9]{7,15})(?:$|[_ )])`)

func importPhone(value string) string {
	value = strings.TrimSpace(value)
	var phone strings.Builder
	for i, c := range value {
		switch {
		case c >= '0' && c <= '9':
			phone.WriteRune(c)
		case c == '+' && i == 0, c == ' ', c == '-', c == '(', c == ')':
		default:
			return ""
		}
	}
	if phone.Len() < 7 || phone.Len() > 15 {
		return ""
	}
	return phone.String()
}

func parseImportMetadata(b []byte, basename string) (registration, importProfile, error) {
	var out registration
	var profile importProfile
	var raw map[string]json.RawMessage
	if json.Unmarshal(b, &raw) != nil || raw == nil {
		return out, profile, fail(400, "INVALID_METADATA")
	}
	id, e := metadataID(raw, "api_id")
	if e != nil {
		return out, profile, e
	}
	alias, e := metadataID(raw, "app_id")
	if e != nil {
		return out, profile, e
	}
	if id != 0 && alias != 0 && id != alias {
		return out, profile, fail(400, "CONFLICTING_API_CREDENTIALS")
	}
	if id == 0 {
		id = alias
	}
	apiHash, e := metadataString(raw, "api_hash")
	if e != nil {
		return out, profile, e
	}
	hashAlias, e := metadataString(raw, "app_hash")
	if e != nil {
		return out, profile, e
	}
	if apiHash != "" && hashAlias != "" && apiHash != hashAlias {
		return out, profile, fail(400, "CONFLICTING_API_CREDENTIALS")
	}
	if apiHash == "" {
		apiHash = hashAlias
	}
	decoded, e := hex.DecodeString(apiHash)
	if id == 0 || e != nil || len(decoded) != 16 {
		return out, profile, fail(400, "INVALID_API_CREDENTIALS")
	}
	out.APIID = id
	out.APIHash = strings.ToLower(apiHash)
	for key, dst := range map[string]*string{"device_model": &out.DeviceModel, "system_version": &out.SystemVersion, "app_version": &out.AppVersion} {
		v, e := metadataString(raw, key)
		if e != nil {
			return out, profile, e
		}
		*dst = v
	}
	for _, field := range []struct {
		keys []string
		dst  **string
	}{
		{[]string{"username", "user_name"}, &profile.username},
		{[]string{"first_name", "firstName"}, &profile.firstName},
		{[]string{"last_name", "lastName"}, &profile.lastName},
	} {
		for _, key := range field.keys {
			v, e := metadataString(raw, key)
			if e != nil {
				return out, profile, e
			}
			v = strings.TrimSpace(v)
			if field.dst == &profile.username {
				v = strings.TrimPrefix(v, "@")
			}
			if v != "" {
				*field.dst = &v
				break
			}
		}
	}
	for _, key := range []string{"phone", "phone_number", "phoneNumber"} {
		v := raw[key]
		if len(v) == 0 || bytes.Equal(v, []byte("null")) {
			continue
		}
		var phone string
		if json.Unmarshal(v, &phone) != nil {
			var number json.Number
			if json.Unmarshal(v, &number) != nil {
				return out, profile, fail(400, "INVALID_METADATA")
			}
			phone = number.String()
		}
		if phone = importPhone(phone); phone != "" {
			profile.phone = &phone
			break
		}
	}
	if profile.phone == nil {
		phone := importPhone(basename)
		if phone == "" {
			if match := importFilenamePhone.FindStringSubmatch(basename); match != nil {
				phone = match[1]
			}
		}
		if phone != "" {
			profile.phone = &phone
		}
	}
	return out, profile, nil
}
func (a *App) importAccounts(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
	reader, e := r.MultipartReader()
	if e != nil {
		return fail(400, "INVALID_MULTIPART")
	}
	pairs := map[string]*importFiles{}
	group := ""
	groupFields := 0
	count := 0
	for {
		p, e := reader.NextPart()
		if e == io.EOF {
			break
		}
		if e != nil {
			return fail(400, "INVALID_MULTIPART")
		}
		if p.FileName() == "" {
			if p.FormName() != "groupId" {
				return fail(400, "INVALID_MULTIPART")
			}
			groupFields++
			if groupFields > 1 {
				return fail(400, "INVALID_MULTIPART")
			}
			v, e := io.ReadAll(io.LimitReader(p, 1025))
			p.Close()
			if e != nil || len(v) > 1024 {
				return fail(400, "INVALID_MULTIPART")
			}
			group = string(v)
			continue
		}
		if p.FormName() != "files" {
			return fail(400, "INVALID_MULTIPART")
		}
		count++
		if count > 200 {
			return fail(400, "TOO_MANY_FILES")
		}
		name := p.FileName()
		ext := strings.ToLower(filepath.Ext(name))
		base := strings.TrimSuffix(name, filepath.Ext(name))
		if base == "" {
			base = name
		}
		pair := pairs[base]
		if pair == nil {
			pair = &importFiles{basename: base}
			pairs[base] = pair
		}
		v, e := io.ReadAll(io.LimitReader(p, (16<<20)+1))
		p.Close()
		if e != nil || len(v) > 16<<20 {
			pair.invalid = true
			continue
		}
		switch ext {
		case ".session":
			pair.sessions++
			pair.session = v
		case ".json":
			pair.metadatas++
			pair.metadata = v
		default:
			pair.invalid = true
		}
	}
	if count == 0 {
		return fail(400, "NO_FILES")
	}
	keys := make([]string, 0, len(pairs))
	for key := range pairs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	results := make([]importResult, 0, len(keys))
	for _, key := range keys {
		pair := pairs[key]
		result := importResult{Basename: key}
		id, e := a.importAccountPair(r, pair, group)
		if id != "" {
			result.AccountID = &id
		}
		if e != nil {
			code := safeAccountCode(e)
			result.Error = &importFailure{Code: code, Message: code}
			if id != "" {
				a.recordAccountAttempt(r.Context(), id, "import", e)
			}
		} else {
			result.Success = true
		}
		results = append(results, result)
	}
	respond(w, 200, map[string]any{"results": results})
	return nil
}
func (a *App) importAccountPair(r *http.Request, pair *importFiles, group string) (string, error) {
	if pair.sessions > 1 || pair.metadatas > 1 {
		return "", fail(400, "AMBIGUOUS_FILE_PAIR")
	}
	if pair.invalid {
		return "", fail(400, "UNSUPPORTED_FILE")
	}
	if pair.sessions != 1 || pair.metadatas != 1 {
		return "", fail(400, "INCOMPLETE_FILE_PAIR")
	}
	if !bytes.HasPrefix(pair.session, []byte("SQLite format 3\x00")) {
		return "", fail(400, "INVALID_SESSION")
	}
	reg, profile, e := parseImportMetadata(pair.metadata, pair.basename)
	if e != nil {
		return "", e
	}
	sessionHash := digestBytes(pair.session)
	reg.AccountID = "acct_" + ID()
	configBytes, _ := json.Marshal(reg)
	configHash := digestBytes(configBytes)
	id := reg.AccountID
	ctx := r.Context()
	// Persist only fingerprints and stable identity before the external registration.
	tx, e := a.pool.Begin(ctx)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(ctx)
	if group != "" {
		var g string
		e = tx.QueryRow(ctx, `SELECT id FROM account_groups WHERE id=$1 FOR KEY SHARE`, group).Scan(&g)
		if e == pgx.ErrNoRows {
			return "", fail(404, "ACCOUNT_GROUP_NOT_FOUND")
		}
		if e != nil {
			return "", e
		}
	}
	if _, e = tx.Exec(ctx, `INSERT INTO account_imports(session_fingerprint,config_fingerprint,account_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, sessionHash, configHash, id); e != nil {
		return "", e
	}
	if e = tx.Commit(ctx); e != nil {
		return "", e
	}
	tx, e = a.pool.Begin(ctx)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(ctx)
	var existing string
	var completed bool
	if e = tx.QueryRow(ctx, `SELECT account_id,config_fingerprint,completed FROM account_imports WHERE session_fingerprint=$1 FOR UPDATE`, sessionHash).Scan(&id, &existing, &completed); e == pgx.ErrNoRows {
		return "", fail(409, "ACCOUNT_IMPORT_CONFLICT")
	} else if e != nil {
		return "", e
	}
	reg.AccountID = id
	configBytes, _ = json.Marshal(reg)
	configHash = digestBytes(configBytes)
	if existing != configHash {
		return id, fail(409, "ACCOUNT_IMPORT_CONFLICT")
	}
	reg.Session = base64.StdEncoding.EncodeToString(pair.session)
	if completed {
		var evidence struct {
			AccountID string  `json:"accountId"`
			Status    string  `json:"status"`
			Source    *string `json:"source"`
		}
		if e = a.gateway(ctx, "GET", "/accounts/"+id+"/registration", nil, &evidence); e != nil {
			return id, accountGatewayError(e)
		}
		if evidence.AccountID != id {
			return id, fail(502, "GATEWAY_PROTOCOL_ERROR")
		}
		switch evidence.Status {
		case "ready":
			if evidence.Source == nil || (*evidence.Source != "upload" && *evidence.Source != "manifest") {
				return id, fail(502, "GATEWAY_PROTOCOL_ERROR")
			}
			return id, fail(409, "ACCOUNT_ALREADY_IMPORTED")
		case "incomplete":
			return id, fail(409, "REGISTRATION_INCOMPLETE")
		case "absent":
			if evidence.Source != nil {
				return id, fail(502, "GATEWAY_PROTOCOL_ERROR")
			}
		default:
			return id, fail(502, "GATEWAY_PROTOCOL_ERROR")
		}
		// Absence is established without RPC. Persist recovery before registration;
		// an interrupted retry can retain the same account and its business history.
		var status string
		e = tx.QueryRow(ctx, `SELECT status FROM accounts WHERE id=$1 FOR UPDATE`, id).Scan(&status)
		if e != nil && e != pgx.ErrNoRows {
			return id, e
		}
		if status == "online" || status == "rate_limited" {
			if e = transition(ctx, tx, id, status, "disconnected", nil); e != nil {
				return id, e
			}
		}
		if _, e = tx.Exec(ctx, `UPDATE accounts SET connection_id=NULL WHERE id=$1`, id); e != nil {
			return id, e
		}
		if _, e = tx.Exec(ctx, `UPDATE account_imports SET completed=false WHERE session_fingerprint=$1`, sessionHash); e != nil {
			return id, e
		}
		if e = tx.Commit(ctx); e != nil {
			return id, e
		}
		tx, e = a.pool.Begin(ctx)
		if e != nil {
			return id, e
		}
		defer tx.Rollback(ctx)
		if e = tx.QueryRow(ctx, `SELECT config_fingerprint,completed FROM account_imports WHERE session_fingerprint=$1 FOR UPDATE`, sessionHash).Scan(&existing, &completed); e != nil {
			return id, e
		}
		if existing != configHash {
			return id, fail(409, "ACCOUNT_IMPORT_CONFLICT")
		}
		if completed {
			return id, fail(409, "ACCOUNT_ALREADY_IMPORTED")
		}
	}
	var groupID *string
	if group != "" {
		var g string
		e = tx.QueryRow(ctx, `SELECT id FROM account_groups WHERE id=$1 FOR KEY SHARE`, group).Scan(&g)
		if e == pgx.ErrNoRows {
			return "", fail(404, "ACCOUNT_GROUP_NOT_FOUND")
		}
		if e != nil {
			return "", e
		}
		groupID = &g
	}
	var out struct {
		AccountID string `json:"accountId"`
		Status    string `json:"status"`
	}
	if e = a.gateway(ctx, "POST", "/accounts/register", reg, &out); e != nil {
		return id, accountGatewayError(e)
	}
	if out.AccountID != id || out.Status != "idle" {
		return id, fail(502, "GATEWAY_PROTOCOL_ERROR")
	}
	inserted, e := tx.Exec(ctx, `INSERT INTO accounts(id,group_id,phone,username,first_name,last_name) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(id) DO NOTHING`, id, groupID, profile.phone, profile.username, profile.firstName, profile.lastName)
	if e != nil {
		return id, e
	}
	if _, e = tx.Exec(ctx, `UPDATE account_imports SET completed=true WHERE session_fingerprint=$1`, sessionHash); e != nil {
		return id, e
	}
	action := "import"
	if inserted.RowsAffected() == 0 {
		action = "import_recovered"
	}
	if e = writeAccountLog(ctx, tx, id, actor(ctx), action, nil); e != nil {
		return id, e
	}
	if e = tx.Commit(ctx); e != nil {
		return id, e
	}
	return id, nil
}
