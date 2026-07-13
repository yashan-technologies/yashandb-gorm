package yasdb

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/yashan-technologies/yashandb-gorm/clauses"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const defaultVectorScoreAlias = "distance"

// VectorSearchOptions 向量相似度检索参数。
type VectorSearchOptions struct {
	// FieldName 模型字段名（如 "Embedding"）；与 ColumnName 二选一。
	FieldName string
	// ColumnName 向量列名（如 "embedding"）；与 FieldName 二选一。
	ColumnName string
	QueryVector Vector
	Distance    VectorDistanceMetric
	// TopK 返回最近邻数量；为 0 时由调用方 Limit 决定。
	TopK int
	// Approximate 为 true 时使用 FETCH APPROXIMATE 走 HNSW 索引。
	Approximate bool
	// WithScore 为 true 时在 SELECT 中附加距离分数列。
	WithScore bool
	// ScoreAlias 距离列别名，默认 distance。
	ScoreAlias string
}

// VectorSearchHit 带距离分数的检索结果包装。
type VectorSearchHit[T any] struct {
	Entity   T
	Distance float64 `gorm:"column:distance"`
}

// VectorDistanceMetricSQL 返回 vector_distance 第三个参数（度量标识符）。
func VectorDistanceMetricSQL(metric VectorDistanceMetric) (string, error) {
	normalized, err := normalizeVectorDistanceMetric(metric)
	if err != nil {
		return "", err
	}
	return string(normalized), nil
}

func normalizeVectorSearchOptions(stmt *gorm.Statement, opts VectorSearchOptions) (clauses.VectorSearch, error) {
	if opts.QueryVector.Data == nil {
		return clauses.VectorSearch{}, fmt.Errorf("query vector is required")
	}

	columnName := strings.TrimSpace(opts.ColumnName)
	if columnName == "" {
		fieldName := strings.TrimSpace(opts.FieldName)
		if fieldName == "" {
			return clauses.VectorSearch{}, fmt.Errorf("field name or column name is required")
		}
		if stmt == nil || stmt.Schema == nil {
			return clauses.VectorSearch{}, fmt.Errorf("schema is required to resolve field %q", fieldName)
		}
		field := stmt.Schema.LookUpField(fieldName)
		if field == nil {
			return clauses.VectorSearch{}, fmt.Errorf("field %q not found in schema", fieldName)
		}
		columnName = field.DBName
	}

	metricSQL, err := VectorDistanceMetricSQL(opts.Distance)
	if err != nil {
		return clauses.VectorSearch{}, err
	}

	scoreAlias := strings.TrimSpace(opts.ScoreAlias)
	if scoreAlias == "" {
		scoreAlias = defaultVectorScoreAlias
	}

	col := clause.Column{Name: columnName}
	if stmt != nil && stmt.Table != "" {
		col.Table = stmt.Table
	}

	return clauses.VectorSearch{
		Column:         col,
		QueryVector:    opts.QueryVector,
		DistanceMetric: metricSQL,
		Approximate:    opts.Approximate,
		WithScore:      opts.WithScore,
		ScoreAlias:     scoreAlias,
		TopK:           opts.TopK,
	}, nil
}

// VectorSearchClause 构造向量检索 GORM 子句。
func VectorSearchClause(stmt *gorm.Statement, opts VectorSearchOptions) (clause.Interface, error) {
	return normalizeVectorSearchOptions(stmt, opts)
}

// ApplyVectorSearch 在 DB 链上启用向量相似度检索。
func ApplyVectorSearch(db *gorm.DB, opts VectorSearchOptions) *gorm.DB {
	if db == nil {
		return nil
	}
	if db.Statement.Model != nil {
		_ = db.Statement.Parse(db.Statement.Model)
	}
	vs, err := normalizeVectorSearchOptions(db.Statement, opts)
	if err != nil {
		_ = db.AddError(err)
		return db
	}
	tx := db.Clauses(vs).Order(vectorDistanceOrderExpr(vs))
	if opts.WithScore {
		selectSQL, args := vectorDistanceSelectColumns(db.Statement, vs)
		tx = tx.Select(selectSQL, args...)
	}
	if opts.TopK > 0 {
		tx = tx.Limit(opts.TopK)
	}
	return tx
}

func vectorSearchFromStatement(stmt *gorm.Statement) (clauses.VectorSearch, bool) {
	if stmt == nil {
		return clauses.VectorSearch{}, false
	}
	c, ok := stmt.Clauses[clauses.VectorSearchName]
	if !ok || c.Expression == nil {
		return clauses.VectorSearch{}, false
	}
	vs, ok := c.Expression.(clauses.VectorSearch)
	return vs, ok
}

func qualifiedVectorColumn(vs clauses.VectorSearch) string {
	col := ConvertNameToFormat(vs.Column.Name)
	if vs.Column.Table != "" {
		col = ConvertNameToFormat(vs.Column.Table) + "." + col
	}
	return col
}

func vectorDistanceOrderExpr(vs clauses.VectorSearch) clause.Expr {
	col := qualifiedVectorColumn(vs)
	return clause.Expr{
		SQL:  fmt.Sprintf("vector_distance(%s, ?, %s)", col, vs.DistanceMetric),
		Vars: []interface{}{vs.QueryVector},
	}
}

func vectorDistanceSelectColumns(stmt *gorm.Statement, vs clauses.VectorSearch) (string, []interface{}) {
	table := ConvertNameToFormat(stmt.Table)
	parts := make([]string, 0, len(stmt.Schema.DBNames)+1)
	for _, name := range stmt.Schema.DBNames {
		parts = append(parts, table+"."+ConvertNameToFormat(name))
	}
	alias := ConvertNameToFormat(vs.ScoreAlias)
	col := qualifiedVectorColumn(vs)
	parts = append(parts, fmt.Sprintf("vector_distance(%s, ?, %s) AS %s", col, vs.DistanceMetric, alias))
	return strings.Join(parts, ", "), []interface{}{vs.QueryVector}
}

// BuildVectorSearchSQL 根据 Statement 生成向量检索 SQL（主要用于测试与调试）。
func BuildVectorSearchSQL(stmt *gorm.Statement, opts VectorSearchOptions) (string, error) {
	if stmt == nil || stmt.DB == nil {
		return "", fmt.Errorf("statement is nil")
	}
	model := stmt.Model
	if model == nil {
		model = stmt.Schema.ModelType
	}
	if model == nil {
		return "", fmt.Errorf("model is required")
	}
	tx := stmt.DB.Session(&gorm.Session{DryRun: true}).Model(model)
	if err := tx.Statement.Parse(model); err != nil {
		return "", err
	}
	if opts.TopK > 0 {
		tx = tx.Limit(opts.TopK)
	}
	tx = ApplyVectorSearch(tx, opts)
	if tx.Error != nil {
		return "", tx.Error
	}
	dest := reflect.New(tx.Statement.Schema.ModelType).Interface()
	return tx.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return tx.Find(dest)
	}), nil
}
