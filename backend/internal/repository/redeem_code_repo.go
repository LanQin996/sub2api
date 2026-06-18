package repository

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/redeemcode"
	"github.com/Wei-Shaw/sub2api/ent/redeemcodeusage"
	"github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"

	entsql "entgo.io/ent/dialect/sql"
)

type redeemCodeRepository struct {
	client *dbent.Client
}

const redeemCodeUsageHistoryIDSalt int64 = 1_000_000_000_000

func NewRedeemCodeRepository(client *dbent.Client) service.RedeemCodeRepository {
	return &redeemCodeRepository{client: client}
}

func normalizeMaxRedemptions(value int) int {
	if value <= 0 {
		return 1
	}
	return value
}

func (r *redeemCodeRepository) Create(ctx context.Context, code *service.RedeemCode) error {
	created, err := r.client.RedeemCode.Create().
		SetCode(code.Code).
		SetType(code.Type).
		SetValue(code.Value).
		SetStatus(code.Status).
		SetMaxRedemptions(normalizeMaxRedemptions(code.MaxRedemptions)).
		SetRedeemedCount(code.RedeemedCount).
		SetPerUserLimit(code.PerUserLimit).
		SetRandomAmountEnabled(code.RandomAmountEnabled).
		SetRandomMinValue(code.RandomMinValue).
		SetRandomMaxValue(code.RandomMaxValue).
		SetNotes(code.Notes).
		SetValidityDays(code.ValidityDays).
		SetNillableExpiresAt(code.ExpiresAt).
		SetNillableUsedBy(code.UsedBy).
		SetNillableCreatedBy(code.CreatedBy).
		SetNillableUsedAt(code.UsedAt).
		SetNillableGroupID(code.GroupID).
		Save(ctx)
	if err == nil {
		code.ID = created.ID
		code.CreatedAt = created.CreatedAt
	}
	return err
}

func (r *redeemCodeRepository) CreateBatch(ctx context.Context, codes []service.RedeemCode) error {
	if len(codes) == 0 {
		return nil
	}

	builders := make([]*dbent.RedeemCodeCreate, 0, len(codes))
	for i := range codes {
		c := &codes[i]
		b := r.client.RedeemCode.Create().
			SetCode(c.Code).
			SetType(c.Type).
			SetValue(c.Value).
			SetStatus(c.Status).
			SetMaxRedemptions(normalizeMaxRedemptions(c.MaxRedemptions)).
			SetRedeemedCount(c.RedeemedCount).
			SetPerUserLimit(c.PerUserLimit).
			SetRandomAmountEnabled(c.RandomAmountEnabled).
			SetRandomMinValue(c.RandomMinValue).
			SetRandomMaxValue(c.RandomMaxValue).
			SetNotes(c.Notes).
			SetValidityDays(c.ValidityDays).
			SetNillableExpiresAt(c.ExpiresAt).
			SetNillableUsedBy(c.UsedBy).
			SetNillableCreatedBy(c.CreatedBy).
			SetNillableUsedAt(c.UsedAt).
			SetNillableGroupID(c.GroupID)
		builders = append(builders, b)
	}

	created, err := r.client.RedeemCode.CreateBulk(builders...).Save(ctx)
	if err != nil {
		return err
	}
	for i := range created {
		codes[i].ID = created[i].ID
		codes[i].CreatedAt = created[i].CreatedAt
	}
	return nil
}

func (r *redeemCodeRepository) GetByID(ctx context.Context, id int64) (*service.RedeemCode, error) {
	m, err := r.client.RedeemCode.Query().
		Where(redeemcode.IDEQ(id)).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, service.ErrRedeemCodeNotFound
		}
		return nil, err
	}
	return redeemCodeEntityToService(m), nil
}

func (r *redeemCodeRepository) GetByCode(ctx context.Context, code string) (*service.RedeemCode, error) {
	m, err := r.client.RedeemCode.Query().
		Where(redeemcode.CodeEQ(code)).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, service.ErrRedeemCodeNotFound
		}
		return nil, err
	}
	return redeemCodeEntityToService(m), nil
}

func (r *redeemCodeRepository) Delete(ctx context.Context, id int64) error {
	_, err := r.client.RedeemCode.Delete().Where(redeemcode.IDEQ(id)).Exec(ctx)
	return err
}

func (r *redeemCodeRepository) List(ctx context.Context, params pagination.PaginationParams) ([]service.RedeemCode, *pagination.PaginationResult, error) {
	return r.ListWithFilters(ctx, params, "", "", "")
}

func (r *redeemCodeRepository) ListWithFilters(ctx context.Context, params pagination.PaginationParams, codeType, status, search string) ([]service.RedeemCode, *pagination.PaginationResult, error) {
	q := r.client.RedeemCode.Query()

	if codeType != "" {
		q = q.Where(redeemcode.TypeEQ(codeType))
	}
	if status != "" {
		now := time.Now()
		switch status {
		case service.StatusExpired:
			q = q.Where(redeemcode.Or(
				redeemcode.StatusEQ(service.StatusExpired),
				redeemcode.And(
					redeemcode.StatusEQ(service.StatusUnused),
					redeemcode.ExpiresAtNotNil(),
					redeemcode.ExpiresAtLTE(now),
				),
			))
		case service.StatusUnused:
			q = q.Where(
				redeemcode.StatusEQ(service.StatusUnused),
				redeemcode.Or(
					redeemcode.ExpiresAtIsNil(),
					redeemcode.ExpiresAtGT(now),
				),
			)
		default:
			q = q.Where(redeemcode.StatusEQ(status))
		}
	}
	if search != "" {
		q = q.Where(
			redeemcode.Or(
				redeemcode.CodeContainsFold(search),
				redeemcode.HasUserWith(user.EmailContainsFold(search)),
			),
		)
	}

	total, err := q.Count(ctx)
	if err != nil {
		return nil, nil, err
	}

	codesQuery := q.
		WithUser().
		WithGroup().
		Offset(params.Offset()).
		Limit(params.Limit())
	for _, order := range redeemCodeListOrder(params) {
		codesQuery = codesQuery.Order(order)
	}

	codes, err := codesQuery.All(ctx)
	if err != nil {
		return nil, nil, err
	}

	outCodes := redeemCodeEntitiesToService(codes)

	return outCodes, paginationResultFromTotal(int64(total), params), nil
}

func redeemCodeListOrder(params pagination.PaginationParams) []func(*entsql.Selector) {
	sortBy := strings.ToLower(strings.TrimSpace(params.SortBy))
	sortOrder := params.NormalizedSortOrder(pagination.SortOrderDesc)

	var field string
	switch sortBy {
	case "type":
		field = redeemcode.FieldType
	case "value":
		field = redeemcode.FieldValue
	case "status":
		field = redeemcode.FieldStatus
	case "used_at":
		field = redeemcode.FieldUsedAt
	case "created_at":
		field = redeemcode.FieldCreatedAt
	case "expires_at":
		field = redeemcode.FieldExpiresAt
	case "code":
		field = redeemcode.FieldCode
	default:
		field = redeemcode.FieldID
	}

	if sortOrder == pagination.SortOrderAsc {
		return []func(*entsql.Selector){dbent.Asc(field), dbent.Asc(redeemcode.FieldID)}
	}
	return []func(*entsql.Selector){dbent.Desc(field), dbent.Desc(redeemcode.FieldID)}
}

func (r *redeemCodeRepository) Update(ctx context.Context, code *service.RedeemCode) error {
	up := r.client.RedeemCode.UpdateOneID(code.ID).
		SetCode(code.Code).
		SetType(code.Type).
		SetValue(code.Value).
		SetStatus(code.Status).
		SetMaxRedemptions(normalizeMaxRedemptions(code.MaxRedemptions)).
		SetRedeemedCount(code.RedeemedCount).
		SetPerUserLimit(code.PerUserLimit).
		SetRandomAmountEnabled(code.RandomAmountEnabled).
		SetRandomMinValue(code.RandomMinValue).
		SetRandomMaxValue(code.RandomMaxValue).
		SetNotes(code.Notes).
		SetValidityDays(code.ValidityDays)

	if code.UsedBy != nil {
		up.SetUsedBy(*code.UsedBy)
	} else {
		up.ClearUsedBy()
	}
	if code.CreatedBy != nil {
		up.SetCreatedBy(*code.CreatedBy)
	} else {
		up.ClearCreatedBy()
	}
	if code.UsedAt != nil {
		up.SetUsedAt(*code.UsedAt)
	} else {
		up.ClearUsedAt()
	}
	if code.GroupID != nil {
		up.SetGroupID(*code.GroupID)
	} else {
		up.ClearGroupID()
	}
	if code.ExpiresAt != nil {
		up.SetExpiresAt(*code.ExpiresAt)
	} else {
		up.ClearExpiresAt()
	}

	updated, err := up.Save(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return service.ErrRedeemCodeNotFound
		}
		return err
	}
	code.CreatedAt = updated.CreatedAt
	return nil
}

func (r *redeemCodeRepository) BatchUpdate(ctx context.Context, ids []int64, fields service.RedeemCodeBatchUpdateFields) (int64, error) {
	uniqueIDs := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniqueIDs = append(uniqueIDs, id)
	}
	if len(uniqueIDs) == 0 {
		return 0, nil
	}

	if tx := dbent.TxFromContext(ctx); tx != nil {
		return r.batchUpdate(ctx, tx.Client(), uniqueIDs, fields)
	}

	tx, err := r.client.Tx(ctx)
	if err != nil {
		return 0, err
	}
	txCtx := dbent.NewTxContext(ctx, tx)
	defer func() { _ = tx.Rollback() }()

	updated, err := r.batchUpdate(txCtx, tx.Client(), uniqueIDs, fields)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return updated, nil
}

func (r *redeemCodeRepository) batchUpdate(ctx context.Context, client *dbent.Client, ids []int64, fields service.RedeemCodeBatchUpdateFields) (int64, error) {
	existing, err := client.RedeemCode.Query().
		Where(redeemcode.IDIn(ids...)).
		All(ctx)
	if err != nil {
		return 0, err
	}
	if len(existing) != len(ids) {
		return 0, service.ErrRedeemCodeNotFound
	}
	if fields.TouchesUsedSensitiveFields() {
		for _, code := range existing {
			if code.Status == service.StatusUsed {
				return 0, service.ErrRedeemCodeUsed
			}
		}
	}

	up := client.RedeemCode.Update().Where(redeemcode.IDIn(ids...))
	if fields.Status != nil {
		up.SetStatus(*fields.Status)
	}
	if fields.Notes != nil {
		up.SetNotes(*fields.Notes)
	}
	if fields.ExpiresAt.Set {
		if fields.ExpiresAt.Value != nil {
			up.SetExpiresAt(*fields.ExpiresAt.Value)
		} else {
			up.ClearExpiresAt()
		}
	}
	if fields.GroupID.Set {
		if fields.GroupID.Value != nil {
			up.SetGroupID(*fields.GroupID.Value)
		} else {
			up.ClearGroupID()
		}
	}

	affected, err := up.Save(ctx)
	if err != nil {
		return 0, err
	}
	if affected != len(ids) {
		return 0, service.ErrRedeemCodeNotFound
	}
	return int64(affected), nil
}

func (r *redeemCodeRepository) Use(ctx context.Context, id, userID int64) error {
	return r.RedeemOnce(ctx, &service.RedeemCode{ID: id, MaxRedemptions: 1}, userID, 0)
}

func (r *redeemCodeRepository) RedeemOnce(ctx context.Context, code *service.RedeemCode, userID int64, value float64) error {
	if code == nil || code.ID <= 0 {
		return service.ErrRedeemCodeNotFound
	}
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return r.redeemOnce(ctx, tx.Client(), code, userID, value)
	}

	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	txCtx := dbent.NewTxContext(ctx, tx)
	defer func() { _ = tx.Rollback() }()

	if err := r.redeemOnce(txCtx, tx.Client(), code, userID, value); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *redeemCodeRepository) CreateUsage(ctx context.Context, codeID, userID int64, value float64, createdAt time.Time) error {
	if codeID <= 0 || userID <= 0 {
		return service.ErrRedeemCodeNotFound
	}
	client := r.client
	if tx := dbent.TxFromContext(ctx); tx != nil {
		client = tx.Client()
	}
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	err := client.RedeemCodeUsage.Create().
		SetRedeemCodeID(codeID).
		SetUserID(userID).
		SetValue(value).
		SetCreatedAt(createdAt).
		Exec(ctx)
	if err != nil {
		if dbent.IsConstraintError(err) {
			return service.ErrRedeemCodeUsed
		}
		return err
	}
	return nil
}

func (r *redeemCodeRepository) redeemOnce(ctx context.Context, client *dbent.Client, code *service.RedeemCode, userID int64, value float64) error {
	now := time.Now()
	current, err := client.RedeemCode.Query().
		Where(redeemcode.IDEQ(code.ID)).
		ForUpdate(entsql.WithLockAction(entsql.NoWait)).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return service.ErrRedeemCodeNotFound
		}
		return err
	}
	if current.Status != service.StatusUnused {
		return service.ErrRedeemCodeUsed
	}
	maxRedemptions := normalizeMaxRedemptions(current.MaxRedemptions)
	if current.RedeemedCount >= maxRedemptions {
		return service.ErrRedeemCodeUsed
	}
	if current.PerUserLimit {
		exists, err := client.RedeemCodeUsage.Query().
			Where(
				redeemcodeusage.RedeemCodeIDEQ(code.ID),
				redeemcodeusage.UserIDEQ(userID),
			).
			Exist(ctx)
		if err != nil {
			return err
		}
		if exists {
			return service.ErrRedeemCodeUsed
		}
	}

	if err := client.RedeemCodeUsage.Create().
		SetRedeemCodeID(code.ID).
		SetUserID(userID).
		SetValue(value).
		SetCreatedAt(now).
		Exec(ctx); err != nil {
		if dbent.IsConstraintError(err) || errors.Is(err, service.ErrRedeemCodeUsed) {
			return service.ErrRedeemCodeUsed
		}
		return err
	}

	up := client.RedeemCode.UpdateOneID(code.ID).
		AddRedeemedCount(1)
	nextCount := current.RedeemedCount + 1
	if nextCount >= maxRedemptions {
		up.SetStatus(service.StatusUsed).
			SetUsedBy(userID).
			SetUsedAt(now)
	} else if current.UsedBy == nil {
		up.SetUsedBy(userID).
			SetUsedAt(now)
	}
	if _, err := up.Save(ctx); err != nil {
		return err
	}
	return nil
}

func (r *redeemCodeRepository) ListByUser(ctx context.Context, userID int64, limit int) ([]service.RedeemCode, error) {
	if limit <= 0 {
		limit = 10
	}

	usages, err := r.client.RedeemCodeUsage.Query().
		Where(redeemcodeusage.UserIDEQ(userID)).
		WithRedeemCode(func(q *dbent.RedeemCodeQuery) {
			q.WithGroup()
		}).
		WithUser().
		Order(dbent.Desc(redeemcodeusage.FieldCreatedAt), dbent.Desc(redeemcodeusage.FieldID)).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	codes := redeemCodeUsageEntitiesToRedeemCodes(usages)
	legacyCodes, err := r.listLegacyUsedByCodesWithOffset(ctx, userID, 0, limit, "")
	if err != nil {
		return nil, err
	}
	codes = append(codes, legacyCodes...)
	sortRedeemCodesByHistoryTime(codes)
	if len(codes) > limit {
		codes = codes[:limit]
	}

	return codes, nil
}

func (r *redeemCodeRepository) ListByCreator(ctx context.Context, userID int64, params pagination.PaginationParams) ([]service.RedeemCode, *pagination.PaginationResult, error) {
	q := r.client.RedeemCode.Query().
		Where(
			redeemcode.CreatedByEQ(userID),
			redeemcode.TypeEQ(service.RedeemTypeInvitation),
		)

	total, err := q.Count(ctx)
	if err != nil {
		return nil, nil, err
	}

	codes, err := q.
		WithUser().
		Offset(params.Offset()).
		Limit(params.Limit()).
		Order(dbent.Desc(redeemcode.FieldCreatedAt), dbent.Desc(redeemcode.FieldID)).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}

	return redeemCodeEntitiesToService(codes), paginationResultFromTotal(int64(total), params), nil
}

// ListByUserPaginated returns paginated balance/concurrency history for a user.
// Supports optional type filter (e.g. "balance", "admin_balance", "concurrency", "admin_concurrency", "subscription").
func (r *redeemCodeRepository) ListByUserPaginated(ctx context.Context, userID int64, params pagination.PaginationParams, codeType string) ([]service.RedeemCode, *pagination.PaginationResult, error) {
	q := r.client.RedeemCodeUsage.Query().
		Where(redeemcodeusage.UserIDEQ(userID))

	if codeType != "" {
		q = q.Where(redeemcodeusage.HasRedeemCodeWith(redeemcode.TypeEQ(codeType)))
	}

	usageTotal, err := q.Count(ctx)
	if err != nil {
		return nil, nil, err
	}
	legacyTotal, err := r.countLegacyUsedByCodes(ctx, userID, codeType)
	if err != nil {
		return nil, nil, err
	}
	total := int64(usageTotal) + legacyTotal

	needed := params.Offset() + params.Limit()
	if needed < params.Limit() {
		needed = params.Limit()
	}
	usages, err := q.
		WithRedeemCode(func(q *dbent.RedeemCodeQuery) {
			q.WithGroup()
		}).
		WithUser().
		Limit(needed).
		Order(dbent.Desc(redeemcodeusage.FieldCreatedAt), dbent.Desc(redeemcodeusage.FieldID)).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}
	codes := redeemCodeUsageEntitiesToRedeemCodes(usages)
	if legacyTotal > 0 {
		legacyCodes, err := r.listLegacyUsedByCodesWithOffset(ctx, userID, 0, needed, codeType)
		if err != nil {
			return nil, nil, err
		}
		codes = append(codes, legacyCodes...)
	}
	sortRedeemCodesByHistoryTime(codes)
	offset := params.Offset()
	if offset >= len(codes) {
		codes = []service.RedeemCode{}
	} else {
		end := offset + params.Limit()
		if end > len(codes) {
			end = len(codes)
		}
		codes = codes[offset:end]
	}

	return codes, paginationResultFromTotal(total, params), nil
}

// SumPositiveBalanceByUser returns total recharged amount (sum of value > 0 where type is balance/admin_balance).
func (r *redeemCodeRepository) SumPositiveBalanceByUser(ctx context.Context, userID int64) (float64, error) {
	var result []struct {
		Sum float64 `json:"sum"`
	}
	err := r.client.RedeemCodeUsage.Query().
		Where(
			redeemcodeusage.UserIDEQ(userID),
			redeemcodeusage.ValueGT(0),
			redeemcodeusage.HasRedeemCodeWith(redeemcode.TypeIn("balance", "admin_balance")),
		).
		Aggregate(dbent.As(dbent.Sum(redeemcodeusage.FieldValue), "sum")).
		Scan(ctx, &result)
	if err != nil {
		return 0, err
	}
	total := 0.0
	if len(result) > 0 {
		total = result[0].Sum
	}

	var legacyResult []struct {
		Sum float64 `json:"sum"`
	}
	err = r.client.RedeemCode.Query().
		Where(
			redeemcode.UsedByEQ(userID),
			redeemcode.ValueGT(0),
			redeemcode.TypeIn("balance", "admin_balance"),
			redeemcode.Not(redeemcode.HasUsages()),
		).
		Aggregate(dbent.As(dbent.Sum(redeemcode.FieldValue), "sum")).
		Scan(ctx, &legacyResult)
	if err != nil {
		return 0, err
	}
	if len(legacyResult) > 0 {
		total += legacyResult[0].Sum
	}
	return total, nil
}

func (r *redeemCodeRepository) ListUsagesByCode(ctx context.Context, codeID int64, params pagination.PaginationParams) ([]service.RedeemCodeUsage, *pagination.PaginationResult, error) {
	q := r.client.RedeemCodeUsage.Query().
		Where(redeemcodeusage.RedeemCodeIDEQ(codeID))

	total, err := q.Count(ctx)
	if err != nil {
		return nil, nil, err
	}
	usages, err := q.
		WithRedeemCode(func(q *dbent.RedeemCodeQuery) {
			q.WithGroup()
		}).
		WithUser().
		Offset(params.Offset()).
		Limit(params.Limit()).
		Order(dbent.Desc(redeemcodeusage.FieldCreatedAt), dbent.Desc(redeemcodeusage.FieldID)).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}
	return redeemCodeUsageEntitiesToService(usages), paginationResultFromTotal(int64(total), params), nil
}

func (r *redeemCodeRepository) countLegacyUsedByCodes(ctx context.Context, userID int64, codeType string) (int64, error) {
	q := r.client.RedeemCode.Query().
		Where(
			redeemcode.UsedByEQ(userID),
			redeemcode.Not(redeemcode.HasUsages()),
		)
	if codeType != "" {
		q = q.Where(redeemcode.TypeEQ(codeType))
	}
	total, err := q.Count(ctx)
	return int64(total), err
}

func (r *redeemCodeRepository) listLegacyUsedByCodesWithOffset(ctx context.Context, userID int64, offset, limit int, codeType string) ([]service.RedeemCode, error) {
	if limit <= 0 {
		return nil, nil
	}
	q := r.client.RedeemCode.Query().
		Where(
			redeemcode.UsedByEQ(userID),
			redeemcode.Not(redeemcode.HasUsages()),
		)
	if codeType != "" {
		q = q.Where(redeemcode.TypeEQ(codeType))
	}
	codes, err := q.
		WithGroup().
		WithUser().
		Offset(offset).
		Limit(limit).
		Order(dbent.Desc(redeemcode.FieldUsedAt), dbent.Desc(redeemcode.FieldID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return redeemCodeEntitiesToService(codes), nil
}

func sortRedeemCodesByHistoryTime(codes []service.RedeemCode) {
	sort.SliceStable(codes, func(i, j int) bool {
		iTime := codes[i].CreatedAt
		if codes[i].UsedAt != nil {
			iTime = *codes[i].UsedAt
		}
		jTime := codes[j].CreatedAt
		if codes[j].UsedAt != nil {
			jTime = *codes[j].UsedAt
		}
		if iTime.Equal(jTime) {
			return codes[i].ID > codes[j].ID
		}
		return iTime.After(jTime)
	})
}

func redeemCodeEntityToService(m *dbent.RedeemCode) *service.RedeemCode {
	if m == nil {
		return nil
	}
	out := &service.RedeemCode{
		ID:                  m.ID,
		Code:                m.Code,
		Type:                m.Type,
		Value:               m.Value,
		Status:              m.Status,
		MaxRedemptions:      m.MaxRedemptions,
		RedeemedCount:       m.RedeemedCount,
		PerUserLimit:        m.PerUserLimit,
		RandomAmountEnabled: m.RandomAmountEnabled,
		RandomMinValue:      m.RandomMinValue,
		RandomMaxValue:      m.RandomMaxValue,
		UsedBy:              m.UsedBy,
		CreatedBy:           m.CreatedBy,
		UsedAt:              m.UsedAt,
		Notes:               derefString(m.Notes),
		CreatedAt:           m.CreatedAt,
		ExpiresAt:           m.ExpiresAt,
		GroupID:             m.GroupID,
		ValidityDays:        m.ValidityDays,
	}
	if m.Edges.User != nil {
		out.User = userEntityToService(m.Edges.User)
	}
	if m.Edges.Group != nil {
		out.Group = groupEntityToService(m.Edges.Group)
	}
	return out
}

func redeemCodeEntitiesToService(models []*dbent.RedeemCode) []service.RedeemCode {
	out := make([]service.RedeemCode, 0, len(models))
	for i := range models {
		if s := redeemCodeEntityToService(models[i]); s != nil {
			out = append(out, *s)
		}
	}
	return out
}

func redeemCodeUsageEntityToService(m *dbent.RedeemCodeUsage) *service.RedeemCodeUsage {
	if m == nil {
		return nil
	}
	out := &service.RedeemCodeUsage{
		ID:           m.ID,
		RedeemCodeID: m.RedeemCodeID,
		UserID:       m.UserID,
		Value:        m.Value,
		CreatedAt:    m.CreatedAt,
	}
	if m.Edges.RedeemCode != nil {
		out.RedeemCode = redeemCodeEntityToService(m.Edges.RedeemCode)
	}
	if m.Edges.User != nil {
		out.User = userEntityToService(m.Edges.User)
	}
	return out
}

func redeemCodeUsageEntitiesToService(models []*dbent.RedeemCodeUsage) []service.RedeemCodeUsage {
	out := make([]service.RedeemCodeUsage, 0, len(models))
	for i := range models {
		if s := redeemCodeUsageEntityToService(models[i]); s != nil {
			out = append(out, *s)
		}
	}
	return out
}

func redeemCodeUsageEntityToRedeemCode(m *dbent.RedeemCodeUsage) *service.RedeemCode {
	if m == nil || m.Edges.RedeemCode == nil {
		return nil
	}
	code := redeemCodeEntityToService(m.Edges.RedeemCode)
	if code == nil {
		return nil
	}
	usedBy := m.UserID
	usedAt := m.CreatedAt
	code.ID = -redeemCodeUsageHistoryIDSalt - m.ID
	code.Value = m.Value
	code.Status = service.StatusUsed
	code.UsedBy = &usedBy
	code.UsedAt = &usedAt
	if m.Edges.User != nil {
		code.User = userEntityToService(m.Edges.User)
	}
	return code
}

func redeemCodeUsageEntitiesToRedeemCodes(models []*dbent.RedeemCodeUsage) []service.RedeemCode {
	out := make([]service.RedeemCode, 0, len(models))
	for i := range models {
		if s := redeemCodeUsageEntityToRedeemCode(models[i]); s != nil {
			out = append(out, *s)
		}
	}
	return out
}
